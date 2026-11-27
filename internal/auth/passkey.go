package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Passkeys (D3, D4, N5): registration and sign-in with a WebAuthn credential, through github.com/go-webauthn/webauthn.
//
//   - Relying party: the ID and the accepted origins come from [Options] (config.Config.PasskeyRP derives them from
//     OSFM_PUBLIC_URL). Every ceremony requires user verification (the authenticator checks a PIN, fingerprint or face,
//     so the passkey alone is both factors) and a discoverable credential (sign-in needs no login name). Attestation
//     is "none": OSFM does not restrict authenticator models, and attestation would identify the device model.
//   - User handle: every passkey of a member carries the member's passkey_user_handle, 32 random bytes made when the
//     first passkey is registered. It is neither the record ID nor the uuid (AD4): authenticators and the passkey
//     providers that sync them (Apple, Google, password managers) keep the handle, and it must tell them nothing about
//     the member and must not link the passkey to an identifier that appears in responses, exports or, later,
//     federation (the uuid, a UUIDv7, would also reveal when the member registered).
//   - Ceremony state between the two requests of a ceremony: see ceremony.go.
//   - Signature counter: a sign-in whose counter is not greater than the stored one is refused when either is not
//     zero (a cloned authenticator, §7.2 step 17 of the specification); synced passkeys report zero throughout. The
//     new counter is written with a conditional update on the old one.

// Errors of the passkey ceremonies.
var (
	// ErrPasskeyFailed is returned for every ceremony that does not verify: a state that is malformed, expired, used
	// or for another ceremony, a response that does not verify (wrong origin or relying party ID, wrong challenge, no
	// user verification, bad signature), an unknown credential, a user handle that does not match, or a signature
	// counter that went backwards. Deliberately one error.
	ErrPasskeyFailed = errors.New("auth: passkey verification failed")
	// ErrPasskeysUnavailable is returned when the relying party configuration is unusable (for example
	// OSFM_PUBLIC_URL with an IP address, which cannot be a relying party ID).
	ErrPasskeysUnavailable = errors.New("auth: passkeys are not available on this instance")
)

// Ceremony kinds, bound into the sealed state.
const (
	ceremonyRegister = "passkey.register"
	ceremonySignIn   = "passkey.signin"
)

// CeremonyTimeout is the time the browser is given to complete a passkey ceremony; the sealed state lives a minute
// longer, for the request that returns the result.
const (
	CeremonyTimeout = 5 * time.Minute
	ceremonyTTL     = CeremonyTimeout + time.Minute
)

// userHandleBytes is the size of a member's passkey user handle: 256 random bits.
const userHandleBytes = 32

// RPDisplayName is the relying party name authenticators show next to the passkey.
const RPDisplayName = "OSFM"

// Audit log actions of the passkeys.
const (
	// AuditPasskeyCloneWarning: a sign-in was refused because the credential's signature counter did not increase.
	AuditPasskeyCloneWarning = "credentials.passkey_counter_regression"
)

// passkeysKey is the app store key of the [passkeys] of an app, set by [Bind].
const passkeysKey = "osfmPasskeys"

// passkeys is the passkey setup of an app: the relying party and the ceremony state.
type passkeys struct {
	wa     *webauthn.WebAuthn
	err    error // why wa is nil
	states *ceremonies
}

// newPasskeys builds the passkey setup for the relying party rpID, accepting ceremonies from origins.
func newPasskeys(rpID string, origins []string) (*passkeys, error) {
	states, err := newCeremonies()
	if err != nil {
		return nil, err
	}
	p := &passkeys{states: states}
	p.wa, p.err = webauthn.New(&webauthn.Config{
		RPID:                  rpID,
		RPDisplayName:         RPDisplayName,
		RPOrigins:             origins,
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: CeremonyTimeout, TimeoutUVD: CeremonyTimeout},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: CeremonyTimeout, TimeoutUVD: CeremonyTimeout},
		},
		// OSFM requests no extension and uses no extension output; an output a client or password manager adds on its
		// own is ignored rather than failing the ceremony.
		ExtensionsUnsolicitedOutputPolicy: protocol.UnsolicitedOutputPolicyIgnore,
	})
	return p, nil
}

