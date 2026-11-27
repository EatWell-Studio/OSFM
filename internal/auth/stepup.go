package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Re-verification ("step-up", D3, D5; architecture §3.3). A session proves that its member passed both factors when
// signing in, possibly weeks ago, on a device that may since have been left unlocked. Changes that could lock the
// member out or hand the account to someone else need a fresh proof as well: adding or removing a credential, changing
// the password, replacing the TOTP secret, regenerating the recovery codes, and, for maintainers, creating a reset
// link for someone. A route that needs it carries [RequireFresh].
//
//   - Each session row records when its member last proved a factor on it (sessions.verified_at). Signing in sets it
//     (every factor was proved then), and so does a re-verification: with one of the member's own passkeys, or with
//     the password and a TOTP code or a recovery code, exactly as at sign-in (the TOTP step is used up, the recovery
//     code too).
//   - A session is fresh for [StepUpWindow] after that. A stale one gets 403 reauth_required from the routes that need
//     a fresh proof; the web app then asks for the proof and repeats the request.
//   - The proof belongs to the session, not to the member: re-verifying on one device does not make the member's other
//     sessions fresh.
//   - Wrong passwords and codes count towards the member's lockout of the second sign-in step (TOTP or recovery code,
//     [LimitStepUp]): a session does not give anyone more guesses than the sign-in does.

// StepUpWindow is how long a session counts as freshly verified after the sign-in or a re-verification.
const StepUpWindow = 10 * time.Minute

// CodeReauthRequired is the [AuthError] code of a request that needs a fresh proof (403).
const CodeReauthRequired = "reauth_required"

// RequireFreshMiddlewareID is the route middleware of [RequireFresh]; its priority is the default (0), after the
// session, CSRF and rate limit middlewares.
const RequireFreshMiddlewareID = "osfmRequireFresh"

// AuditVerify: a member re-verified on a session (actor: the member; details: method).
const AuditVerify = "sessions.verify"

// ceremonyVerify is the passkey ceremony kind of a re-verification.
const ceremonyVerify = "passkey.verify"

// Errors of the re-verification.
var (
	errNoPassword = errors.New("auth: the member has no password")
	errNoPasskey  = errors.New("auth: the member has no passkey")
)

// verifiedUntil returns when the session stops being fresh, or the zero time when it never was.
func verifiedUntil(session *core.Record) time.Time {
	if session == nil {
		return time.Time{}
	}
	v := session.GetDateTime("verified_at")
	if v.IsZero() {
		return time.Time{}
	}
	return v.Time().Add(StepUpWindow).UTC()
}

// fresh reports whether the request's session was verified within [StepUpWindow].
func fresh(e *core.RequestEvent) bool {
	return verifiedUntil(currentSession(e)).After(now())
}

// RequireFresh returns the route middleware of the routes that need a fresh proof: 401 without a session, 403 with
// [CodeReauthRequired] when the session was not verified within [StepUpWindow].
func RequireFresh() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: RequireFreshMiddlewareID,
		Func: func(e *core.RequestEvent) error {
			if CurrentMember(e) == nil {
				return e.UnauthorizedError("", nil)
			}
			if !fresh(e) {
				return authError(e, http.StatusForbidden, CodeReauthRequired, "Confirm it is you first.")
			}
			return e.Next()
		},
	}
}

// markVerified records now as the time the session was verified.
func markVerified(app core.App, session *core.Record) error {
	t := now().UTC()
	if _, err := app.DB().Update(store.Sessions, dbx.Params{"verified_at": dbTime(t)},
		dbx.HashExp{"id": session.Id}).Execute(); err != nil {
		return err
	}
	session.Set("verified_at", t)
	return nil
}

