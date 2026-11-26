package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Values sealed with the instance master key (secrets/master.key, AES-256-GCM; architecture §3.3). Three kinds, each
// under a key of its own derived from the master key with HKDF-SHA256, so that a value of one kind can never be
// passed off as another:
//
//   - a member's TOTP secret at rest, in credentials.totp_secret_enc ([sealTOTPSecret]), bound to the member's uuid;
//   - the pending enrolment of a password and TOTP registration, between the step that generates the TOTP secret and
//     the step that confirms it ([passwordEnrolment]); the client holds it and cannot read or change it;
//   - the sign-in ticket between the password step and the second factor ([signInTicket]).
//
// Every value has its own random 96-bit nonce (cipher.NewGCMWithRandomNonce); GCM authenticates the ciphertext
// together with associated data that names what the value belongs to. Without the master key, the database alone
// yields no TOTP secret, and nobody can forge an enrolment or a ticket.
const (
	purposeTOTPSecret = "osfm totp secret v1" // #nosec G101 -- a key derivation label, not a credential
	purposeEnrolment  = "osfm password enrolment v1"
	purposeTicket     = "osfm sign-in ticket v1"
)

// masterKeyStoreKey is the app store key of the master key getter, set by [Bind] from [Options].MasterKey.
const masterKeyStoreKey = "osfmMasterKey"

// errNoMasterKey means that [Bind] was given no usable master key.
var errNoMasterKey = errors.New("auth: the master key is not available")

// errSealed is returned for a sealed value that does not open: malformed, changed, sealed under another key or for
// another purpose or owner, or expired.
var errSealed = errors.New("auth: invalid or expired sealed value")

// masterKeySize is the size of the master key (AES-256), as internal/config generates it.
const masterKeySize = 32

func masterKey(app core.App) ([]byte, error) {
	get, _ := app.Store().Get(masterKeyStoreKey).(func() []byte)
	if get == nil {
		return nil, errNoMasterKey
	}
	key := get()
	if len(key) != masterKeySize {
		return nil, errNoMasterKey
	}
	return key, nil
}

// aead returns AES-256-GCM with random nonces under the key derived from the master key for purpose.
func aead(app core.App, purpose string) (cipher.AEAD, error) {
	mk, err := masterKey(app)
	if err != nil {
		return nil, err
	}
	key, err := hkdf.Key(sha256.New, mk, nil, purpose, masterKeySize)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}

// seal encrypts plaintext for purpose, authenticating aad with it, and returns nonce, ciphertext and tag,
// base64url-encoded without padding.
func seal(app core.App, purpose string, plaintext, aad []byte) (string, error) {
	a, err := aead(app, purpose)
	if err != nil {
		return "", err
	}
	// #nosec G407 -- the nonce is nil because NewGCMWithRandomNonce generates a random one for every value.
	return base64.RawURLEncoding.EncodeToString(a.Seal(nil, nil, plaintext, aad)), nil
}

// open reverses [seal]; any failure is [errSealed], except a missing master key.
func open(app core.App, purpose, sealed string, aad []byte) ([]byte, error) {
	a, err := aead(app, purpose)
	if err != nil {
		return nil, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		return nil, errSealed
	}
	plaintext, err := a.Open(nil, nil, raw, aad)
	if err != nil {
		return nil, errSealed
	}
	return plaintext, nil
}

// expiring is a sealed JSON value with an expiry, in Unix seconds.
type expiring interface{ expiry() int64 }

// sealJSON seals v, which carries its own expiry, as JSON.
func sealJSON(app core.App, purpose string, v expiring, aad []byte) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return seal(app, purpose, b, aad)
}

// openJSON opens a value sealed by [sealJSON] into v; an expired value is [errSealed].
func openJSON(app core.App, purpose, sealed string, aad []byte, v expiring) error {
	b, err := open(app, purpose, sealed, aad)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return errSealed
	}
	if now().Unix() >= v.expiry() {
		return errSealed
	}
	return nil
}

// totpSecretVersion prefixes credentials.totp_secret_enc, so that the format (or the key) can change later.
const totpSecretVersion = "v1."

// sealTOTPSecret encrypts a TOTP secret for storage in credentials.totp_secret_enc. The member's uuid is the
// associated data: the ciphertext opens only for that member, so it cannot be moved to another member's credential.
func sealTOTPSecret(app core.App, memberUUID string, secret []byte) (string, error) {
	s, err := seal(app, purposeTOTPSecret, secret, []byte(memberUUID))
	if err != nil {
		return "", fmt.Errorf("auth: encrypt the TOTP secret: %w", err)
	}
	return totpSecretVersion + s, nil
}

// openTOTPSecret decrypts credentials.totp_secret_enc of the member with that uuid.
func openTOTPSecret(app core.App, memberUUID, stored string) ([]byte, error) {
	if len(stored) <= len(totpSecretVersion) || stored[:len(totpSecretVersion)] != totpSecretVersion {
		return nil, errSealed
	}
	return open(app, purposeTOTPSecret, stored[len(totpSecretVersion):], []byte(memberUUID))
}

// unixAfter returns the Unix time d from now.
func unixAfter(d time.Duration) int64 {
	return now().Add(d).Unix()
}