func passkeysOf(app core.App) (*passkeys, error) {
	p, _ := app.Store().Get(passkeysKey).(*passkeys)
	if p == nil {
		return nil, ErrPasskeysUnavailable
	}
	if p.wa == nil {
		return nil, fmt.Errorf("%w: %w", ErrPasskeysUnavailable, p.err)
	}
	return p, nil
}

// passkeyUser is a member, or a member to be, as go-webauthn sees them.
type passkeyUser struct {
	handle      []byte
	name        string // login name
	displayName string
	credentials []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte                         { return u.handle }
func (u *passkeyUser) WebAuthnName() string                       { return u.name }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.displayName }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// registrationState is what the first request of a registration decided, sealed into its state.
type registrationState struct {
	InviteHash  string               `json:"i"` // SHA-256 of the invite or setup code: the second request brings the same
	LoginName   string               `json:"l"`
	DisplayName string               `json:"n"`
	UserHandle  []byte               `json:"h"`
	Session     webauthn.SessionData `json:"s"`
	Instance    string               `json:"in,omitempty"` // the first maintainer's: the instance name (setup.go)
	Member      string               `json:"m,omitempty"`  // an existing member's uuid (enrol.go)
}

// BeginPasskeyRegistration starts registering a new member with a passkey: it checks the invite code (without using
// it), the login name and the display name, and returns the options for navigator.credentials.create() together with
// the sealed state that [FinishPasskeyRegistration] needs. Errors: errLoginNameInvalid, errDisplayNameInvalid,
// [ErrInvalidInvite], errLoginNameTaken (only told to holders of a valid invite) and [ErrPasskeysUnavailable].
func BeginPasskeyRegistration(app core.App, code, loginName, displayName string) (
	options *protocol.CredentialCreation, state string, err error) {
	p, err := passkeysOf(app)
	if err != nil {
		return nil, "", err
	}
	if loginName = NormalizeLoginName(loginName); !validLoginName(loginName) {
		return nil, "", errLoginNameInvalid
	}
	var ok bool
	if displayName, ok = normalizeDisplayName(displayName, loginName); !ok {
		return nil, "", errDisplayNameInvalid
	}
	ok, err = CheckInvite(app, code)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", ErrInvalidInvite
	}
	return beginPasskeyEnrolment(app, p, ceremonyRegister, hashToken(code), "", loginName, displayName)
}

// beginPasskeyEnrolment is the first request of a passkey registration after the right to register has been checked
// (an invite, or the setup code for the first maintainer: setup.go) and the names normalised: it checks that the login
// name is free and returns the creation options with the state sealed for the ceremony kind, binding bindHash, the
// hash of the invite or setup code, and, for the first maintainer, newInstance, the instance name the setup saves.
func beginPasskeyEnrolment(app core.App, p *passkeys, kind, bindHash, newInstance, loginName, displayName string) (
	options *protocol.CredentialCreation, state string, err error) {
	if taken, err := loginNameTaken(app, loginName); err != nil {
		return nil, "", err
	} else if taken {
		return nil, "", errLoginNameTaken
	}

	handle := make([]byte, userHandleBytes)
	if _, err := rand.Read(handle); err != nil {
		return nil, "", err
	}
	user := &passkeyUser{handle: handle, name: loginName, displayName: displayName}
	options, session, err := p.wa.BeginRegistration(user)
	if err != nil {
		return nil, "", fmt.Errorf("auth: begin passkey registration: %w", err)
	}
	state, err = p.states.seal(kind, ceremonyTTL, registrationState{
		InviteHash: bindHash, LoginName: loginName, DisplayName: displayName, UserHandle: handle,
		Session: *session, Instance: newInstance,
	})
	if err != nil {
		return nil, "", err
	}
	return options, state, nil
}