// verifyWithPassword checks member's password and a second factor, code: a TOTP code (6 digits, its time step used
// up) or a recovery code (used up). It returns the method for the audit log. Errors: errNoPassword, errBadCredentials
// (wrong password), errCodeInvalid.
func verifyWithPassword(ctx context.Context, app core.App, member *core.Record, password, code string) (string, error) {
	cred, err := findPasswordCredential(app, member)
	if err != nil {
		return "", err
	}
	if cred == nil {
		return "", errNoPassword
	}
	if passwordLength(password) > MaxPasswordLength {
		return "", errBadCredentials
	}
	ok, err := verifyPassword(ctx, cred.GetString("password_hash"), password)
	if errors.Is(err, errBadHash) {
		return "", errBadCredentials
	}
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errBadCredentials
	}
	if _, isTOTP := normalizeTOTPCode(code); isTOTP {
		secret, err := openTOTPSecret(app, member.GetString(store.FieldUUID), cred.GetString("totp_secret_enc"))
		if err != nil {
			return "", fmt.Errorf("auth: the TOTP secret of a credential does not decrypt: %w", err)
		}
		step, ok := matchTOTP(secret, code, now(), int64(cred.GetInt("totp_last_step")))
		if !ok {
			return "", errCodeInvalid
		}
		if ok, err := consumeTOTPStep(app, cred, step); err != nil {
			return "", err
		} else if !ok {
			return "", errCodeInvalid
		}
		return MethodPasswordTOTP, nil
	}
	ok, err = useRecoveryCode(app, member, code)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errCodeInvalid
	}
	return MethodRecoveryCode, nil
}

// verifyState is what the first request of a re-verification with a passkey decided.
type verifyState struct {
	Member  string               `json:"m"` // the member's uuid
	Session webauthn.SessionData `json:"s"`
}

// memberPasskeys returns the stored passkeys of member and them as go-webauthn's credentials.
func memberPasskeys(app core.App, member *core.Record) ([]*core.Record, []webauthn.Credential, error) {
	records, err := app.FindAllRecords(store.Credentials,
		dbx.HashExp{"member": member.Id, "kind": CredentialPasskey})
	if err != nil {
		return nil, nil, err
	}
	creds := make([]webauthn.Credential, 0, len(records))
	for _, r := range records {
		c, err := loadPasskey(r)
		if err != nil {
			return nil, nil, err
		}
		creds = append(creds, c)
	}
	return records, creds, nil
}

// memberUser returns member as go-webauthn's user, with their passkeys.
func memberUser(member *core.Record, creds []webauthn.Credential) (*passkeyUser, error) {
	handle, err := decodeHandle(member.GetString("passkey_user_handle"))
	if err != nil {
		return nil, err
	}
	return &passkeyUser{handle: handle, name: member.GetString("login_name"),
		displayName: member.GetString("display_name"), credentials: creds}, nil
}

// beginVerifyPasskey starts a re-verification with one of member's passkeys: the request options list them
// (allowCredentials) and require user verification. errNoPasskey when the member has none.
func beginVerifyPasskey(app core.App, member *core.Record) (*protocol.CredentialAssertion, string, error) {
	p, err := passkeysOf(app)
	if err != nil {
		return nil, "", err
	}
	_, creds, err := memberPasskeys(app, member)
	if err != nil {
		return nil, "", err
	}
	if len(creds) == 0 || member.GetString("passkey_user_handle") == "" {
		return nil, "", errNoPasskey
	}
	user, err := memberUser(member, creds)
	if err != nil {
		return nil, "", err
	}
	options, session, err := p.wa.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", fmt.Errorf("auth: begin passkey verification: %w", err)
	}
	state, err := p.states.seal(ceremonyVerify, ceremonyTTL,
		verifyState{Member: member.GetString(store.FieldUUID), Session: *session})
	if err != nil {
		return nil, "", err
	}
	return options, state, nil
}

// finishVerifyPasskey verifies the assertion against the state of [beginVerifyPasskey], begun for the same member,
// and records the passkey's use as a sign-in does. [ErrPasskeyFailed] for every failure.
func finishVerifyPasskey(app core.App, member *core.Record, state string, response []byte) error {
	p, err := passkeysOf(app)
	if err != nil {
		return err
	}
	var st verifyState
	if err := p.states.open(ceremonyVerify, state, &st); err != nil {
		return ErrPasskeyFailed
	}
	if st.Member != member.GetString(store.FieldUUID) {
		return ErrPasskeyFailed
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPasskeyFailed, err)
	}
	records, creds, err := memberPasskeys(app, member)
	if err != nil {
		return err
	}
	user, err := memberUser(member, creds)
	if err != nil {
		return err
	}
	cred, err := p.wa.ValidateLogin(user, st.Session, parsed)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPasskeyFailed, err)
	}
	for i, c := range creds {
		if string(c.ID) == string(cred.ID) {
			return recordPasskeyUse(app, member, records[i], cred)
		}
	}
	return ErrPasskeyFailed
}
