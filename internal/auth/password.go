package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

// Passwords (D3) are hashed with argon2id (RFC 9106) and stored in credentials.password_hash in the PHC string format,
// "$argon2id$v=19$m=<KiB>,t=<passes>,p=<lanes>$<salt>$<hash>" (standard base64 without padding), so that each hash
// carries its own parameters: the parameters can be raised later without invalidating existing hashes.
//
// Parameters ([PasswordParams]): 64 MiB of memory, 2 passes, 2 lanes, a 16-byte random salt and a 32-byte hash.
// That is RFC 9106's memory-hard profile scaled to the smallest supported host, a 2-core ARM board: with 2 lanes one
// hash uses both cores and takes in the order of 100–300 ms there (much less on a current laptop), and at most
// [argon2Concurrency] hashes run at a time, which caps their memory at 128 MiB and keeps a burst of sign-in attempts
// from exhausting a small machine. Further requests wait for a slot (or until the request is cancelled).
//
// Passwords are normalised to Unicode NFKC before hashing, so that the same password typed on different devices
// (composed or decomposed characters, full-width forms) gives the same hash. Rules: at least [MinPasswordLength] and
// at most [MaxPasswordLength] characters after normalisation, and not the login name itself; no composition rules.

// PasswordParams are the argon2id parameters of new password hashes.
type PasswordParams struct {
	Memory  uint32 // KiB
	Time    uint32 // passes
	Threads uint8  // lanes
}

// DefaultPasswordParams are the parameters new hashes use; see the package comment above.
var DefaultPasswordParams = PasswordParams{Memory: 64 * 1024, Time: 2, Threads: 2}

// passwordParams are the parameters in use; tests lower them.
var passwordParams = DefaultPasswordParams

// Password rules, counted in characters (code points) after NFKC normalisation.
const (
	MinPasswordLength = 8
	MaxPasswordLength = 128
)

const (
	saltBytes = 16
	hashBytes = 32
	// argon2Concurrency bounds the argon2id computations running at a time (memory: this many times Memory).
	argon2Concurrency = 2
	// Bounds of the parameters accepted from a stored hash, against a corrupted or planted row.
	maxStoredMemory  = 1 << 20 // 1 GiB
	maxStoredTime    = 16
	maxStoredThreads = 16
)

var argon2Slots = make(chan struct{}, argon2Concurrency)

// errBadHash is returned for a stored password hash that is not an argon2id PHC string within the accepted bounds.
var errBadHash = errors.New("auth: malformed password hash")

// normalizePassword returns the password as it is hashed.
func normalizePassword(password string) string {
	return norm.NFKC.String(password)
}

// passwordLength is the length the password rules count.
func passwordLength(password string) int {
	return utf8.RuneCountInString(normalizePassword(password))
}

// hashPassword returns the PHC string of password with a new random salt and the current parameters.
func hashPassword(ctx context.Context, password string) (string, error) {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: password salt: %w", err)
	}
	p := passwordParams
	sum, err := argon2id(ctx, []byte(normalizePassword(password)), salt, p, hashBytes)
	if err != nil {
		return "", err
	}
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.Memory, p.Time, p.Threads,
		b64.EncodeToString(salt), b64.EncodeToString(sum)), nil
}

// verifyPassword reports whether password matches the PHC string encoded. The comparison is constant in time.
func verifyPassword(ctx context.Context, encoded, password string) (bool, error) {
	p, salt, want, err := parsePasswordHash(encoded)
	if err != nil {
		return false, err
	}
	got, err := argon2id(ctx, []byte(normalizePassword(password)), salt, p, uint32(len(want))) // #nosec G115 -- len(want) is at most 64
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

var (
	dummyHashOnce sync.Once
	dummyHash     string
	dummyHashErr  error
)

// verifyDummy spends the time of a password verification without anything to verify against: for a login name that
// does not exist, or a member without a password, so that the response time does not tell these apart from a wrong
// password. It always reports a mismatch.
func verifyDummy(ctx context.Context, password string) error {
	dummyHashOnce.Do(func() {
		var b [16]byte
		_, _ = rand.Read(b[:])
		dummyHash, dummyHashErr = hashPassword(context.Background(), base64.RawStdEncoding.EncodeToString(b[:]))
	})
	if dummyHashErr != nil {
		return dummyHashErr
	}
	_, err := verifyPassword(ctx, dummyHash, password)
	return err
}

// argon2id runs argon2id in one of the [argon2Concurrency] slots, waiting for a slot until ctx is done.
func argon2id(ctx context.Context, password, salt []byte, p PasswordParams, keyLen uint32) ([]byte, error) {
	select {
	case argon2Slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-argon2Slots }()
	return argon2.IDKey(password, salt, p.Time, p.Memory, p.Threads, keyLen), nil
}

// parsePasswordHash splits a PHC string into its parameters, salt and hash, within the accepted bounds.
func parsePasswordHash(encoded string) (p PasswordParams, salt, sum []byte, err error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=…,t=…,p=…", salt, hash
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return p, nil, nil, errBadHash
	}
	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 {
		return p, nil, nil, errBadHash
	}
	var values [3]uint64 // m, t, p
	for i, name := range [3]string{"m", "t", "p"} {
		key, value, ok := strings.Cut(fields[i], "=")
		if !ok || key != name {
			return p, nil, nil, errBadHash
		}
		if values[i], err = strconv.ParseUint(value, 10, 32); err != nil {
			return p, nil, nil, errBadHash
		}
	}
	m, t, threads := values[0], values[1], values[2]
	if t < 1 || t > maxStoredTime || threads < 1 || threads > maxStoredThreads || m < 8*threads || m > maxStoredMemory {
		return p, nil, nil, errBadHash
	}
	b64 := base64.RawStdEncoding
	if salt, err = b64.DecodeString(parts[4]); err != nil || len(salt) < 8 {
		return p, nil, nil, errBadHash
	}
	if sum, err = b64.DecodeString(parts[5]); err != nil || len(sum) < 16 || len(sum) > 64 {
		return p, nil, nil, errBadHash
	}
	return PasswordParams{Memory: uint32(m), Time: uint32(t), Threads: uint8(threads)}, salt, sum, nil // #nosec G115 -- bounded above
}
