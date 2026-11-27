package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Registration and sign-in with password and TOTP (D3, D4; architecture §3.3).
//
// Registration takes two requests, and creates nothing until the second:
//
//  1. Start: invite code, login name, display name and password. The server checks the invite (one answer for every
//     invalid one), the login name (form, not taken: these errors are specific, and only someone holding a valid
//     invite gets that far) and the password rules, hashes the password, generates a TOTP secret and returns the
//     secret (and its otpauth:// URI, for the QR code) together with a pending enrolment: login name, display name,
//     password hash, TOTP secret, the hash of the invite code and an expiry, sealed with the master key
//     ([passwordEnrolment]). Nothing is stored and the invite is not consumed.
//  2. Confirm: the pending enrolment, the invite code and one code from the authenticator app. The server opens the
//     enrolment (so the client cannot have changed anything in it), checks that it was started with this invite and
//     that the code matches its TOTP secret, then creates the member through [RegisterWithInvite]: the member, the
//     credential (password hash; the TOTP secret encrypted for this member; the step of the confirming code as the
//     last used step) and the recovery codes, in one transaction that also consumes the invite. Then it starts a
//     session.
//
// There is no way past step 2 without a code from the secret of step 1, and an enrolment expires after
// [enrolmentLifetime]; an abandoned registration leaves nothing behind.
//
// Sign-in takes two requests too:
//
//  1. Password: login name and password. Every failure gives the same answer, and an unknown login name (or a member
//     without a password, or one who may not sign in) costs the same argon2id computation as a wrong password. On
//     success the answer is a sign-in ticket ([signInTicket]): the member's uuid, a fingerprint of the password hash
//     and an expiry, sealed with the master key. It is not a session and opens nothing but step 2; no cookie is set.
//  2. TOTP code, or recovery code, with the ticket. A TOTP time step is accepted once ([consumeTOTPStep]); a recovery
//     code is used up ([useRecoveryCode]). Only then is the session started ([SignIn]).
//
// A ticket lasts [ticketLifetime] and stops working when the password changes (the fingerprint) or the member may no
// longer sign in. Within its lifetime it can be retried with another code: it proves the password, which one could
// send again anyway. Limits on attempts per account belong to the rate limiting (T011).

// Lifetimes of the sealed values.
const (
	enrolmentLifetime = 15 * time.Minute
	ticketLifetime    = 5 * time.Minute
)

// Login names: the members.login_name field's rules, lowercase letters, digits, "_" and "-", 3 to 32 characters.
// What is entered is trimmed and lowercased first, at registration and at sign-in alike.
const (
	MinLoginNameLength = 3
	MaxLoginNameLength = 32
	// MaxDisplayNameLength is the members.display_name limit, in characters.
	MaxDisplayNameLength = 64
)

var loginNameRegexp = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Registration errors that the client is told about specifically.
var (
	errLoginNameInvalid   = errors.New("auth: invalid login name")
	errLoginNameTaken     = errors.New("auth: login name taken")
	errDisplayNameInvalid = errors.New("auth: invalid display name")
	errPasswordTooShort   = errors.New("auth: password too short")
	errPasswordTooLong    = errors.New("auth: password too long")
	errPasswordIsLogin    = errors.New("auth: password is the login name")
	// errCodeInvalid: the TOTP or recovery code does not match (or has been used).
	errCodeInvalid = errors.New("auth: invalid code")
	// errEnrolmentInvalid: the pending enrolment is malformed, changed, expired or from another invite.
	errEnrolmentInvalid = errors.New("auth: invalid or expired enrolment")
)

// Sign-in errors. Deliberately few: the client learns only that the password step failed, or that the second step
// did.
var (
	// errBadCredentials: unknown login name, wrong password, no password credential, or a member who may not sign in.
	errBadCredentials = errors.New("auth: wrong login name or password")
	// errTicketInvalid: the sign-in ticket is malformed, changed or expired, or its member or password changed.
	errTicketInvalid = errors.New("auth: invalid or expired sign-in ticket")
)

// CredentialPasswordTOTP is the credentials.kind of a password with TOTP.
const CredentialPasswordTOTP = "password_totp"

