package auth

import (
	"os"
	"testing"
	"time"
)

// TestMain lowers the argon2id parameters for every test of the package: hashing with the production parameters
// under the race detector would make the tests slow. The hashes record their parameters, so verification works the
// same way.
func TestMain(m *testing.M) {
	passwordParams = PasswordParams{Memory: 256, Time: 1, Threads: 1}
	os.Exit(m.Run())
}

// SetNow replaces the session clock for the rest of the test.
func SetNow(t testing.TB, f func() time.Time) {
	t.Helper()
	old := now
	now = f
	t.Cleanup(func() { now = old })
}

// HashToken exposes the stored form of a token.
var HashToken = hashToken

// HashRecoveryCode exposes the stored form of a recovery code (in canonical form).
var HashRecoveryCode = hashRecoveryCode

// OpenTOTPSecret exposes the decryption of a stored TOTP secret.
var OpenTOTPSecret = openTOTPSecret
