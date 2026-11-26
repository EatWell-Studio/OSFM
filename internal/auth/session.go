package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Sessions (AD6). A member's session is a row of the sessions collection; the browser holds a random token for it in
// the session cookie, and the row stores only the token's SHA-256 hash. The token carries nothing: no member ID, no
// PocketBase record ID, no expiry. A request has a session when its cookie's token hashes to a row that has not
// expired and whose member may still sign in ([CanSignIn]); everything else about a session lives on the server, so
// that revoking it is deleting its row:
//
//   - logout deletes the current session ([SignOut]);
//   - a credential reset, or anything else that must end every session of a member, calls [RevokeSessions];
//   - when a member leaves, is removed or is soft-deleted, their sessions are deleted by a record hook ([Bind]), and
//     the session check refuses them in any case.
//
// Sessions last [SessionLifetime] from sign-in; they are not extended by use.

// Names of the session cookie. An instance served over HTTPS at a public host name uses [HostCookieName]
// ([Options].HostPrefix); otherwise, local development included, [CookieName].
//
// The __Host- prefix makes browsers accept the cookie only with Secure, Path=/ and no Domain, and refuse any cookie of
// that name that a sibling subdomain tries to set for the parent domain (cookie tossing: planting a session of the
// attacker's own, or shadowing the member's). It is not used for local development because WebKit (Safari) refuses
// __Host- cookies over plain http://localhost and http://127.0.0.1, while it accepts the same cookie without the
// prefix; Chromium accepts both there.
const (
	CookieName     = "osfm_session"
	HostCookieName = "__Host-" + CookieName
)

// cookieNameKey is the app store key of the session cookie's name, set by [Bind].
const cookieNameKey = "osfmSessionCookieName"

// SessionCookieName returns the name of the session cookie on app: [HostCookieName] when [Bind] was given
// [Options].HostPrefix, else [CookieName].
func SessionCookieName(app core.App) string {
	if name, ok := app.Store().Get(cookieNameKey).(string); ok {
		return name
	}
	return CookieName
}

// SessionLifetime is how long a session lasts from sign-in.
const SessionLifetime = 30 * 24 * time.Hour

// tokenBytes is the size of a session token's random value: 256 bits.
const tokenBytes = 32

// Reasons for [RevokeSessions], recorded in the audit log.
const (
	RevokeCredentialsReset = "credentials_reset" // the member's credentials were reset (D5)
	RevokeLeft             = "left"              // the member left or was removed (left_at set; D7, D9)
	RevokeDeleted          = "deleted"           // the member's record was soft-deleted
)

// AuditRevokeSessions is the audit_log action of [RevokeSessions].
const AuditRevokeSessions = "sessions.revoke_all"

// errNoSession means that a request's token does not name a valid session.
var errNoSession = errors.New("auth: no valid session")

// now is the clock of the session code; tests replace it.
var now = time.Now

// newToken returns a new random session token: 32 bytes from the system's secure random source, base64url-encoded
// without padding (43 characters).
func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the hex SHA-256 hash of a token, the form sessions are stored and looked up by. A token is 256
// random bits, so a plain hash is enough: there is nothing to guess, and the database alone gives no usable token.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// wellFormed reports whether token can be a session token, before any database lookup.
func wellFormed(token string) bool {
	b, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(b) == tokenBytes
}

// CreateSession creates a session for member and returns its token, the cookie value, together with the session
// record. It fails with [ErrCannotSignIn] for a member who may not sign in. The sign-in endpoints use [SignIn], which
// also sets the cookie.
func CreateSession(app core.App, member *core.Record) (token string, session *core.Record, err error) {
	if !isMember(member) {
		return "", nil, ErrNotMember
	}
	if !CanSignIn(member) {
		return "", nil, ErrCannotSignIn
	}
	c, err := app.FindCachedCollectionByNameOrId(store.Sessions)
	if err != nil {
		return "", nil, err
	}
	if token, err = newToken(); err != nil {
		return "", nil, err
	}
	t := now().UTC()
	session = core.NewRecord(c)
	session.Set("member", member.Id)
	session.Set("token_hash", hashToken(token))
	session.Set("expires_at", t.Add(SessionLifetime))
	session.Set("last_seen_at", day(t))
	if err := app.Save(session); err != nil {
		return "", nil, fmt.Errorf("auth: save session: %w", err)
	}
	return token, session, nil
}

