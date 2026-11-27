package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Credential management on the "me" page (D5; architecture §3.3). A signed-in member lists their credentials, renames
// and removes passkeys, adds passkeys, sets up password and TOTP when they have none, changes the password, replaces
// the TOTP secret and regenerates the recovery codes. Everything but listing and renaming needs a fresh proof
// ([RequireFresh]).
//
// Two-step verification stays mandatory (D3): a member always keeps at least one way to sign in, a passkey or password
// and TOTP. Removing the last one is refused ([errLastCredential]); the check counts inside the transaction that
// deletes, so two removals at once cannot both pass it.
//
// Removing a credential, changing the password and replacing the TOTP secret end the member's other sessions: the
// member may be doing it because a device was lost. The current session stays.

// Audit log actions of the credential management. None records a password, code or secret.
const (
	// AuditCredentialAdd: a credential was added (actor: the member; details: kind, the credential's uuid, via).
	AuditCredentialAdd = "credentials.add"
	// AuditCredentialRemove: a credential was removed (actor: the member; details: kind, the credential's uuid).
	AuditCredentialRemove = "credentials.remove"
	// AuditPasswordChange: the member changed the password.
	AuditPasswordChange = "credentials.password_change"
	// AuditTOTPReplace: the member replaced the TOTP secret.
	AuditTOTPReplace = "credentials.totp_replace"
	// AuditRecoveryCodesRegenerate: the member generated new recovery codes; the old ones no longer work.
	AuditRecoveryCodesRegenerate = "recovery_codes.regenerate"
	// AuditRevokeOtherSessions: the member's other sessions were ended (details: reason, sessions).
	AuditRevokeOtherSessions = "sessions.revoke_others"
)

// Where a credential was added, in the AuditCredentialAdd details.
const (
	viaSettings = "settings"
	viaRecovery = "recovery"
	viaReset    = "reset_link"
)

// Reasons for ending a member's other sessions.
const (
	revokeCredentialRemoved = "credential_removed" // #nosec G101 -- an audit reason, not a credential
	revokePasswordChanged   = "password_changed"
	revokeTOTPReplaced      = "totp_replaced"
)

// Errors of the credential management.
var (
	// errLastCredential: removing this credential would leave the member without a way to sign in.
	errLastCredential = errors.New("auth: the member's last way to sign in cannot be removed")
	// errCredentialNotFound: no credential of the member has this uuid.
	errCredentialNotFound = errors.New("auth: no such credential")
	// errPasswordExists: the member has a password already.
	errPasswordExists = errors.New("auth: the member has a password already")
)

