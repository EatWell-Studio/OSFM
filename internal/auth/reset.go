package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Reset links (D5; architecture §3.3). A member who has lost every credential and every recovery code asks a
// maintainer, who creates a one-time reset link for them. The member opens it and sets up new credentials, a passkey
// or password and TOTP, with new recovery codes; then every old credential and recovery code is gone and every session
// of the member has ended.
//
//   - A token is 256 bits from crypto/rand, base64url-encoded, as an invite code; reset_links stores only its SHA-256
//     hash, the member, the maintainer who created it, the expiry ([ResetLinkLifetime]) and, once used or revoked,
//     when. The token is shown to the maintainer once, as the link /reset#token=…, in the fragment so that it never
//     reaches a server log.
//   - A link is valid while it is neither used nor revoked nor expired and its member may still sign in. Every
//     invalid token gives the same answer ([ErrInvalidResetLink]), whatever happened to it.
//   - Creating a link revokes the member's earlier pending links: one link per member is enough, and an older one
//     that went astray stops working.
//   - Maintainers may create links for any member who may sign in, other maintainers included (in a circle with one
//     or two maintainers, the other maintainer is the only one who can help), but not for themselves (they manage
//     their credentials on the "me" page) nor for members who have left or were removed (they cannot sign in anyway).
//     Creating a link needs a fresh proof ([RequireFresh]): it hands the member's account to whoever opens it.
//   - Using a link is one transaction: the link is consumed with a conditional update (of two uses, one wins), every
//     credential and recovery code of the member is deleted, the new credential and new recovery codes are saved, the
//     passkey user handle is replaced (a new passkey gets a new one; with password and TOTP it is cleared), and every
//     session of the member ends ([RevokeSessions], [RevokeCredentialsReset]). Then the member is signed in.

// ResetLinkLifetime is how long a reset link is valid.
const ResetLinkLifetime = 24 * time.Hour

// States of a reset link, as the maintainers' list shows them.
const (
	ResetLinkActive  = "active"
	ResetLinkUsed    = "used"
	ResetLinkExpired = "expired"
	ResetLinkRevoked = "revoked"
)

// Audit log actions of the reset links.
const (
	// AuditCreateResetLink: a maintainer created a reset link (actor: the maintainer; target: the link; details: the
	// member's uuid).
	AuditCreateResetLink = "reset_links.create"
	// AuditRevokeResetLink: a reset link was revoked (by a maintainer, or replaced by a new link for the member).
	AuditRevokeResetLink = "reset_links.revoke"
	// AuditUseResetLink: the member used a reset link (actor: the member; target: the link; details: method).
	AuditUseResetLink = "reset_links.use"
)

// ceremony and enrolment of a reset are bound to the token's hash; see enrol.go for their kinds.

// Errors of the reset links.
var (
	// ErrInvalidResetLink is returned for every token that cannot be used. Deliberately one error.
	ErrInvalidResetLink = errors.New("auth: invalid or expired reset link")
	// errResetMemberNotFound: no member who may sign in has the uuid.
	errResetMemberNotFound = errors.New("auth: no member who may sign in has this uuid")
	// errResetSelf: a maintainer asked for a reset link for themselves.
	errResetSelf = errors.New("auth: no reset link for oneself")
	// errResetLinkUsed: a used link cannot be revoked.
	errResetLinkUsed = errors.New("auth: the reset link has been used")
)

// ResetLinkState returns the state of a reset link at time t.
func ResetLinkState(link *core.Record, t time.Time) string {
	switch {
	case !link.GetDateTime("used_at").IsZero():
		return ResetLinkUsed
	case !link.GetDateTime("revoked_at").IsZero():
		return ResetLinkRevoked
	case !link.GetDateTime("expires_at").Time().After(t):
		return ResetLinkExpired
	default:
		return ResetLinkActive
	}
}

// revokePendingResetLinks revokes member's links that are neither used nor revoked nor expired, auditing each.
func revokePendingResetLinks(tx core.App, member, actor *core.Record, reason string) error {
	t := now().UTC()
	links, err := tx.FindAllRecords(store.ResetLinks, dbx.HashExp{"member": member.Id, "used_at": "", "revoked_at": ""},
		dbx.NewExp("expires_at > {:now}", dbx.Params{"now": dbTime(t)}))
	if err != nil {
		return err
	}
	for _, l := range links {
		if _, err := tx.DB().Update(store.ResetLinks, dbx.Params{"revoked_at": dbTime(t), store.FieldUpdated: dbTime(t)},
			dbx.HashExp{"id": l.Id}).Execute(); err != nil {
			return err
		}
		if err := writeAudit(tx, actor, AuditRevokeResetLink, store.ResetLinks, l.GetString(store.FieldUUID),
			map[string]any{"reason": reason}); err != nil {
			return err
		}
	}
	return nil
}