// FinishPasskeyRegistration verifies the authenticator's response (the JSON of the PublicKeyCredential) against the
// state from [BeginPasskeyRegistration] and then, through [RegisterWithInvite], creates the member with the passkey
// and a new set of recovery codes, consuming the invite. It returns the member and the recovery codes, which exist
// nowhere else. The caller signs the member in. Errors: [ErrPasskeyFailed] (also for a code other than the one the
// ceremony began with), [ErrInvalidInvite], errLoginNameTaken.
func FinishPasskeyRegistration(app core.App, code, state string, response []byte) (
	member *core.Record, recoveryCodes []string, err error) {
	p, err := passkeysOf(app)
	if err != nil {
		return nil, nil, err
	}
	pending, err := passkeyRegistration(p, ceremonyRegister, hashToken(code), state, response)
	if err != nil {
		return nil, nil, err
	}
	if member, err = RegisterWithInvite(app, code, pending.register); err != nil {
		return nil, nil, err
	}
	return member, *pending.codes, nil
}

// passkeyRegistration verifies the second request of a passkey registration of the ceremony kind, begun with the
// invite or setup code whose hash is bindHash, and returns the registration that saves the member with the passkey
// and new recovery codes.
func passkeyRegistration(p *passkeys, kind, bindHash, state string, response []byte) (*pendingRegistration, error) {
	var st registrationState
	if err := p.states.open(kind, state, &st); err != nil {
		return nil, ErrPasskeyFailed
	}
	if subtle.ConstantTimeCompare([]byte(st.InviteHash), []byte(bindHash)) != 1 {
		return nil, ErrPasskeyFailed
	}
	// The verification needs no database and runs before the registration transaction (see [Registration]).
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPasskeyFailed, err)
	}
	user := &passkeyUser{handle: st.UserHandle, name: st.LoginName, displayName: st.DisplayName}
	cred, err := p.wa.CreateCredential(user, st.Session, parsed)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPasskeyFailed, err)
	}

	var recoveryCodes []string
	return &pendingRegistration{codes: &recoveryCodes, newInstance: st.Instance, register: func(tx core.App,
		m *core.Record) error {
		if taken, err := loginNameTaken(tx, st.LoginName); err != nil {
			return err
		} else if taken {
			return errLoginNameTaken
		}
		m.Set("login_name", st.LoginName)
		m.Set("display_name", st.DisplayName)
		m.Set("passkey_user_handle", base64.RawURLEncoding.EncodeToString(st.UserHandle))
		if err := tx.Save(m); err != nil {
			return err
		}
		if _, err := savePasskey(tx, m, cred, ""); err != nil {
			return err
		}
		var err error
		recoveryCodes, err = NewRecoveryCodes(tx, m)
		return err
	}}, nil
}

// errCredentialExists: the credential ID is registered already.
var errCredentialExists = fmt.Errorf("%w: the credential is registered already", ErrPasskeyFailed)

// savePasskey stores a verified credential as a passkey of member, named deviceName (normalised already), and
// returns the record.
func savePasskey(tx core.App, member *core.Record, cred *webauthn.Credential, deviceName string) (*core.Record, error) {
	id := base64.RawURLEncoding.EncodeToString(cred.ID)
	if _, err := tx.FindFirstRecordByData(store.Credentials, "credential_id", id); err == nil {
		return nil, errCredentialExists
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	c, err := tx.FindCachedCollectionByNameOrId(store.Credentials)
	if err != nil {
		return nil, err
	}
	aaguid := uuid.Nil
	if len(cred.Authenticator.AAGUID) == 16 {
		aaguid = uuid.UUID(cred.Authenticator.AAGUID)
	}
	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		transports = append(transports, string(t))
	}
	r := core.NewRecord(c)
	r.Set("member", member.Id)
	r.Set("kind", CredentialPasskey)
	r.Set("credential_id", id)
	r.Set("device_name", deviceName)
	r.Set("public_key", base64.RawURLEncoding.EncodeToString(cred.PublicKey))
	r.Set("sign_count", cred.Authenticator.SignCount)
	r.Set("aaguid", aaguid.String())
	r.Set("transports", transports)
	r.Set("backup_eligible", cred.Flags.BackupEligible)
	r.Set("backup_state", cred.Flags.BackupState)
	r.Set("user_verified", cred.Flags.UserVerified)
	r.Set("attestation_type", cred.AttestationType)
	r.Set("attestation_format", cred.AttestationFormat)
	return r, tx.Save(r)
}

