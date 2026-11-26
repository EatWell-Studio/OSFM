package auth

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Invites (D1, D2). Nobody joins without an invite: a maintainer creates one, which yields a one-time code; the code
// goes to the new member as a link, and registration consumes it. An invite row stores only the SHA-256 hash of its
// code (N5), the inviter, the expiry and, once used, the new member and the time.
//
//   - A code is 256 bits from crypto/rand, base64url-encoded (43 characters), the same form as a session token. With
//     that much randomness a plain hash is enough: guessing a code is hopeless however often one tries, and the
//     database alone yields no usable code.
//   - A code is looked up by its hash (unique index); the code itself is never compared, so the lookup reveals nothing
//     about it through timing. The hash found is compared once more in constant time.
//   - An invite is valid while it is neither used nor revoked nor expired and its inviter may still sign in. Every
//     invalid code, whether it never existed, expired, was used or was revoked, gives the same answer ([ErrInvalidInvite],
//     and the same response from the check endpoint), so that holding an old link tells nothing about what happened to it.
//   - Registration consumes an invite with [RegisterWithInvite], inside the transaction that creates the member; a
//     conditional update makes sure that of two registrations with the same code exactly one succeeds.

// Invite validity: the maintainer chooses a number of days within these bounds when creating an invite (D2).
const (
	DefaultInviteDays = 7
	MinInviteDays     = 1
	MaxInviteDays     = 30
)

// States of an invite, as the maintainers' list shows them.
const (
	InviteActive  = "active"
	InviteUsed    = "used"
	InviteExpired = "expired"
	InviteRevoked = "revoked"
)

// Audit log actions of the invites.
const (
	AuditCreateInvite = "invites.create"
	AuditRevokeInvite = "invites.revoke"
	// AuditRevokeInvites: the open invites of a member who left, was removed or was deleted were revoked.
	AuditRevokeInvites = "invites.revoke_all"
)

// ErrInvalidInvite is returned for every invite code that cannot be used: malformed, unknown, expired, used, revoked,
// or from an inviter who may no longer sign in. Deliberately one error: callers must not tell these cases apart.
var ErrInvalidInvite = errors.New("auth: invalid or expired invite")

// ErrInviteUsed is returned by [RevokeInvite] for an invite that has been used: it cannot be revoked any more.
var ErrInviteUsed = errors.New("auth: the invite has been used")

// errInviteDays is returned by [CreateInvite] for a validity outside [MinInviteDays, MaxInviteDays].
var errInviteDays = fmt.Errorf("auth: an invite is valid for %d to %d days", MinInviteDays, MaxInviteDays)

// CreateInvite creates an invite valid for days days (within [MinInviteDays, MaxInviteDays]) on behalf of inviter, a
// maintainer, and writes an audit_log entry. It returns the code, which exists only in this return value: the
// database keeps only its hash, so the caller must hand it out now. Only maintainers create invites in M1; the route
// checks the role, this function does not.
func CreateInvite(app core.App, inviter *core.Record, days int) (code string, invite *core.Record, err error) {
	if !isMember(inviter) {
		return "", nil, ErrNotMember
	}
	if days < MinInviteDays || days > MaxInviteDays {
		return "", nil, errInviteDays
	}
	if code, err = newToken(); err != nil {
		return "", nil, err
	}
	err = app.RunInTransaction(func(tx core.App) error {
		c, err := tx.FindCachedCollectionByNameOrId(store.Invites)
		if err != nil {
			return err
		}
		invite = core.NewRecord(c)
		invite.Set("code_hash", hashToken(code))
		invite.Set("created_by", inviter.Id)
		invite.Set("expires_at", now().UTC().Add(time.Duration(days)*24*time.Hour))
		if err := tx.Save(invite); err != nil {
			return err
		}
		return writeAudit(tx, inviter, AuditCreateInvite, store.Invites, invite.GetString(store.FieldUUID),
			map[string]any{"days": days, "expiresAt": invite.GetDateTime("expires_at").Time().Format(time.RFC3339)})
	})
	if err != nil {
		return "", nil, fmt.Errorf("auth: create invite: %w", err)
	}
	return code, invite, nil
}

// RevokeInvite revokes invite on behalf of actor and writes an audit_log entry; invite is reloaded with its current
// state. Revoking an invite that is already revoked changes nothing and returns nil; an invite that has been used
// returns [ErrInviteUsed]. The revocation is a conditional update, so it cannot overlap with a registration consuming
// the same invite: one of the two wins.
func RevokeInvite(app core.App, invite, actor *core.Record) error {
	if invite == nil || invite.Collection().Name != store.Invites {
		return errors.New("auth: not an invite record")
	}
	err := app.RunInTransaction(func(tx core.App) error {
		t := dbTime(now())
		res, err := tx.DB().Update(store.Invites, dbx.Params{"revoked_at": t, store.FieldUpdated: t},
			dbx.HashExp{"id": invite.Id, "used_at": "", "revoked_at": ""}).Execute()
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		current, err := tx.FindRecordById(store.Invites, invite.Id)
		if err != nil {
			return err
		}
		invite.Load(current.FieldsData())
		switch {
		case n == 1:
			return writeAudit(tx, actor, AuditRevokeInvite, store.Invites, invite.GetString(store.FieldUUID), nil)
		case !current.GetDateTime("used_at").IsZero():
			return ErrInviteUsed
		default:
			return nil // revoked already
		}
	})
	if errors.Is(err, ErrInviteUsed) {
		return err
	}
	if err != nil {
		return fmt.Errorf("auth: revoke invite: %w", err)
	}
	return nil
}