// CreateResetLink creates a reset link for the member with the uuid on behalf of actor, a maintainer, valid for
// [ResetLinkLifetime], revoking the member's earlier pending links, and audits it. It returns the token, which exists
// only in this return value, and the link record. errResetMemberNotFound, errResetSelf.
func CreateResetLink(app core.App, actor *core.Record, memberUUID string) (string, *core.Record, error) {
	if !store.IsUUID(memberUUID) {
		return "", nil, errResetMemberNotFound
	}
	member, err := store.Query(app, store.Members).
		Where(dbx.HashExp{store.Members + "." + store.FieldUUID: memberUUID}).One()
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, errResetMemberNotFound
	}
	if err != nil {
		return "", nil, err
	}
	if !CanSignIn(member) {
		return "", nil, errResetMemberNotFound
	}
	if member.Id == actor.Id {
		return "", nil, errResetSelf
	}
	token, err := newToken()
	if err != nil {
		return "", nil, err
	}
	var link *core.Record
	err = app.RunInTransaction(func(tx core.App) error {
		if err := revokePendingResetLinks(tx, member, actor, "replaced"); err != nil {
			return err
		}
		c, err := tx.FindCachedCollectionByNameOrId(store.ResetLinks)
		if err != nil {
			return err
		}
		link = core.NewRecord(c)
		link.Set("code_hash", hashToken(token))
		link.Set("member", member.Id)
		link.Set("created_by", actor.Id)
		link.Set("expires_at", now().UTC().Add(ResetLinkLifetime))
		if err := tx.Save(link); err != nil {
			return err
		}
		return writeAudit(tx, actor, AuditCreateResetLink, store.ResetLinks, link.GetString(store.FieldUUID),
			map[string]any{"member": member.GetString(store.FieldUUID)})
	})
	if err != nil {
		return "", nil, fmt.Errorf("auth: create reset link: %w", err)
	}
	return token, link, nil
}

// RevokeResetLink revokes link on behalf of actor and audits it; link is reloaded. Revoking a revoked (or expired)
// link changes nothing; a used link gives errResetLinkUsed.
func RevokeResetLink(app core.App, link, actor *core.Record) error {
	err := app.RunInTransaction(func(tx core.App) error {
		t := dbTime(now())
		res, err := tx.DB().Update(store.ResetLinks, dbx.Params{"revoked_at": t, store.FieldUpdated: t},
			dbx.HashExp{"id": link.Id, "used_at": "", "revoked_at": ""}).Execute()
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		current, err := tx.FindRecordById(store.ResetLinks, link.Id)
		if err != nil {
			return err
		}
		link.Load(current.FieldsData())
		switch {
		case n == 1:
			return writeAudit(tx, actor, AuditRevokeResetLink, store.ResetLinks, link.GetString(store.FieldUUID), nil)
		case !current.GetDateTime("used_at").IsZero():
			return errResetLinkUsed
		default:
			return nil
		}
	})
	if errors.Is(err, errResetLinkUsed) {
		return err
	}
	if err != nil {
		return fmt.Errorf("auth: revoke reset link: %w", err)
	}
	return nil
}

// findValidResetLink returns the link a token names and its member if it can be used now; otherwise
// [ErrInvalidResetLink]. The lookup is by the token's hash, as for invites.
func findValidResetLink(app core.App, token string) (link, member *core.Record, err error) {
	if !wellFormed(token) {
		return nil, nil, ErrInvalidResetLink
	}
	hash := hashToken(token)
	link, err = app.FindFirstRecordByData(store.ResetLinks, "code_hash", hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrInvalidResetLink
	}
	if err != nil {
		return nil, nil, err
	}
	if subtle.ConstantTimeCompare([]byte(link.GetString("code_hash")), []byte(hash)) != 1 ||
		ResetLinkState(link, now().UTC()) != ResetLinkActive {
		return nil, nil, ErrInvalidResetLink
	}
	member, err = store.Query(app, store.Members).
		Where(dbx.HashExp{store.Members + ".id": link.GetString("member")}).One()
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrInvalidResetLink
	}
	if err != nil {
		return nil, nil, err
	}
	if !CanSignIn(member) {
		return nil, nil, ErrInvalidResetLink
	}
	return link, member, nil
}