// loadPasskey returns a stored passkey as go-webauthn's credential record.
func loadPasskey(r *core.Record) (webauthn.Credential, error) {
	id, err := base64.RawURLEncoding.DecodeString(r.GetString("credential_id"))
	if err != nil {
		return webauthn.Credential{}, err
	}
	key, err := base64.RawURLEncoding.DecodeString(r.GetString("public_key"))
	if err != nil {
		return webauthn.Credential{}, err
	}
	flags := protocol.FlagUserPresent
	if r.GetBool("user_verified") {
		flags |= protocol.FlagUserVerified
	}
	if r.GetBool("backup_eligible") {
		flags |= protocol.FlagBackupEligible
	}
	if r.GetBool("backup_state") {
		flags |= protocol.FlagBackupState
	}
	var aaguid []byte
	if u, err := uuid.Parse(r.GetString("aaguid")); err == nil {
		aaguid = u[:]
	}
	var transports []protocol.AuthenticatorTransport
	for _, t := range r.GetStringSlice("transports") {
		transports = append(transports, protocol.AuthenticatorTransport(t))
	}
	signCount := r.GetInt("sign_count")
	if signCount < 0 || int64(signCount) > int64(^uint32(0)) {
		return webauthn.Credential{}, errors.New("auth: stored signature counter out of range")
	}
	return webauthn.Credential{
		ID:                id,
		PublicKey:         key,
		AttestationType:   r.GetString("attestation_type"),
		AttestationFormat: r.GetString("attestation_format"),
		Transport:         transports,
		Flags:             webauthn.NewCredentialFlags(flags),
		Authenticator:     webauthn.Authenticator{AAGUID: aaguid, SignCount: uint32(signCount)},
	}, nil
}

// BeginPasskeySignIn starts a sign-in with a discoverable passkey: no login name, any passkey of this relying party.
// It returns the options for navigator.credentials.get() and the sealed state [VerifyPasskeySignIn] needs.
func BeginPasskeySignIn(app core.App) (options *protocol.CredentialAssertion, state string, err error) {
	p, err := passkeysOf(app)
	if err != nil {
		return nil, "", err
	}
	options, session, err := p.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", fmt.Errorf("auth: begin passkey sign-in: %w", err)
	}
	if state, err = p.states.seal(ceremonySignIn, ceremonyTTL, session); err != nil {
		return nil, "", err
	}
	return options, state, nil
}