// lookup returns the session the token names and its member, or errNoSession. A session found expired, or whose
// member may no longer sign in, is deleted on the way. Other errors are database failures.
func lookup(app core.App, token string) (session, member *core.Record, err error) {
	if !wellFormed(token) {
		return nil, nil, errNoSession
	}
	session, err = app.FindFirstRecordByData(store.Sessions, "token_hash", hashToken(token))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, errNoSession
	}
	if err != nil {
		return nil, nil, err
	}

	t := now().UTC()
	if !session.GetDateTime("expires_at").Time().After(t) {
		deleteSession(app, session)
		return nil, nil, errNoSession
	}
	// store.Query leaves out soft-deleted members.
	member, err = store.Query(app, store.Members).Where(dbx.HashExp{store.Members + ".id": session.GetString("member")}).One()
	if errors.Is(err, sql.ErrNoRows) {
		deleteSession(app, session)
		return nil, nil, errNoSession
	}
	if err != nil {
		return nil, nil, err
	}
	if !CanSignIn(member) {
		deleteSession(app, session)
		return nil, nil, errNoSession
	}

	// The day of the last use, at most one write per session and day.
	if today := day(t); session.GetDateTime("last_seen_at").Time().Before(today) {
		_, err := app.DB().Update(store.Sessions, dbx.Params{"last_seen_at": dbTime(today)},
			dbx.HashExp{"id": session.Id}).Execute()
		if err != nil {
			app.Logger().Warn("could not record the day a session was last used", "error", err)
		}
	}
	return session, member, nil
}

// dbTime returns t as PocketBase stores dates ("2006-01-02 15:04:05.000Z", UTC), for SQL comparisons and updates.
func dbTime(t time.Time) string {
	d, _ := types.ParseDateTime(t) // never fails for a time.Time
	return d.String()
}

// day returns the start of t's UTC day.
func day(t time.Time) time.Time {
	return t.UTC().Truncate(24 * time.Hour)
}

// deleteSession deletes a session found invalid. Failing to delete it only leaves a row that is refused anyway, and
// that the daily cleanup removes, so the failure is logged rather than returned.
func deleteSession(app core.App, session *core.Record) {
	if _, err := app.DB().Delete(store.Sessions, dbx.HashExp{"id": session.Id}).Execute(); err != nil {
		app.Logger().Warn("could not delete an invalid session", "error", err)
	}
}

// RevokeSessions deletes every session of member, so that each of them fails from the next request on, and writes an
// audit_log entry: actor (nil when the member's own action or the system caused it), the member as target, the reason
// (one of the Revoke* constants) and the number of sessions. It returns that number. It runs in a transaction, or in
// the caller's when app is a transaction's app.
//
// The credential reset (D5) calls it; leaving, removal and soft deletion are handled by the hook [Bind] installs.
func RevokeSessions(app core.App, member, actor *core.Record, reason string) (int64, error) {
	if !isMember(member) {
		return 0, ErrNotMember
	}
	var n int64
	err := app.RunInTransaction(func(tx core.App) error {
		res, err := tx.DB().Delete(store.Sessions, dbx.HashExp{"member": member.Id}).Execute()
		if err != nil {
			return err
		}
		if n, err = res.RowsAffected(); err != nil {
			return err
		}
		return writeAudit(tx, actor, AuditRevokeSessions, store.Members, member.GetString(store.FieldUUID),
			map[string]any{"reason": reason, "sessions": n})
	})
	if err != nil {
		return 0, fmt.Errorf("auth: revoke sessions: %w", err)
	}
	return n, nil
}

// DeleteExpiredSessions deletes the sessions that have expired and returns how many. Expired sessions are refused
// whether or not their rows exist; this keeps the table small. [Bind] runs it daily.
func DeleteExpiredSessions(app core.App) (int64, error) {
	res, err := app.DB().Delete(store.Sessions, dbx.NewExp("expires_at <= {:now}",
		dbx.Params{"now": dbTime(now())})).Execute()
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// writeAudit appends an audit_log entry: who (actor, nil for the system or the member's own action), what (action),
// about which object (the collection name and the uuid of the target) and details.
func writeAudit(app core.App, actor *core.Record, action, targetType, targetUUID string, details map[string]any) error {
	c, err := app.FindCachedCollectionByNameOrId(store.AuditLog)
	if err != nil {
		return err
	}
	r := core.NewRecord(c)
	if actor != nil {
		r.Set("actor", actor.Id)
	}
	r.Set("action", action)
	r.Set("target_type", targetType)
	r.Set("target_uuid", targetUUID)
	r.Set("details", details)
	return app.Save(r)
}

// sessionCookie returns the session cookie called name for token, valid until expires. HttpOnly keeps it from
// scripts; Secure keeps it off plain HTTP (browsers accept Secure cookies from http://localhost and http://127.0.0.1,
// so local development works); SameSite=Lax keeps it off cross-site subrequests and form posts. Path=/ and no Domain:
// the cookie goes to this host only, the tile requests included (and the __Host- prefix requires both).
func sessionCookie(name, token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    token,
		Path:     "/",
		Expires:  expires.UTC(),
		MaxAge:   max(1, int(expires.Sub(now()).Seconds())),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

// clearedCookie returns a cookie that makes the browser drop the session cookie called name.
func clearedCookie(name string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}
