package main

import (
	"os"
	"testing"

	"github.com/EatWell-Studio/OSFM/internal/pbtest"
)

// TestMain removes the migrated template data directory that internal/testapp copies for every app it starts.
func TestMain(m *testing.M) {
	code := m.Run()
	pbtest.RemoveTemplate()
	os.Exit(code)
}