// VerifyPasskeySignIn verifies the authenticator's response (the JSON of the PublicKeyCredential) against the state
// from [BeginPasskeySignIn]: the credential must be a stored passkey, its user handle that of the passkey's member,
// the signature valid for the challenge, origin and relying party, with user verification, and the signature counter
// must not go backwards. It records the new counter and returns the member, who has not been signed in yet (the
// caller does that with [SignIn]). It returns [ErrPasskeyFailed] for every failure of the ceremony, and
// [ErrCannotSignIn] for a member who has left or was deleted; callers answer both alike.
func VerifyPasskeySignIn(app core.App, state string, response []byte) (*core.Record, error) {
	p, err := passkeysOf(app)
	if err != nil {
		return nil, err
	}
	var session webauthn.SessionData
	if err := p.states.open(ceremonySignIn, state, &session); err != nil {
		return nil, ErrPasskeyFailed
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPasskeyFailed, err)
	}

	var stored, member *core.Record
	var lookupErr error
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		stored, member, lookupErr = findPasskey(app, rawID, userHandle)
		if lookupErr != nil {
			return nil, lookupErr
		}
		cred, err := loadPasskey(stored)
		if err != nil {
			return nil, err
		}
		handle, _ := base64.RawURLEncoding.DecodeString(member.GetString("passkey_user_handle"))
		return &passkeyUser{handle: handle, name: member.GetString("login_name"),
			displayName: member.GetString("display_name"), credentials: []webauthn.Credential{cred}}, nil
	}
	_, cred, err := p.wa.ValidatePasskeyLogin(handler, session, parsed)
	if errors.Is(lookupErr, ErrCannotSignIn) {
		return nil, ErrCannotSignIn
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPasskeyFailed, err)
	}
	if err := recordPasskeyUse(app, member, stored, cred); err != nil {
		return nil, err
	}
	return member, nil
}

// recordPasskeyUse finishes a verified assertion of the stored passkey of member: a signature counter that did not
// increase is refused ([ErrPasskeyFailed]) and audited; otherwise the new counter, the backup state (which may change)
// and the day of this use are written, only if the stored counter is still the one verified against: of two
// assertions racing with the same counter, one fails.
func recordPasskeyUse(app core.App, member, stored *core.Record, cred *webauthn.Credential) error {
	if cred.Authenticator.CloneWarning {
		if err := writeAudit(app, nil, AuditPasskeyCloneWarning, store.Members, member.GetString(store.FieldUUID),
			map[string]any{"credential": stored.GetString(store.FieldUUID)}); err != nil {
			app.Logger().Warn("could not audit a passkey counter regression", "error", err)
		}
		return fmt.Errorf("%w: the signature counter did not increase", ErrPasskeyFailed)
	}
	res, err := app.DB().Update(store.Credentials, dbx.Params{
		"sign_count":       cred.Authenticator.SignCount,
		"backup_state":     cred.Flags.BackupState,
		"last_used_at":     dbTime(day(now())),
		store.FieldUpdated: dbTime(now()),
	}, dbx.HashExp{"id": stored.Id, "sign_count": stored.GetInt("sign_count")}).Execute()
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return fmt.Errorf("%w: the signature counter changed meanwhile", ErrPasskeyFailed)
	}
	return nil
}

// errUnknownPasskey: no passkey has the credential ID, or its user handle is not its member's.
var errUnknownPasskey = errors.New("auth: unknown passkey")

// findPasskey returns the stored passkey with the credential ID and its member, after checking that userHandle is the
// member's passkey user handle. A member who may not sign in gives [ErrCannotSignIn].
func findPasskey(app core.App, rawID, userHandle []byte) (stored, member *core.Record, err error) {
	stored, err = app.FindFirstRecordByFilter(store.Credentials, "kind = 'passkey' && credential_id = {:id}",
		dbx.Params{"id": base64.RawURLEncoding.EncodeToString(rawID)})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, errUnknownPasskey
	}
	if err != nil {
		return nil, nil, err
	}
	// store.Query leaves out soft-deleted members.
	member, err = store.Query(app, store.Members).
		Where(dbx.HashExp{store.Members + ".id": stored.GetString("member")}).One()
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrCannotSignIn
	}
	if err != nil {
		return nil, nil, err
	}
	want := sha256.Sum256([]byte(member.GetString("passkey_user_handle")))
	got := sha256.Sum256([]byte(base64.RawURLEncoding.EncodeToString(userHandle)))
	if member.GetString("passkey_user_handle") == "" || subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
		return nil, nil, errUnknownPasskey
	}
	if !CanSignIn(member) {
		return nil, nil, ErrCannotSignIn
	}
	return stored, member, nil
}
