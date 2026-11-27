package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Recovery of a member without a password (D3, D5; architecture §3.3). A member who signs in with password and TOTP
// replaces the TOTP code with a recovery code at sign-in. A member who has only passkeys has no password step to put
// a recovery code after, so their recovery codes serve another purpose: to enrol a new credential after losing the
// passkeys.
//
//  1. Login name and a recovery code ([startRecovery]). The code is used up, and the answer is a recovery ticket: the
//     member's uuid and the hash of the code just used, sealed with the master key. It is not a session: no cookie is
//     set, and it opens nothing but the enrolment routes below. Every failure gives the same answer, whether the login
//     name is unknown, the member has left, the code is wrong or used, or the member has a password, and costs the
//     same queries. Only someone holding a valid code of the member learns that this member has a password and should
//     sign in with it (the code is then not used).
//  2. A new credential, with the ticket: a passkey (for the member's user handle, the existing passkeys excluded) or
//     password and TOTP, as at registration. In one transaction the credential is saved, the member gets new recovery
//     codes (the old ones, the one just used included, stop working, so the ticket cannot enrol a second credential),
//     and every session of the member ends; then the member is signed in. Only now are they on the map.
//
// The passkeys the member had are kept: one may sit on a device the member still trusts, and the member is the one who
// knows. The "me" page lists them with when they were last used, and removing one needs no further proof right after
// this sign-in (it is fresh, stepup.go).

// Lifetime of a recovery ticket: time for one enrolment.
const recoveryTicketLifetime = enrolmentLifetime

// purposeRecovery seals the recovery tickets.
const purposeRecovery = "osfm recovery ticket v1"

var recoveryTicketAAD = []byte("passkey-only recovery")

// AuditRecoveryStart: a recovery code started a recovery (target: the member; details: recoveryCodesLeft).
const AuditRecoveryStart = "recovery.start"

// RevokeRecovery is the [RevokeSessions] reason after a recovery enrolment.
const RevokeRecovery = "recovery"

// Errors of the recovery.
var (
	// errRecoveryFailed: unknown login name, a member who may not sign in, a wrong or used code, or a member with a
	// password. Deliberately one error.
	errRecoveryFailed = errors.New("auth: recovery failed")
	// errRecoveryUsePassword: a valid code of a member who has a password; they recover by signing in with it.
	errRecoveryUsePassword = errors.New("auth: the member has a password")
	// errRecoveryTicketInvalid: a recovery ticket that is malformed, expired or used for an enrolment already.
	errRecoveryTicketInvalid = errors.New("auth: invalid or expired recovery ticket")
)

// recoveryTicket is the sealed state between the recovery code and the enrolment.
type recoveryTicket struct {
	Expires int64  `json:"exp"`
	Member  string `json:"m"` // the member's uuid
	Code    string `json:"c"` // hashRecoveryCode of the code used
}

func (t *recoveryTicket) expiry() int64 { return t.Expires }

// recoveryCodeRow reports whether memberID (possibly empty) has a recovery code with the hash, used or unused.
func recoveryCodeRow(app core.App, memberID, hash string, used bool) (bool, error) {
	cond := dbx.And(dbx.HashExp{"member": memberID, "code_hash": hash})
	if used {
		cond = dbx.And(cond, dbx.Not(dbx.HashExp{"used_at": ""}))
	} else {
		cond = dbx.And(cond, dbx.HashExp{"used_at": ""})
	}
	var n int64
	err := app.DB().Select("COUNT(*)").From(store.RecoveryCodes).Where(cond).Row(&n)
	return n > 0, err
}

// startRecovery is step 1: see the comment at the top of this file. It returns the sealed ticket and the member.
func startRecovery(app core.App, loginName, code string) (string, *core.Record, error) {
	login := NormalizeLoginName(loginName)
	canonical, wellFormed := normalizeRecoveryCode(code)
	var member *core.Record
	var err error
	if validLoginName(login) {
		if member, err = findSignInMember(app, login); err != nil {
			return "", nil, err
		}
	}
	// The same lookups whether or not there is a member: an unknown login name looks up nothing, at the same cost.
	memberID := ""
	if member != nil {
		memberID = member.Id
	}
	var hasPassword int64
	if err := app.DB().Select("COUNT(*)").From(store.Credentials).
		Where(dbx.HashExp{"member": memberID, "kind": CredentialPasswordTOTP}).Row(&hasPassword); err != nil {
		return "", nil, err
	}
	hash := hashRecoveryCode(canonical)
	valid, err := recoveryCodeRow(app, memberID, hash, false)
	if err != nil {
		return "", nil, err
	}
	if member == nil || !wellFormed || !valid {
		return "", nil, errRecoveryFailed
	}
	if hasPassword > 0 {
		return "", nil, errRecoveryUsePassword
	}
	ok, err := useRecoveryCode(app, member, code)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, errRecoveryFailed // used meanwhile by a concurrent request
	}
	ticket, err := sealJSON(app, purposeRecovery, &recoveryTicket{
		Expires: unixAfter(recoveryTicketLifetime), Member: member.GetString(store.FieldUUID), Code: hash,
	}, recoveryTicketAAD)
	if err != nil {
		return "", nil, err
	}
	return ticket, member, nil
}

