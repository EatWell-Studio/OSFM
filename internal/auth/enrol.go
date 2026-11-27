package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Enrolling a new credential for a member who exists already: on the "me" page (credentials.go), in the recovery of a
// passkey-only member (recover.go) and with a reset link (reset.go). The steps are those of the registration, with the
// member's login name in place of a new one:
//
//   - password and TOTP: the start step checks the password rules, hashes the password and generates a TOTP secret,
//     and seals both, with the member's login name and what entitles the enrolment (bindHash), into a pending enrolment
//     ([passwordEnrolment]) for the client to send back with one code from the authenticator app. Replacing only the
//     TOTP secret uses the same enrolment without a password.
//   - passkey: creation options for the member's user handle, excluding the passkeys the member has, and a ceremony
//     state that binds the same things.
//
// Each use has its own associated data (pending enrolments) or ceremony kind (passkeys), so that what was started for
// one cannot finish another.

// Associated data of the pending enrolments of existing members.
var (
	settingsEnrolmentAAD = []byte("password+totp enrolment, member settings")
	totpReplaceAAD       = []byte("totp replacement, member settings")
	recoveryEnrolmentAAD = []byte("password+totp enrolment, recovery")
	resetEnrolmentAAD    = []byte("password+totp enrolment, reset link")
)

// Ceremony kinds of passkey enrolments of existing members.
const (
	ceremonyAddPasskey     = "passkey.add"
	ceremonyRecoverPasskey = "passkey.recover"
	ceremonyResetPasskey   = "passkey.reset"
)

// CredentialPasskey is the credentials.kind of a passkey.
const CredentialPasskey = "passkey"

// MaxDeviceNameLength is the length of a passkey's device name, in characters.
const MaxDeviceNameLength = 64

// errDeviceNameInvalid: a device name that is too long or has control characters.
var errDeviceNameInvalid = errors.New("auth: invalid device name")

// normalizeDeviceName returns a passkey's device name as stored: trimmed; empty is allowed.
func normalizeDeviceName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxDeviceNameLength {
		return "", errDeviceNameInvalid
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", errDeviceNameInvalid
		}
	}
	return s, nil
}

// memberBindHash is what the enrolments started on the "me" page are bound to: the member.
func memberBindHash(member *core.Record) string {
	return hashToken("member " + member.GetString(store.FieldUUID))
}

// startMemberEnrolment is the start step of a password and TOTP enrolment for member: with withPassword, the password
// is checked and hashed; without, only a new TOTP secret is made (replacing the TOTP secret). The issuer is the
// instance's name.
func startMemberEnrolment(ctx context.Context, app core.App, aad []byte, bindHash string, member *core.Record,
	password string, withPassword bool) (*PasswordEnrolmentStart, error) {
	login := member.GetString("login_name")
	var hash string
	if withPassword {
		if err := checkPassword(password, login); err != nil {
			return nil, err
		}
		var err error
		if hash, err = hashPassword(ctx, password); err != nil {
			return nil, err
		}
	}
	secret, err := newTOTPSecret()
	if err != nil {
		return nil, err
	}
	key, err := totpKey(secret, instanceName(app), login)
	if err != nil {
		return nil, err
	}
	p := &passwordEnrolment{
		Expires:      unixAfter(enrolmentLifetime),
		InviteHash:   bindHash,
		LoginName:    login,
		Member:       member.GetString(store.FieldUUID),
		PasswordHash: hash,
		TOTPSecret:   secret,
	}
	sealed, err := sealJSON(app, purposeEnrolment, p, aad)
	if err != nil {
		return nil, err
	}
	return &PasswordEnrolmentStart{Enrolment: sealed, Secret: key.Secret(), OTPAuthURI: key.URL(),
		Expires: time.Unix(p.Expires, 0).UTC()}, nil
}

// confirmedEnrolment is a pending enrolment of member whose TOTP code matched.
type confirmedEnrolment struct {
	passwordHash string // empty for a TOTP replacement
	secretEnc    string // the TOTP secret encrypted for the member
	step         int64  // the confirming code's time step, used up
}

// openMemberEnrolment opens a pending enrolment started by [startMemberEnrolment] for member with aad and bindHash and
// checks the TOTP code. Errors: errEnrolmentInvalid, errCodeInvalid.
func openMemberEnrolment(app core.App, aad []byte, bindHash string, member *core.Record, enrolment,
	code string) (*confirmedEnrolment, error) {
	var p passwordEnrolment
	if err := openJSON(app, purposeEnrolment, enrolment, aad, &p); err != nil {
		if errors.Is(err, errSealed) {
			return nil, errEnrolmentInvalid
		}
		return nil, err
	}
	uuid := member.GetString(store.FieldUUID)
	if subtle.ConstantTimeCompare([]byte(p.InviteHash), []byte(bindHash)) != 1 || p.Member != uuid ||
		p.LoginName != member.GetString("login_name") {
		return nil, errEnrolmentInvalid
	}
	step, ok := matchTOTP(p.TOTPSecret, code, now(), 0)
	if !ok {
		return nil, errCodeInvalid
	}
	secretEnc, err := sealTOTPSecret(app, uuid, p.TOTPSecret)
	if err != nil {
		return nil, err
	}
	return &confirmedEnrolment{passwordHash: p.PasswordHash, secretEnc: secretEnc, step: step}, nil
}