// InviteState returns the state of invite at time t: used, revoked, expired or active, in that order of precedence.
func InviteState(invite *core.Record, t time.Time) string {
	switch {
	case !invite.GetDateTime("used_at").IsZero():
		return InviteUsed
	case !invite.GetDateTime("revoked_at").IsZero():
		return InviteRevoked
	case !invite.GetDateTime("expires_at").Time().After(t):
		return InviteExpired
	default:
		return InviteActive
	}
}

// findValidInvite returns the invite the code names, with its inviter, if it can be used now; otherwise
// [ErrInvalidInvite]. Other errors are database failures.
func findValidInvite(app core.App, code string) (invite, inviter *core.Record, err error) {
	if !wellFormed(code) {
		return nil, nil, ErrInvalidInvite
	}
	hash := hashToken(code)
	invite, err = app.FindFirstRecordByData(store.Invites, "code_hash", hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrInvalidInvite
	}
	if err != nil {
		return nil, nil, err
	}
	// The row was found by the hash, so this cannot fail; it keeps every comparison of secret-derived values constant
	// in time.
	if subtle.ConstantTimeCompare([]byte(invite.GetString("code_hash")), []byte(hash)) != 1 {
		return nil, nil, ErrInvalidInvite
	}
	if InviteState(invite, now().UTC()) != InviteActive {
		return nil, nil, ErrInvalidInvite
	}
	// store.Query leaves out soft-deleted members.
	inviter, err = store.Query(app, store.Members).
		Where(dbx.HashExp{store.Members + ".id": invite.GetString("created_by")}).One()
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrInvalidInvite
	}
	if err != nil {
		return nil, nil, err
	}
	if !CanSignIn(inviter) {
		return nil, nil, ErrInvalidInvite
	}
	return invite, inviter, nil
}

// CheckInvite reports whether code names an invite that can be used now. It returns false, not an error, for every
// kind of invalid code; an error means the database failed.
func CheckInvite(app core.App, code string) (bool, error) {
	_, _, err := findValidInvite(app, code)
	if errors.Is(err, ErrInvalidInvite) {
		return false, nil
	}
	return err == nil, err
}

// Registration fills in and saves a new member inside the registration transaction. member is a new, unsaved record
// of the members collection with role member, invited_by (the inviter) and joined_at already set. The function sets
// the member's own fields (login name, display name), saves the member with tx.Save and then saves the member's
// credentials and recovery codes, all through tx.
//
// It must use tx, never the app it was handed elsewhere, for every read and write: the transaction holds the
// database's single write connection, so a write through another handle would wait for it forever. Work that takes
// long and needs no database (password hashing, verifying a WebAuthn attestation) belongs before
// [RegisterWithInvite], not in here. Returning an error rolls back everything, the invite stays unused.
type Registration func(tx core.App, member *core.Record) error

// RegisterWithInvite creates a member with the invite that code names: the only way to create a member through the
// API (D1). In one transaction it
//
//  1. finds the invite and checks that it can be used ([ErrInvalidInvite] otherwise, for every reason alike);
//  2. prepares a new member record (role member, invited_by the inviter, joined_at now) and calls register, which
//     fills it in and saves it with the member's credentials;
//  3. checks that register saved that record, and that it still is a member invited by the inviter;
//  4. consumes the invite with a conditional update (used_by, used_at; only while it is still unused, not revoked
//     and not expired), which fails with [ErrInvalidInvite] if another registration or a revocation got there first.
//
// Any failure rolls back the whole transaction: no member, no credentials, and the invite stays as it was. It returns
// the saved member. An invite never creates a maintainer.
func RegisterWithInvite(app core.App, code string, register Registration) (*core.Record, error) {
	var member *core.Record
	err := app.RunInTransaction(func(tx core.App) error {
		invite, inviter, err := findValidInvite(tx, code)
		if err != nil {
			return err
		}
		t := now().UTC()
		if member, err = NewMemberRecord(tx); err != nil {
			return err
		}
		member.Set("role", RoleMember)
		member.Set("invited_by", inviter.Id)
		member.Set("joined_at", t)
		if err := register(tx, member); err != nil {
			return err
		}

		saved, err := tx.FindRecordById(store.Members, member.Id)
		if err != nil {
			return fmt.Errorf("the registration did not save the member: %w", err)
		}
		if saved.GetString("role") != RoleMember || saved.GetString("invited_by") != inviter.Id || !CanSignIn(saved) {
			return errors.New("the registration changed the member's role or inviter")
		}

		res, err := tx.DB().Update(store.Invites, dbx.Params{
			"used_by": member.Id, "used_at": dbTime(t), store.FieldUpdated: dbTime(t),
		}, dbx.And(
			dbx.HashExp{"id": invite.Id, "used_at": "", "revoked_at": ""},
			dbx.NewExp("expires_at > {:now}", dbx.Params{"now": dbTime(t)}),
		)).Execute()
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return ErrInvalidInvite
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalidInvite) {
			return nil, ErrInvalidInvite
		}
		return nil, fmt.Errorf("auth: register with invite: %w", err)
	}
	return member, nil
}

// revokeOpenInvites revokes the invites of inviter that are neither used nor revoked, expired ones included, and
// writes an audit_log entry when there were any. [Bind] calls it when a member leaves, is removed or is deleted: their
// links must stop working (and [findValidInvite] refuses them in any case).
func revokeOpenInvites(app core.App, inviter *core.Record, reason string) error {
	t := dbTime(now())
	res, err := app.DB().Update(store.Invites, dbx.Params{"revoked_at": t, store.FieldUpdated: t},
		dbx.HashExp{"created_by": inviter.Id, "used_at": "", "revoked_at": ""}).Execute()
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	return writeAudit(app, nil, AuditRevokeInvites, store.Members, inviter.GetString(store.FieldUUID),
		map[string]any{"reason": reason, "invites": n})
}