// NormalizeLoginName returns a login name as entered in the form it is stored and looked up in: trimmed, lowercase.
func NormalizeLoginName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func validLoginName(login string) bool {
	n := len(login) // ASCII only, when valid
	return n >= MinLoginNameLength && n <= MaxLoginNameLength && loginNameRegexp.MatchString(login)
}

// normalizeDisplayName returns the display name as stored: trimmed, the login name when empty; ok is false for a name
// that is too long or contains control or format characters.
func normalizeDisplayName(s, login string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return login, true
	}
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxDisplayNameLength {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", false
		}
	}
	return s, true
}

func checkPassword(password, login string) error {
	switch n := passwordLength(password); {
	case n < MinPasswordLength:
		return errPasswordTooShort
	case n > MaxPasswordLength:
		return errPasswordTooLong
	}
	if NormalizeLoginName(password) == login {
		return errPasswordIsLogin
	}
	return nil
}

// loginNameTaken reports whether a member, soft-deleted ones included, has the login name: login names are never
// reused (the unique index covers tombstones too).
func loginNameTaken(app core.App, login string) (bool, error) {
	_, err := app.FindFirstRecordByData(store.Members, "login_name", login)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// instanceName is the issuer shown in authenticator apps: the instance's name, or "OSFM" while it has none.
func instanceName(app core.App) string {
	settings, err := app.FindRecordById(store.InstanceSettings, store.SettingsID)
	if err == nil {
		if name := strings.TrimSpace(settings.GetString("name")); name != "" {
			return name
		}
	}
	return "OSFM"
}

// passwordEnrolment is the pending enrolment of a password and TOTP registration, sealed under purposeEnrolment.
type passwordEnrolment struct {
	Expires      int64  `json:"exp"`
	InviteHash   string `json:"inv"`  // hashToken of the invite code, or of the setup code (setup.go)
	LoginName    string `json:"ln"`   // normalised
	DisplayName  string `json:"dn"`   // normalised
	PasswordHash string `json:"pw"`   // PHC string
	TOTPSecret   []byte `json:"totp"` // raw secret
}

func (p *passwordEnrolment) expiry() int64 { return p.Expires }

// enrolmentAAD binds a pending enrolment to its purpose and format.
var enrolmentAAD = []byte("password+totp registration")

// PasswordEnrolmentStart is what the start of a registration returns: the pending enrolment for the confirm step,
// and the TOTP secret (base32, for typing in) with its otpauth:// URI (for the QR code).
type PasswordEnrolmentStart struct {
	Enrolment  string
	Secret     string
	OTPAuthURI string
	Expires    time.Time
}

// startPasswordRegistration is step 1 of the registration: see the comment at the top of this file. It returns
// [ErrInvalidInvite] for every unusable invite, or one of the specific registration errors.
func startPasswordRegistration(ctx context.Context, app core.App, inviteCode, loginName, displayName,
	password string) (*PasswordEnrolmentStart, error) {
	valid, err := CheckInvite(app, inviteCode)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, ErrInvalidInvite
	}
	return startPasswordEnrolment(ctx, app, enrolmentAAD, hashToken(inviteCode), loginName, displayName, password)
}

// startPasswordEnrolment is step 1 of a registration after the right to register has been checked: an invite here,
// the setup code for the first maintainer (setup.go). bindHash is the hash of that invite or setup code, sealed into
// the enrolment; aad tells the two kinds of enrolment apart.
func startPasswordEnrolment(ctx context.Context, app core.App, aad []byte, bindHash, loginName, displayName,
	password string) (*PasswordEnrolmentStart, error) {
	login := NormalizeLoginName(loginName)
	if !validLoginName(login) {
		return nil, errLoginNameInvalid
	}
	display, ok := normalizeDisplayName(displayName, login)
	if !ok {
		return nil, errDisplayNameInvalid
	}
	if err := checkPassword(password, login); err != nil {
		return nil, err
	}
	taken, err := loginNameTaken(app, login)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, errLoginNameTaken
	}

	hash, err := hashPassword(ctx, password)
	if err != nil {
		return nil, err
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
		DisplayName:  display,
		PasswordHash: hash,
		TOTPSecret:   secret,
	}
	sealed, err := sealJSON(app, purposeEnrolment, p, aad)
	if err != nil {
		return nil, err
	}
	return &PasswordEnrolmentStart{
		Enrolment:  sealed,
		Secret:     key.Secret(),
		OTPAuthURI: key.URL(),
		Expires:    time.Unix(p.Expires, 0).UTC(),
	}, nil
}

