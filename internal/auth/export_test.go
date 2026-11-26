package auth

import (
	"testing"
	"time"
)

// SetNow replaces the session clock for the rest of the test.
func SetNow(t testing.TB, f func() time.Time) {
	t.Helper()
	old := now
	now = f
	t.Cleanup(func() { now = old })
}

// HashToken exposes the stored form of a token.
var HashToken = hashToken
