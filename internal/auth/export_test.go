package auth

import (
	"os"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/pbtest"
)

// TestMain lowers the argon2id parameters for every test of the package: hashing with the production parameters
// under the race detector would make the tests slow. The hashes record their parameters, so verification works the
// same way. Test apps are copies of one migrated data directory (internal/pbtest), removed at the end.
func TestMain(m *testing.M) {
	passwordParams = PasswordParams{Memory: 256, Time: 1, Threads: 1}
	code := m.Run()
	pbtest.RemoveTemplate()
	os.Exit(code)
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

// NewSetupCode replaces the app's setup code with a new one and returns it.
func NewSetupCode(t testing.TB, app core.App) string {
	t.Helper()
	code, err := setupCodeOf(app).generate()
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// RegisterFirstMaintainer exposes the creation of the first maintainer.
var RegisterFirstMaintainer = registerFirstMaintainer