// confirmPasswordRegistration is step 2 of the registration: see the comment at the top of this file. It returns the
// new member and the recovery codes; the caller starts the session.
func confirmPasswordRegistration(app core.App, inviteCode, enrolment, code string) (*core.Record, []string, error) {
	register, codes, err := passwordRegistration(app, enrolmentAAD, hashToken(inviteCode), enrolment, code)
	if err != nil {
		return nil, nil, err
	}
	member, err := RegisterWithInvite(app, inviteCode, register)
	switch {
	case errors.Is(err, ErrInvalidInvite):
		return nil, nil, ErrInvalidInvite
	case errors.Is(err, errLoginNameTaken):
		return nil, nil, errLoginNameTaken
	case err != nil:
		return nil, nil, err
	}
	return member, *codes, nil
}

// passwordRegistration is step 2 of a registration up to the creation of the member: it opens the pending enrolment
// (sealed with aad, started with the invite or setup code whose hash is bindHash) and checks the TOTP code. It returns
// the [Registration] that saves the member, the credential and the recovery codes, and where it leaves the codes.
func passwordRegistration(app core.App, aad []byte, bindHash, enrolment, code string) (Registration, *[]string,
	error) {
	var p passwordEnrolment
	if err := openJSON(app, purposeEnrolment, enrolment, aad, &p); err != nil {
		if errors.Is(err, errSealed) {
			return nil, nil, errEnrolmentInvalid
		}
		return nil, nil, err
	}
	if subtle.ConstantTimeCompare([]byte(p.InviteHash), []byte(bindHash)) != 1 {
		return nil, nil, errEnrolmentInvalid
	}
	step, ok := matchTOTP(p.TOTPSecret, code, now(), 0)
	if !ok {
		return nil, nil, errCodeInvalid
	}

	memberUUID, err := store.NewUUID()
	if err != nil {
		return nil, nil, err
	}
	secretEnc, err := sealTOTPSecret(app, memberUUID, p.TOTPSecret)
	if err != nil {
		return nil, nil, err
	}
	var codes []string
	return func(tx core.App, member *core.Record) error {
		taken, err := loginNameTaken(tx, p.LoginName)
		if err != nil {
			return err
		}
		if taken {
			return errLoginNameTaken
		}
		member.Set(store.FieldUUID, memberUUID)
		member.Set("login_name", p.LoginName)
		member.Set("display_name", p.DisplayName)
		if err := tx.Save(member); err != nil {
			return err
		}
		c, err := tx.FindCachedCollectionByNameOrId(store.Credentials)
		if err != nil {
			return err
		}
		cred := core.NewRecord(c)
		cred.Set("member", member.Id)
		cred.Set("kind", CredentialPasswordTOTP)
		cred.Set("password_hash", p.PasswordHash)
		cred.Set("totp_secret_enc", secretEnc)
		// The confirming code's step counts as used: the same code cannot sign in afterwards.
		cred.Set("totp_last_step", step)
		if err := tx.Save(cred); err != nil {
			return err
		}
		codes, err = NewRecoveryCodes(tx, member)
		return err
	}, &codes, nil
}

// signInTicket is the sealed proof that the password step succeeded, under purposeTicket.
type signInTicket struct {
	Expires  int64  `json:"exp"`
	Member   string `json:"m"`  // the member's uuid
	Password string `json:"pw"` // passwordFingerprint of the credential's hash when the ticket was issued
}

func (t *signInTicket) expiry() int64 { return t.Expires }

var ticketAAD = []byte("password sign-in, second step")

// passwordFingerprint identifies a password hash without revealing it: a ticket stops working when the hash changes.
func passwordFingerprint(hash string) string {
	sum := sha256.Sum256([]byte(hash))
	return hex.EncodeToString(sum[:16])
}

// findPasswordCredential returns member's password and TOTP credential, or nil when they have none.
func findPasswordCredential(app core.App, member *core.Record) (*core.Record, error) {
	cred, err := app.FindFirstRecordByFilter(store.Credentials, "member = {:member} && kind = {:kind}",
		dbx.Params{"member": member.Id, "kind": CredentialPasswordTOTP})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return cred, err
}

