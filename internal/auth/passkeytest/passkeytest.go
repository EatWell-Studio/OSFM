// Package passkeytest is a software WebAuthn authenticator for tests: it answers the creation and request options of
// the passkey routes the way a browser with a platform authenticator does, with ES256 discoverable credentials, user
// verification and attestation "none". Only tests import this package.
package passkeytest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
)

// Authenticator flags (WebAuthn §6.1).
const (
	flagUP = 0x01
	flagUV = 0x04
	flagBE = 0x08
	flagBS = 0x10
	flagAT = 0x40
)

// Credential is a credential the authenticator holds.
type Credential struct {
	ID         []byte
	RPID       string
	UserHandle []byte
	Key        *ecdsa.PrivateKey
	// SignCount is the counter of the last signature; it is incremented before every signature unless ZeroCounter is
	// set on the authenticator.
	SignCount uint32
}

// Authenticator holds discoverable credentials. Its fields change how it answers, so that tests can produce responses
// a real authenticator would not.
type Authenticator struct {
	// Origin is the origin the browser reports in the client data (http://localhost:8090 when empty).
	Origin string
	// RPID, when not empty, replaces the relying party ID of the options in what the authenticator signs.
	RPID string
	// ZeroCounter makes the authenticator report a signature counter of 0, as synced passkeys do.
	ZeroCounter bool
	// NoUserVerification leaves the UV flag unset.
	NoUserVerification bool
	// BackupEligible sets the BE and BS flags, as a synced passkey has them.
	BackupEligible bool

	Credentials []*Credential
}

func (a *Authenticator) origin() string {
	if a.Origin == "" {
		return "http://localhost:8090"
	}
	return a.Origin
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// creationOptions is the part of the creation options ({"publicKey": …}) the authenticator reads.
type creationOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		RP        struct {
			ID string `json:"id"`
		} `json:"rp"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"publicKey"`
}

// requestOptions is the part of the request options the authenticator reads.
type requestOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		RPID      string `json:"rpId"`
	} `json:"publicKey"`
}

func (a *Authenticator) clientData(typ, challenge string) []byte {
	data, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": a.origin(),
		"crossOrigin": false})
	return data
}

func (a *Authenticator) flags() byte {
	f := byte(flagUP)
	if !a.NoUserVerification {
		f |= flagUV
	}
	if a.BackupEligible {
		f |= flagBE | flagBS
	}
	return f
}

// Create answers creation options (JSON, as the options route returns them in "options") with a new credential and
// returns the RegistrationResponseJSON a browser would send.
func (a *Authenticator) Create(options []byte) ([]byte, error) {
	var o creationOptions
	if err := json.Unmarshal(options, &o); err != nil {
		return nil, err
	}
	userHandle, err := base64.RawURLEncoding.DecodeString(o.PublicKey.User.ID)
	if err != nil {
		return nil, fmt.Errorf("user.id: %w", err)
	}
	rpID := o.PublicKey.RP.ID
	if a.RPID != "" {
		rpID = a.RPID
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	cred := &Credential{ID: id, RPID: o.PublicKey.RP.ID, UserHandle: userHandle, Key: key}

	pub, err := key.PublicKey.ECDH()
	if err != nil {
		return nil, err
	}
	raw := pub.Bytes() // 0x04 || X || Y
	coseKey, err := webauthncbor.Marshal(webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{
			KeyType:   int64(webauthncose.EllipticKey),
			Algorithm: int64(webauthncose.AlgES256),
		},
		Curve:  int64(webauthncose.P256),
		XCoord: raw[1:33],
		YCoord: raw[33:65],
	})
	if err != nil {
		return nil, err
	}
	rpHash := sha256.Sum256([]byte(rpID))
	authData := append([]byte{}, rpHash[:]...)
	authData = append(authData, a.flags()|flagAT)
	authData = binary.BigEndian.AppendUint32(authData, 0)
	authData = append(authData, make([]byte, 16)...)                    // AAGUID: zero, as with attestation "none"
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(id))) // #nosec G115 -- 16 bytes
	authData = append(authData, id...)
	authData = append(authData, coseKey...)

	attObj, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": authData})
	if err != nil {
		return nil, err
	}
	a.Credentials = append(a.Credentials, cred)
	return json.Marshal(map[string]any{
		"id": b64(id), "rawId": b64(id), "type": "public-key", "authenticatorAttachment": "platform",
		"response": map[string]any{
			"clientDataJSON":    b64(a.clientData("webauthn.create", o.PublicKey.Challenge)),
			"attestationObject": b64(attObj),
			"transports":        []string{"internal", "hybrid"},
		},
		"clientExtensionResults": map[string]any{},
	})
}

// Get answers request options (JSON) with an assertion of the most recently created credential for the options'
// relying party ID and returns the AuthenticationResponseJSON a browser would send.
func (a *Authenticator) Get(options []byte) ([]byte, error) {
	var o requestOptions
	if err := json.Unmarshal(options, &o); err != nil {
		return nil, err
	}
	var cred *Credential
	for _, c := range a.Credentials {
		if c.RPID == o.PublicKey.RPID {
			cred = c
		}
	}
	if cred == nil {
		return nil, errors.New("passkeytest: no credential for the relying party " + o.PublicKey.RPID)
	}
	return a.Assert(cred, o.PublicKey.Challenge)
}

// Assert signs an assertion with cred over challenge (base64url, as in the options).
func (a *Authenticator) Assert(cred *Credential, challenge string) ([]byte, error) {
	rpID := cred.RPID
	if a.RPID != "" {
		rpID = a.RPID
	}
	if !a.ZeroCounter {
		cred.SignCount++
	}
	var count uint32
	if !a.ZeroCounter {
		count = cred.SignCount
	}
	rpHash := sha256.Sum256([]byte(rpID))
	authData := append([]byte{}, rpHash[:]...)
	authData = append(authData, a.flags())
	authData = binary.BigEndian.AppendUint32(authData, count)
	clientData := a.clientData("webauthn.get", challenge)
	clientHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, cred.Key, digest[:])
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"id": b64(cred.ID), "rawId": b64(cred.ID), "type": "public-key", "authenticatorAttachment": "platform",
		"response": map[string]any{
			"clientDataJSON":    b64(clientData),
			"authenticatorData": b64(authData),
			"signature":         b64(sig),
			"userHandle":        b64(cred.UserHandle),
		},
		"clientExtensionResults": map[string]any{},
	})
}