// PasskeyInfo is one of the member's passkeys as the "me" page lists it. lastUsedAt is the day (UTC midnight) of the
// last sign-in or re-verification with it, null before the first.
type PasskeyInfo struct {
	UUID       string     `json:"uuid"`
	DeviceName string     `json:"deviceName"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

// PasswordTOTPInfo is the member's password and TOTP credential: when it was set up, and the day a TOTP code last
// signed in or re-verified.
type PasswordTOTPInfo struct {
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

// CredentialsResponse is the response of GET [MeCredentialsPath] and of the changes on the "me" page: the member's
// passkeys (oldest first), the password and TOTP credential (null when none), the number of unused recovery codes, and
// until when the current session counts as freshly verified (null when it does not).
type CredentialsResponse struct {
	Passkeys          []PasskeyInfo     `json:"passkeys"`
	PasswordTOTP      *PasswordTOTPInfo `json:"passwordTotp"`
	RecoveryCodesLeft int64             `json:"recoveryCodesLeft"`
	VerifiedUntil     *time.Time        `json:"verifiedUntil"`
}

// listCredentials returns member's credentials as [CredentialsResponse], with verifiedUntil from session.
func listCredentials(app core.App, member, session *core.Record) (*CredentialsResponse, error) {
	records, err := app.FindRecordsByFilter(store.Credentials, "member = {:m}", "created,uuid", 0, 0,
		dbx.Params{"m": member.Id})
	if err != nil {
		return nil, err
	}
	out := &CredentialsResponse{Passkeys: []PasskeyInfo{}}
	for _, r := range records {
		created := r.GetDateTime(store.FieldCreated).Time().UTC()
		switch r.GetString("kind") {
		case CredentialPasskey:
			out.Passkeys = append(out.Passkeys, PasskeyInfo{UUID: r.GetString(store.FieldUUID),
				DeviceName: r.GetString("device_name"), CreatedAt: created, LastUsedAt: optionalTime(r, "last_used_at")})
		case CredentialPasswordTOTP:
			out.PasswordTOTP = &PasswordTOTPInfo{CreatedAt: created, LastUsedAt: optionalTime(r, "last_used_at")}
		}
	}
	if out.RecoveryCodesLeft, err = RemainingRecoveryCodes(app, member); err != nil {
		return nil, err
	}
	if until := verifiedUntil(session); until.After(now()) {
		out.VerifiedUntil = &until
	}
	return out, nil
}

// signInMethods counts member's passkeys and tells whether they have password and TOTP.
func signInMethods(app core.App, member *core.Record) (passkeys int64, password bool, err error) {
	if passkeys, err = app.CountRecords(store.Credentials,
		dbx.HashExp{"member": member.Id, "kind": CredentialPasskey}); err != nil {
		return 0, false, err
	}
	n, err := app.CountRecords(store.Credentials, dbx.HashExp{"member": member.Id, "kind": CredentialPasswordTOTP})
	return passkeys, n > 0, err
}

// findOwnPasskey returns member's passkey with the uuid, or errCredentialNotFound.
func findOwnPasskey(app core.App, member *core.Record, uuid string) (*core.Record, error) {
	if !store.IsUUID(uuid) {
		return nil, errCredentialNotFound
	}
	r, err := app.FindFirstRecordByFilter(store.Credentials, "member = {:m} && kind = {:k} && uuid = {:u}",
		dbx.Params{"m": member.Id, "k": CredentialPasskey, "u": uuid})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errCredentialNotFound
	}
	return r, err
}

// renamePasskey sets the device name of member's passkey with the uuid.
func renamePasskey(app core.App, member *core.Record, uuid, name string) error {
	name, err := normalizeDeviceName(name)
	if err != nil {
		return err
	}
	r, err := findOwnPasskey(app, member, uuid)
	if err != nil {
		return err
	}
	r.Set("device_name", name)
	return app.Save(r)
}

// removePasskey deletes member's passkey with the uuid, unless it is their last way to sign in, and ends their other
// sessions (keep is the current session).
func removePasskey(app core.App, member, keep *core.Record, uuid string) error {
	return app.RunInTransaction(func(tx core.App) error {
		r, err := findOwnPasskey(tx, member, uuid)
		if err != nil {
			return err
		}
		passkeys, password, err := signInMethods(tx, member)
		if err != nil {
			return err
		}
		if passkeys <= 1 && !password {
			return errLastCredential
		}
		if err := tx.Delete(r); err != nil {
			return err
		}
		if err := store.WriteAudit(tx, member, AuditCredentialRemove, store.Members, member.GetString(store.FieldUUID),
			map[string]any{"kind": CredentialPasskey, "credential": uuid}); err != nil {
			return err
		}
		return revokeOtherSessions(tx, member, keep, revokeCredentialRemoved)
	})
}

// removePassword deletes member's password and TOTP credential, unless they have no passkey, and ends their other
// sessions.
func removePassword(app core.App, member, keep *core.Record) error {
	return app.RunInTransaction(func(tx core.App) error {
		cred, err := findPasswordCredential(tx, member)
		if err != nil {
			return err
		}
		if cred == nil {
			return errNoPassword
		}
		passkeys, _, err := signInMethods(tx, member)
		if err != nil {
			return err
		}
		if passkeys == 0 {
			return errLastCredential
		}
		if err := tx.Delete(cred); err != nil {
			return err
		}
		if err := store.WriteAudit(tx, member, AuditCredentialRemove, store.Members, member.GetString(store.FieldUUID),
			map[string]any{"kind": CredentialPasswordTOTP, "credential": cred.GetString(store.FieldUUID)}); err != nil {
			return err
		}
		return revokeOtherSessions(tx, member, keep, revokeCredentialRemoved)
	})
}

// revokeOtherSessions deletes member's sessions except keep and audits it when there were any.
func revokeOtherSessions(tx core.App, member, keep *core.Record, reason string) error {
	cond := dbx.And(dbx.HashExp{"member": member.Id})
	if keep != nil {
		cond = dbx.And(cond, dbx.Not(dbx.HashExp{"id": keep.Id}))
	}
	res, err := tx.DB().Delete(store.Sessions, cond).Execute()
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	return store.WriteAudit(tx, member, AuditRevokeOtherSessions, store.Members, member.GetString(store.FieldUUID),
		map[string]any{"reason": reason, "sessions": n})
}

// startAddPassword starts setting up password and TOTP for a member who has none.
func startAddPassword(ctx context.Context, app core.App, member *core.Record, password string) (
	*PasswordEnrolmentStart, error) {
	if cred, err := findPasswordCredential(app, member); err != nil {
		return nil, err
	} else if cred != nil {
		return nil, errPasswordExists
	}
	return startMemberEnrolment(ctx, app, settingsEnrolmentAAD, memberBindHash(member), member, password, true)
}

// confirmAddPassword saves the password and TOTP credential of [startAddPassword] once the code matches.
func confirmAddPassword(app core.App, member *core.Record, enrolment, code string) error {
	c, err := openMemberEnrolment(app, settingsEnrolmentAAD, memberBindHash(member), member, enrolment, code)
	if err != nil {
		return err
	}
	return app.RunInTransaction(func(tx core.App) error {
		if cred, err := findPasswordCredential(tx, member); err != nil {
			return err
		} else if cred != nil {
			return errPasswordExists
		}
		cred, err := savePasswordCredential(tx, member, c)
		if err != nil {
			return err
		}
		return store.WriteAudit(tx, member, AuditCredentialAdd, store.Members, member.GetString(store.FieldUUID),
			map[string]any{"kind": CredentialPasswordTOTP, "credential": cred.GetString(store.FieldUUID),
				"via": viaSettings})
	})
}

// changePassword replaces member's password (the TOTP secret stays) and ends their other sessions. Outstanding
// sign-in tickets stop working with the old hash.
func changePassword(ctx context.Context, app core.App, member, keep *core.Record, password string) error {
	if err := checkPassword(password, member.GetString("login_name")); err != nil {
		return err
	}
	if cred, err := findPasswordCredential(app, member); err != nil {
		return err
	} else if cred == nil {
		return errNoPassword
	}
	hash, err := hashPassword(ctx, password)
	if err != nil {
		return err
	}
	return app.RunInTransaction(func(tx core.App) error {
		cred, err := findPasswordCredential(tx, member)
		if err != nil {
			return err
		}
		if cred == nil {
			return errNoPassword
		}
		cred.Set("password_hash", hash)
		if err := tx.Save(cred); err != nil {
			return err
		}
		if err := store.WriteAudit(tx, member, AuditPasswordChange, store.Members, member.GetString(store.FieldUUID),
			nil); err != nil {
			return err
		}
		return revokeOtherSessions(tx, member, keep, revokePasswordChanged)
	})
}

// startReplaceTOTP starts replacing member's TOTP secret: a new secret, confirmed by [confirmReplaceTOTP].
func startReplaceTOTP(ctx context.Context, app core.App, member *core.Record) (*PasswordEnrolmentStart, error) {
	if cred, err := findPasswordCredential(app, member); err != nil {
		return nil, err
	} else if cred == nil {
		return nil, errNoPassword
	}
	return startMemberEnrolment(ctx, app, totpReplaceAAD, memberBindHash(member), member, "", false)
}

// confirmReplaceTOTP stores the new TOTP secret once a code of it matches; the old secret no longer works. It ends the
// member's other sessions.
func confirmReplaceTOTP(app core.App, member, keep *core.Record, enrolment, code string) error {
	c, err := openMemberEnrolment(app, totpReplaceAAD, memberBindHash(member), member, enrolment, code)
	if err != nil {
		return err
	}
	return app.RunInTransaction(func(tx core.App) error {
		cred, err := findPasswordCredential(tx, member)
		if err != nil {
			return err
		}
		if cred == nil {
			return errNoPassword
		}
		cred.Set("totp_secret_enc", c.secretEnc)
		cred.Set("totp_last_step", c.step)
		if err := tx.Save(cred); err != nil {
			return err
		}
		if err := store.WriteAudit(tx, member, AuditTOTPReplace, store.Members, member.GetString(store.FieldUUID),
			nil); err != nil {
			return err
		}
		return revokeOtherSessions(tx, member, keep, revokeTOTPReplaced)
	})
}

// beginAddPasskey starts adding a passkey: creation options for the member's user handle (a new one for a member who
// has none yet) that exclude the member's passkeys.
func beginAddPasskey(app core.App, member *core.Record) (*protocol.CredentialCreation, string, error) {
	handle, err := memberHandle(member)
	if err != nil {
		return nil, "", err
	}
	return beginMemberPasskey(app, ceremonyAddPasskey, memberBindHash(member), member, handle, true)
}

// finishAddPasskey verifies and saves the passkey of [beginAddPasskey] under deviceName.
func finishAddPasskey(app core.App, member *core.Record, state string, response []byte, deviceName string) error {
	name, err := normalizeDeviceName(deviceName)
	if err != nil {
		return err
	}
	cred, handle, err := finishMemberPasskey(app, ceremonyAddPasskey, memberBindHash(member), member, state, response)
	if err != nil {
		return err
	}
	return app.RunInTransaction(func(tx core.App) error {
		return addPasskey(tx, member, cred, handle, name, false, viaSettings)
	})
}

// addPasskey saves a verified passkey of member inside tx, claiming its user handle, and audits it.
func addPasskey(tx core.App, member *core.Record, cred *webauthn.Credential, handle []byte, name string,
	replaceHandle bool, via string) error {
	if err := claimUserHandle(tx, member, handle, replaceHandle); err != nil {
		return err
	}
	r, err := savePasskey(tx, member, cred, name)
	if err != nil {
		return err
	}
	return store.WriteAudit(tx, member, AuditCredentialAdd, store.Members, member.GetString(store.FieldUUID),
		map[string]any{"kind": CredentialPasskey, "credential": r.GetString(store.FieldUUID), "via": via})
}

// regenerateRecoveryCodes replaces member's recovery codes with new ones and returns them.
func regenerateRecoveryCodes(app core.App, member *core.Record) ([]string, error) {
	var codes []string
	err := app.RunInTransaction(func(tx core.App) error {
		var err error
		if codes, err = NewRecoveryCodes(tx, member); err != nil {
			return err
		}
		return store.WriteAudit(tx, member, AuditRecoveryCodesRegenerate, store.Members, member.GetString(store.FieldUUID),
			nil)
	})
	if err != nil {
		return nil, fmt.Errorf("auth: regenerate recovery codes: %w", err)
	}
	return codes, nil
}