// findSignInMember returns the member with the login name who may sign in, or nil.
func findSignInMember(app core.App, login string) (*core.Record, error) {
	member, err := store.Query(app, store.Members).Where(dbx.HashExp{store.Members + ".login_name": login}).One()
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !CanSignIn(member) {
		return nil, nil
	}
	return member, nil
}

// signInWithPassword is step 1 of the sign-in. It returns a sealed [signInTicket], or [errBadCredentials] for every
// kind of failure, after the same amount of work.
func signInWithPassword(ctx context.Context, app core.App, loginName, password string) (string, error) {
	login := NormalizeLoginName(loginName)
	var member, cred *core.Record
	var err error
	if validLoginName(login) && passwordLength(password) <= MaxPasswordLength {
		if member, err = findSignInMember(app, login); err != nil {
			return "", err
		}
		if member != nil {
			if cred, err = findPasswordCredential(app, member); err != nil {
				return "", err
			}
		}
	}
	if cred == nil {
		if err := verifyDummy(ctx, password); err != nil {
			return "", err
		}
		return "", errBadCredentials
	}
	hash := cred.GetString("password_hash")
	ok, err := verifyPassword(ctx, hash, password)
	if errors.Is(err, errBadHash) {
		app.Logger().Error("a password credential has a malformed hash", "member", member.GetString(store.FieldUUID))
		return "", errBadCredentials
	}
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errBadCredentials
	}
	return sealJSON(app, purposeTicket, &signInTicket{
		Expires:  unixAfter(ticketLifetime),
		Member:   member.GetString(store.FieldUUID),
		Password: passwordFingerprint(hash),
	}, ticketAAD)
}

// openTicket returns the member and credential a ticket names, if the ticket is valid and the member may still sign
// in with the same password; otherwise [errTicketInvalid].
func openTicket(app core.App, ticket string) (member, cred *core.Record, err error) {
	var t signInTicket
	if err := openJSON(app, purposeTicket, ticket, ticketAAD, &t); err != nil {
		if errors.Is(err, errSealed) {
			return nil, nil, errTicketInvalid
		}
		return nil, nil, err
	}
	if !store.IsUUID(t.Member) {
		return nil, nil, errTicketInvalid
	}
	member, err = store.FindByUUID(app, store.Members, t.Member)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, errTicketInvalid
	}
	if err != nil {
		return nil, nil, err
	}
	if !CanSignIn(member) {
		return nil, nil, errTicketInvalid
	}
	if cred, err = findPasswordCredential(app, member); err != nil {
		return nil, nil, err
	}
	if cred == nil || subtle.ConstantTimeCompare([]byte(passwordFingerprint(cred.GetString("password_hash"))),
		[]byte(t.Password)) != 1 {
		return nil, nil, errTicketInvalid
	}
	return member, cred, nil
}

// signInWithTOTP is step 2 of the sign-in with a TOTP code: it returns the member, whose session the caller starts.
func signInWithTOTP(app core.App, ticket, code string) (*core.Record, error) {
	member, cred, err := openTicket(app, ticket)
	if err != nil {
		return nil, err
	}
	secret, err := openTOTPSecret(app, member.GetString(store.FieldUUID), cred.GetString("totp_secret_enc"))
	if err != nil {
		return nil, fmt.Errorf("auth: the TOTP secret of a credential does not decrypt: %w", err)
	}
	step, ok := matchTOTP(secret, code, now(), int64(cred.GetInt("totp_last_step")))
	if !ok {
		return nil, errCodeInvalid
	}
	if ok, err := consumeTOTPStep(app, cred, step); err != nil {
		return nil, err
	} else if !ok {
		return nil, errCodeInvalid // used meanwhile by a concurrent request
	}
	return member, nil
}

// signInWithRecoveryCode is step 2 of the sign-in with a recovery code, which is used up: it returns the member,
// whose session the caller starts, and the number of recovery codes left.
func signInWithRecoveryCode(app core.App, ticket, code string) (*core.Record, int64, error) {
	member, _, err := openTicket(app, ticket)
	if err != nil {
		return nil, 0, err
	}
	ok, err := useRecoveryCode(app, member, code)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		return nil, 0, errCodeInvalid
	}
	left, err := RemainingRecoveryCodes(app, member)
	if err != nil {
		return nil, 0, err
	}
	return member, left, nil
}
