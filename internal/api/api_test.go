package api_test

import (
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/pbtest"
	"github.com/EatWell-Studio/OSFM/internal/ratelimit"
	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

func TestMain(m *testing.M) {
	code := m.Run()
	pbtest.RemoveTemplate()
	os.Exit(code)
}

// start boots an app with the session layer and the API, with limits; the whole instance as `osfm serve` binds it is
// tested in cmd/osfm.
func start(t *testing.T, limits api.Limits) *testapp.App {
	t.Helper()
	return testapp.Start(t, testapp.Options{Bind: func(app core.App, cfg *config.Config) {
		store.BindHooks(app)
		auth.Bind(app, auth.Options{})
		api.Bind(app, api.Options{Config: cfg, Limits: &limits})
	}})
}

// TestWriteBudget: the write budget counts per member, over all their sessions, and answers 429 with Retry-After;
// reads do not count.
func TestWriteBudget(t *testing.T) {
	t.Parallel()
	a := start(t, api.Limits{Writes: ratelimit.Rate{Burst: 2, Every: time.Hour}})
	update := api.InstanceUpdate{}
	for range 3 {
		a.As(testapp.Maintainer).Get(api.InstancePath).Expect(http.StatusOK)
	}
	a.As(testapp.Maintainer).Post(api.InstancePath, update).Expect(http.StatusOK)
	a.As(testapp.Maintainer).Post(api.InstancePath, update).Expect(http.StatusOK)
	r := a.As(testapp.Maintainer).Post(api.InstancePath, update).Expect(http.StatusTooManyRequests)
	if e := testapp.Decode[api.Error](r); e.Code != api.CodeRateLimited || e.Status != http.StatusTooManyRequests {
		t.Errorf("429 body: %+v", e)
	}
	if s, err := strconv.Atoi(r.Header().Get("Retry-After")); err != nil || s < 3000 || s > 3600 {
		t.Errorf("Retry-After %q", r.Header().Get("Retry-After"))
	}
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", r.Header().Get("Cache-Control"))
	}
	// Another member has a budget of their own (and is refused as a member).
	a.As(testapp.Member).Post(api.InstancePath, update).Expect(http.StatusForbidden)
	// Reads still work.
	a.As(testapp.Maintainer).Get(api.InstancePath).Expect(http.StatusOK)
}

// TestDefaultLimits keeps the default write budget generous for people and bounded for scripts.
func TestDefaultLimits(t *testing.T) {
	l := api.DefaultLimits()
	if l.Writes.Burst < 20 || l.Writes.Every <= 0 || l.Writes.Every > 10*time.Second || l.Capacity <= 0 {
		t.Errorf("%+v", l)
	}
}

// TestRepeatedConstants: the constants that tygo copies into the web app as literals repeat these values.
func TestRepeatedConstants(t *testing.T) {
	for got, want := range map[string]string{
		api.CodeInvalidRequest: auth.CodeInvalidRequest,
		api.CodeRateLimited:    auth.CodeRateLimited,
		api.InstancePath:       settings.InstancePath,
	} {
		if got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
}
