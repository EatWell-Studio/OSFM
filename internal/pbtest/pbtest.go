// Package pbtest makes PocketBase test apps with the OSFM collections, for tests only. Applying every migration
// takes a few seconds under the race detector, and some packages create dozens of test apps; [NewApp] applies them
// once per test binary, into a template data directory, and gives each test app a copy of it, as
// tests.NewTestApp does with a test data directory.
//
// A package that uses it removes the template when its tests end, from its TestMain:
//
//	func TestMain(m *testing.M) {
//		code := m.Run()
//		pbtest.RemoveTemplate()
//		os.Exit(code)
//	}
//
// Tests of the migrations themselves start from an empty data directory instead.
package pbtest

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	_ "github.com/EatWell-Studio/OSFM/migrations" // the OSFM collections
)

var template struct {
	sync.Mutex
	dir string
}

// NewApp returns a test app on a copy of a data directory with PocketBase's and OSFM's migrations applied, as
// tests.NewTestApp(t.TempDir()) returns one after applying them itself. No hooks are bound. The app is cleaned up when
// the test ends.
func NewApp(t testing.TB) *tests.TestApp {
	t.Helper()
	dir, err := templateDir()
	if err != nil {
		t.Fatal(err)
	}
	app, err := tests.NewTestApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

// RemoveTemplate removes the template data directory, if one was made.
func RemoveTemplate() {
	template.Lock()
	defer template.Unlock()
	if template.dir != "" {
		_ = os.RemoveAll(template.dir)
		template.dir = ""
	}
}

func templateDir() (string, error) {
	template.Lock()
	defer template.Unlock()
	if template.dir != "" {
		return template.dir, nil
	}
	dir, err := os.MkdirTemp("", "osfm-pbtest-")
	if err != nil {
		return "", err
	}
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
	err = app.Bootstrap()
	if err == nil {
		err = app.RunAllMigrations()
	}
	if clearErr := app.ClearBootstrap(); err == nil {
		err = clearErr
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("pbtest: template data directory: %w", err)
	}
	template.dir = dir
	return dir, nil
}