// useResetLink runs a reset in one transaction: see the comment at the top of this file. save stores the new
// credential (the old ones are gone by then). It returns the member and the new recovery codes.
func useResetLink(app core.App, token, method string, save func(tx core.App, member *core.Record) error) (
	*core.Record, []string, error) {
	var member *core.Record
	var codes []string
	err := app.RunInTransaction(func(tx core.App) error {
		link, m, err := findValidResetLink(tx, token)
		if err != nil {
			return err
		}
		member = m
		t := now().UTC()
		res, err := tx.DB().Update(store.ResetLinks, dbx.Params{"used_at": dbTime(t), store.FieldUpdated: dbTime(t)},
			dbx.And(dbx.HashExp{"id": link.Id, "used_at": "", "revoked_at": ""},
				dbx.NewExp("expires_at > {:now}", dbx.Params{"now": dbTime(t)}))).Execute()
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return ErrInvalidResetLink
		}
		if _, err := tx.DB().Delete(store.Credentials, dbx.HashExp{"member": member.Id}).Execute(); err != nil {
			return err
		}
		if err := save(tx, member); err != nil {
			return err
		}
		if codes, err = NewRecoveryCodes(tx, member); err != nil {
			return err
		}
		if err := writeAudit(tx, member, AuditUseResetLink, store.ResetLinks, link.GetString(store.FieldUUID),
			map[string]any{"method": method}); err != nil {
			return err
		}
		if _, err := RevokeSessions(tx, member, member, RevokeCredentialsReset); err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, ErrInvalidResetLink) {
		return nil, nil, err
	}
	if err != nil {
		return nil, nil, fmt.Errorf("auth: reset: %w", err)
	}
	return member, codes, nil
}

// startResetPassword starts setting up password and TOTP with a reset link.
func startResetPassword(ctx context.Context, app core.App, token, password string) (*PasswordEnrolmentStart, error) {
	_, member, err := findValidResetLink(app, token)
	if err != nil {
		return nil, err
	}
	return startMemberEnrolment(ctx, app, resetEnrolmentAAD, hashToken(token), member, password, true)
}

// confirmResetPassword completes the reset with the password and TOTP of [startResetPassword].
func confirmResetPassword(app core.App, token, enrolment, code string) (*core.Record, []string, error) {
	_, member, err := findValidResetLink(app, token)
	if err != nil {
		return nil, nil, err
	}
	c, err := openMemberEnrolment(app, resetEnrolmentAAD, hashToken(token), member, enrolment, code)
	if err != nil {
		return nil, nil, err
	}
	return useResetLink(app, token, MethodPasswordTOTP, func(tx core.App, m *core.Record) error {
		if m.Id != member.Id {
			return ErrInvalidResetLink
		}
		// No passkey is left: the user handle goes too, and a passkey added later gets a new one.
		if _, err := tx.DB().Update(store.Members, dbx.Params{"passkey_user_handle": "",
			store.FieldUpdated: dbTime(now())}, dbx.HashExp{"id": m.Id}).Execute(); err != nil {
			return err
		}
		cred, err := savePasswordCredential(tx, m, c)
		if err != nil {
			return err
		}
		return writeAudit(tx, m, AuditCredentialAdd, store.Members, m.GetString(store.FieldUUID),
			map[string]any{"kind": CredentialPasswordTOTP, "credential": cred.GetString(store.FieldUUID), "via": viaReset})
	})
}

// beginResetPasskey starts setting up a passkey with a reset link, for a new user handle.
func beginResetPasskey(app core.App, token string) (*protocol.CredentialCreation, string, error) {
	_, member, err := findValidResetLink(app, token)
	if err != nil {
		return nil, "", err
	}
	handle, err := newUserHandle()
	if err != nil {
		return nil, "", err
	}
	return beginMemberPasskey(app, ceremonyResetPasskey, hashToken(token), member, handle, false)
}

// finishResetPasskey completes the reset with the passkey of [beginResetPasskey].
func finishResetPasskey(app core.App, token, state string, response []byte) (*core.Record, []string, error) {
	_, member, err := findValidResetLink(app, token)
	if err != nil {
		return nil, nil, err
	}
	cred, handle, err := finishMemberPasskey(app, ceremonyResetPasskey, hashToken(token), member, state, response)
	if err != nil {
		return nil, nil, err
	}
	return useResetLink(app, token, MethodPasskey, func(tx core.App, m *core.Record) error {
		if m.Id != member.Id {
			return ErrInvalidResetLink
		}
		return addPasskey(tx, m, cred, handle, "", true, viaReset)
	})
}