// savePasswordCredential stores a confirmed enrolment with a password as member's password and TOTP credential.
func savePasswordCredential(tx core.App, member *core.Record, c *confirmedEnrolment) (*core.Record, error) {
	col, err := tx.FindCachedCollectionByNameOrId(store.Credentials)
	if err != nil {
		return nil, err
	}
	cred := core.NewRecord(col)
	cred.Set("member", member.Id)
	cred.Set("kind", CredentialPasswordTOTP)
	cred.Set("password_hash", c.passwordHash)
	cred.Set("totp_secret_enc", c.secretEnc)
	cred.Set("totp_last_step", c.step)
	return cred, tx.Save(cred)
}

// decodeHandle decodes a stored passkey user handle.
func decodeHandle(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// newUserHandle returns a new random passkey user handle.
func newUserHandle() ([]byte, error) {
	handle := make([]byte, userHandleBytes)
	if _, err := rand.Read(handle); err != nil {
		return nil, err
	}
	return handle, nil
}

// beginMemberPasskey is the first request of a passkey enrolment for member, of the ceremony kind and bound to
// bindHash. handle is the user handle the passkey is created with; with exclude, the member's passkeys are listed
// so that an authenticator holding one of them does not create a second one.
func beginMemberPasskey(app core.App, kind, bindHash string, member *core.Record, handle []byte, exclude bool) (
	*protocol.CredentialCreation, string, error) {
	p, err := passkeysOf(app)
	if err != nil {
		return nil, "", err
	}
	user := &passkeyUser{handle: handle, name: member.GetString("login_name"),
		displayName: member.GetString("display_name")}
	var opts []webauthn.RegistrationOption
	if exclude {
		_, creds, err := memberPasskeys(app, member)
		if err != nil {
			return nil, "", err
		}
		list := make([]protocol.CredentialDescriptor, 0, len(creds))
		for i := range creds {
			list = append(list, creds[i].Descriptor())
		}
		opts = append(opts, webauthn.WithExclusions(list))
	}
	options, session, err := p.wa.BeginRegistration(user, opts...)
	if err != nil {
		return nil, "", fmt.Errorf("auth: begin passkey registration: %w", err)
	}
	state, err := p.states.seal(kind, ceremonyTTL, registrationState{
		InviteHash: bindHash, Member: member.GetString(store.FieldUUID), UserHandle: handle, Session: *session,
	})
	if err != nil {
		return nil, "", err
	}
	return options, state, nil
}

// finishMemberPasskey verifies the second request of a passkey enrolment begun by [beginMemberPasskey] for member
// with the same kind and bindHash. It returns the verified credential and the user handle it was created with.
// [ErrPasskeyFailed] for every failure.
func finishMemberPasskey(app core.App, kind, bindHash string, member *core.Record, state string, response []byte) (
	*webauthn.Credential, []byte, error) {
	p, err := passkeysOf(app)
	if err != nil {
		return nil, nil, err
	}
	var st registrationState
	if err := p.states.open(kind, state, &st); err != nil {
		return nil, nil, ErrPasskeyFailed
	}
	if subtle.ConstantTimeCompare([]byte(st.InviteHash), []byte(bindHash)) != 1 ||
		st.Member != member.GetString(store.FieldUUID) {
		return nil, nil, ErrPasskeyFailed
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrPasskeyFailed, err)
	}
	user := &passkeyUser{handle: st.UserHandle, name: member.GetString("login_name"),
		displayName: member.GetString("display_name")}
	cred, err := p.wa.CreateCredential(user, st.Session, parsed)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrPasskeyFailed, err)
	}
	return cred, st.UserHandle, nil
}

// claimUserHandle makes handle the member's passkey user handle inside tx: it sets it when the member has none, and
// fails with [ErrPasskeyFailed] when the member has another one (a passkey created for the old handle would not sign
// in). With replace, the member's handle is replaced whatever it was (a reset).
func claimUserHandle(tx core.App, member *core.Record, handle []byte, replace bool) error {
	encoded := base64.RawURLEncoding.EncodeToString(handle)
	current, err := tx.FindRecordById(store.Members, member.Id)
	if err != nil {
		return err
	}
	switch existing := current.GetString("passkey_user_handle"); {
	case existing == encoded:
		return nil
	case existing != "" && !replace:
		return fmt.Errorf("%w: the member's user handle changed", ErrPasskeyFailed)
	}
	if _, err := tx.DB().Update(store.Members, dbx.Params{"passkey_user_handle": encoded,
		store.FieldUpdated: dbTime(now())}, dbx.HashExp{"id": member.Id}).Execute(); err != nil {
		return err
	}
	member.Set("passkey_user_handle", encoded)
	return nil
}

// memberHandle returns member's passkey user handle, or a new one when they have none yet (saved only with the
// passkey, by [claimUserHandle]).
func memberHandle(member *core.Record) ([]byte, error) {
	if h := member.GetString("passkey_user_handle"); h != "" {
		return decodeHandle(h)
	}
	return newUserHandle()
}