// openRecoveryTicket returns the member a ticket names and the hash of its code, while the ticket is valid: not
// expired, the member may still sign in, and the code it was opened with is still on record (an enrolment replaces
// the codes, so the ticket enrols once).
func openRecoveryTicket(app core.App, ticket string) (*core.Record, string, error) {
	var t recoveryTicket
	if err := openJSON(app, purposeRecovery, ticket, recoveryTicketAAD, &t); err != nil {
		if errors.Is(err, errSealed) {
			return nil, "", errRecoveryTicketInvalid
		}
		return nil, "", err
	}
	if !store.IsUUID(t.Member) {
		return nil, "", errRecoveryTicketInvalid
	}
	member, err := store.FindByUUID(app, store.Members, t.Member)
	if err != nil || !CanSignIn(member) {
		return nil, "", errRecoveryTicketInvalid
	}
	if ok, err := recoveryCodeRow(app, member.Id, t.Code, true); err != nil {
		return nil, "", err
	} else if !ok {
		return nil, "", errRecoveryTicketInvalid
	}
	return member, t.Code, nil
}

// finishRecovery runs in the enrolment's transaction once the new credential is ready to save (save does it): it
// checks the ticket's code once more, saves the credential, replaces the recovery codes and ends every session of the
// member. It returns the new recovery codes.
func finishRecovery(tx core.App, member *core.Record, codeHash string, save func(tx core.App) error) ([]string, error) {
	if ok, err := recoveryCodeRow(tx, member.Id, codeHash, true); err != nil {
		return nil, err
	} else if !ok {
		return nil, errRecoveryTicketInvalid
	}
	if err := save(tx); err != nil {
		return nil, err
	}
	codes, err := NewRecoveryCodes(tx, member)
	if err != nil {
		return nil, err
	}
	if _, err := RevokeSessions(tx, member, member, RevokeRecovery); err != nil {
		return nil, err
	}
	return codes, nil
}

// startRecoveryPassword starts enrolling password and TOTP with a recovery ticket.
func startRecoveryPassword(ctx context.Context, app core.App, ticket, password string) (*PasswordEnrolmentStart,
	error) {
	member, codeHash, err := openRecoveryTicket(app, ticket)
	if err != nil {
		return nil, err
	}
	return startMemberEnrolment(ctx, app, recoveryEnrolmentAAD, codeHash, member, password, true)
}

// confirmRecoveryPassword saves the password and TOTP of [startRecoveryPassword] once the code matches. It returns
// the member, whose session the caller starts, and the new recovery codes.
func confirmRecoveryPassword(app core.App, ticket, enrolment, code string) (*core.Record, []string, error) {
	member, codeHash, err := openRecoveryTicket(app, ticket)
	if err != nil {
		return nil, nil, err
	}
	c, err := openMemberEnrolment(app, recoveryEnrolmentAAD, codeHash, member, enrolment, code)
	if err != nil {
		return nil, nil, err
	}
	var codes []string
	err = app.RunInTransaction(func(tx core.App) error {
		var err error
		codes, err = finishRecovery(tx, member, codeHash, func(tx core.App) error {
			if cred, err := findPasswordCredential(tx, member); err != nil {
				return err
			} else if cred != nil {
				return errRecoveryTicketInvalid
			}
			cred, err := savePasswordCredential(tx, member, c)
			if err != nil {
				return err
			}
			return store.WriteAudit(tx, member, AuditCredentialAdd, store.Members, member.GetString(store.FieldUUID),
				map[string]any{"kind": CredentialPasswordTOTP, "credential": cred.GetString(store.FieldUUID),
					"via": viaRecovery})
		})
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return member, codes, nil
}

// beginRecoveryPasskey starts enrolling a passkey with a recovery ticket.
func beginRecoveryPasskey(app core.App, ticket string) (*protocol.CredentialCreation, string, error) {
	member, codeHash, err := openRecoveryTicket(app, ticket)
	if err != nil {
		return nil, "", err
	}
	handle, err := memberHandle(member)
	if err != nil {
		return nil, "", err
	}
	return beginMemberPasskey(app, ceremonyRecoverPasskey, codeHash, member, handle, true)
}

// finishRecoveryPasskey saves the passkey of [beginRecoveryPasskey]. It returns the member and the new recovery codes.
func finishRecoveryPasskey(app core.App, ticket, state string, response []byte) (*core.Record, []string, error) {
	member, codeHash, err := openRecoveryTicket(app, ticket)
	if err != nil {
		return nil, nil, err
	}
	cred, handle, err := finishMemberPasskey(app, ceremonyRecoverPasskey, codeHash, member, state, response)
	if err != nil {
		return nil, nil, err
	}
	var codes []string
	err = app.RunInTransaction(func(tx core.App) error {
		var err error
		codes, err = finishRecovery(tx, member, codeHash, func(tx core.App) error {
			return addPasskey(tx, member, cred, handle, "", false, viaRecovery)
		})
		return err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("auth: recovery: %w", err)
	}
	return member, codes, nil
}
