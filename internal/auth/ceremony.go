package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Ceremony state. A WebAuthn ceremony has two requests: the first returns options with a fresh challenge, the second
// returns what the authenticator signed over it. The server must remember the challenge (and what else the first
// request decided) in between, before any session exists. OSFM hands that state to the browser sealed, and takes it
// back with the second request:
//
//   - The state is encrypted and authenticated with AES-256-GCM under a key that exists only in this process's memory,
//     generated when the app starts. The browser can neither read nor change it; nothing in it is an internal ID
//     anyway. A restart makes every state issued before it unusable, which only means starting the ceremony again.
//   - The ceremony kind is the additional authenticated data, so a state issued for one kind of ceremony fails to open
//     for another.
//   - It expires a few minutes after it was issued ([ceremonyTTL]).
//   - It can be used once: opening it records its random ID until it expires, and a second attempt fails, whether the
//     first one succeeded or not. Because the key lives no longer than this record, a state can never be opened twice.
//
// OSFM runs as a single process (AD1), so memory is the right place for both; no database row is written before a
// ceremony has succeeded.

// errCeremony is returned for a state that cannot be used: malformed, forged, for another kind of ceremony, expired or
// used already. Deliberately one error.
var errCeremony = errors.New("auth: invalid or expired ceremony state")

// maxStateLen bounds the length of a sealed state before it is decoded.
const maxStateLen = 8 << 10

const stateIDBytes = 16

// ceremonies seals and opens ceremony states. The zero value is not usable; see [newCeremonies].
type ceremonies struct {
	aead cipher.AEAD

	mu        sync.Mutex
	used      map[[stateIDBytes]byte]time.Time // ID → expiry, of the states opened already
	lastSweep time.Time
}

func newCeremonies() (*ceremonies, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &ceremonies{aead: aead, used: map[[stateIDBytes]byte]time.Time{}}, nil
}

// sealedState is the plaintext of a sealed state.
type sealedState struct {
	ID      []byte          `json:"i"`
	Expires int64           `json:"e"` // Unix time in seconds
	Data    json.RawMessage `json:"d"`
}

// seal returns v as a state for a ceremony of the given kind, valid for ttl.
func (c *ceremonies) seal(kind string, ttl time.Duration, v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	id := make([]byte, stateIDBytes)
	if _, err := rand.Read(id); err != nil {
		return "", err
	}
	plain, err := json.Marshal(sealedState{ID: id, Expires: now().Add(ttl).Unix(), Data: data})
	if err != nil {
		return "", err
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, plain, []byte(kind))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// open decodes a state sealed for a ceremony of the given kind into v and marks it used. It returns [errCeremony] for
// every state that cannot be used.
func (c *ceremonies) open(kind, state string, v any) error {
	if len(state) > maxStateLen {
		return errCeremony
	}
	sealed, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil || len(sealed) < c.aead.NonceSize() {
		return errCeremony
	}
	n := c.aead.NonceSize()
	plain, err := c.aead.Open(nil, sealed[:n], sealed[n:], []byte(kind))
	if err != nil {
		return errCeremony
	}
	var s sealedState
	if err := json.Unmarshal(plain, &s); err != nil || len(s.ID) != stateIDBytes {
		return errCeremony
	}
	t := now()
	expires := time.Unix(s.Expires, 0)
	if !expires.After(t) {
		return errCeremony
	}
	if !c.markUsed([stateIDBytes]byte(s.ID), expires, t) {
		return errCeremony
	}
	if err := json.Unmarshal(s.Data, v); err != nil {
		return fmt.Errorf("%w: %w", errCeremony, err)
	}
	return nil
}

// markUsed records a state's ID until it expires; it reports false if the ID was recorded already. Expired IDs are
// swept at most once a minute: a state past its expiry fails to open anyway.
func (c *ceremonies) markUsed(id [stateIDBytes]byte, expires, t time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.Sub(c.lastSweep) > time.Minute {
		for k, exp := range c.used {
			if !exp.After(t) {
				delete(c.used, k)
			}
		}
		c.lastSweep = t
	}
	if _, ok := c.used[id]; ok {
		return false
	}
	c.used[id] = expires
	return true
}
