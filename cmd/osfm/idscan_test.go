package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/federation"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// The probes of the internal ID scan, gathered from the per-feature files cmd/osfm/probes_<feature>_test.go, so that
// features developed side by side each change only their own file. Each file registers its part from an init
// function, for example (with paths a feature defines):
//
//	func init() {
//		registerProbes(probeSet{
//			probes: map[string][]idscan.Request{
//				"GET " + api.PlacePath: {
//					{Path: placePath(idscan.PlaceUUID), Status: http.StatusOK},
//					{Path: placePath("0190a1b2-0000-7000-8000-00000000ffff"), Status: http.StatusNotFound},
//				},
//				"POST " + api.PlacesPath: {
//					{Method: http.MethodPost, Path: api.PlacesPath, Header: csrfHeader, Body: `{…}`, Status: http.StatusCreated},
//				},
//			},
//			maintainerOnly: []string{"POST " + api.PlaceDeletePath},
//		})
//	}
//
// osfmProbes lists, for every route OSFM registers, the requests the internal ID scan sends to it. The test fails
// for a registered route without an entry here and for an entry whose route is no longer registered, so that a new
// route cannot be left out of the scan unnoticed. Probes should reach the route's real responses (successes as well as
// errors); give a Status to show that a probe reached the handler it is meant for.
//
// Every probe is sent as each of the identities below. Status is the status a signed-in caller (member, maintainer)
// gets, except that members get 403 from the routes in maintainerRoutes; anonymous probes of a path that needs a
// session (/api/osfm/*, /tiles/*) must get 401 instead, unless the route is in publicRoutes. State-changing probes of
// /api/osfm/* carry the X-OSFM header; the scan also sends them without it and expects 403.
var osfmProbes = map[string][]idscan.Request{}

// publicRoutes are the OSFM routes under /api/osfm/ that answer without a session (auth.Public): sign-in,
// registration and credential reset (E1). A route marked public in the code but missing here fails the scan, and so
// does the reverse.
var publicRoutes = map[string]bool{}

// maintainerRoutes are the OSFM routes only maintainers may use (auth.RequireMaintainer, api.MaintainersOnly): the
// member identity's probes of them must get 403, whatever their Status says.
var maintainerRoutes = map[string]bool{}

// probeSet is one file's part of the internal ID scan: the probes of its routes ("METHOD pattern" as the router has
// it), and which of those routes are public or maintainers only.
type probeSet struct {
	probes         map[string][]idscan.Request
	public         []string
	maintainerOnly []string
}

// registerProbes adds set to osfmProbes, publicRoutes and maintainerRoutes. A route registered twice is a mistake
// (two files probing the same route) and panics.
func registerProbes(set probeSet) {
	for route, probes := range set.probes {
		if _, dup := osfmProbes[route]; dup {
			panic("probes for " + route + " are registered twice")
		}
		osfmProbes[route] = probes
	}
	for _, route := range set.public {
		publicRoutes[route] = true
	}
	for _, route := range set.maintainerOnly {
		maintainerRoutes[route] = true
	}
}

var csrfHeader = http.Header{http.CanonicalHeaderKey(auth.CSRFHeader): {"1"}}

// identities are the callers the scan sends every request as. Each returns the request headers that make the caller,
// and the internal IDs it created on the way (which must not leak either); it is called for every request, so that a
// probe that ends the session (logout) does not affect the next one.
var identities = map[string]func(t testing.TB, app core.App, seeded *idscan.Seeded) (http.Header, []string){
	"anonymous": func(testing.TB, core.App, *idscan.Seeded) (http.Header, []string) { return nil, nil },
	"member": func(t testing.TB, app core.App, seeded *idscan.Seeded) (http.Header, []string) {
		return sessionHeader(t, app, seeded.Member)
	},
	"maintainer": func(t testing.TB, app core.App, seeded *idscan.Seeded) (http.Header, []string) {
		return sessionHeader(t, app, seeded.Maintainer)
	},
}

// sessionHeader starts a session for member and returns the Cookie header that carries it, and the session's ID.
func sessionHeader(t testing.TB, app core.App, member *core.Record) (http.Header, []string) {
	t.Helper()
	token, session, err := auth.CreateSession(app, member)
	if err != nil {
		t.Fatal(err)
	}
	return http.Header{"Cookie": {auth.SessionCookieName(app) + "=" + token}}, []string{session.Id}
}

// bootForScan boots an app on a new data directory through bind, as `osfm serve` does, seeds made-up records into
// every collection and builds the router with all routes. It returns the app, the seeded records, the routes
// PocketBase registers itself, the routes OSFM adds and the handler.
func bootForScan(t *testing.T) (app core.App, seeded *idscan.Seeded, pbRoutes, osfmRoutes []idscan.Route,
	h http.Handler) {
	t.Helper()
	app, _, seeded, r := bootServed(t, true)

	// PocketBase's own routes are the ones the same steps register on an app without OSFM.
	plain := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	plainRouter, err := apis.NewRouter(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.OnServe().Trigger(&core.ServeEvent{App: plain, Router: plainRouter}, noNext); err != nil {
		t.Fatal(err)
	}
	isPB := map[string]bool{}
	for _, route := range idscan.Routes(t, plainRouter) {
		isPB[route.String()] = true
	}
	for _, route := range idscan.Routes(t, r) {
		if isPB[route.String()] {
			pbRoutes = append(pbRoutes, route)
		} else {
			osfmRoutes = append(osfmRoutes, route)
		}
	}

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return app, seeded, pbRoutes, osfmRoutes, mux
}

// bootServed boots an app on a new data directory through bind and runs the OnServe hooks on a new router, as
// `osfm serve` does without listening. With seed, made-up records are written into every collection first (so the
// instance has a maintainer and cities); without, the instance starts unconfigured and generates a setup code.
func bootServed(t *testing.T, seed bool) (app core.App, cfg *config.Config, seeded *idscan.Seeded,
	r *router.Router[*core.RequestEvent]) {
	t.Helper()
	dir := t.TempDir()
	// The place search's upstream services at an address nothing listens on: no test reaches the public ones.
	cfg, err := config.Parse(testapp.UnreachableSearch(func(name string) (string, bool) {
		if name == config.EnvDataDir {
			return dir, true
		}
		return "", false
	}))
	if err != nil {
		t.Fatal(err)
	}
	app = core.NewBaseApp(core.BaseAppConfig{DataDir: cfg.PBDataDir()})
	bind(app, cfg)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })

	if seed {
		seeded = idscan.Seed(t, app)
	}

	// What apis.Serve does, without listening: PocketBase's router, then the OnServe hooks, where OSFM registers its
	// routes (and PocketBase a few of its own).
	if r, err = apis.NewRouter(app); err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: r}, noNext); err != nil {
		t.Fatal(err)
	}
	return app, cfg, seeded, r
}

func noNext(*core.ServeEvent) error { return nil }

// TestNoInternalIDsInResponses is the acceptance check of AD4: no response of any route contains the PocketBase
// internal ID of a record (or of a collection). It calls every OSFM route with the probes above and every PocketBase
// route with generic requests (each placeholder filled with every OSFM collection name, or with a dummy value), as
// each identity, and searches each response's headers and body for the seeded IDs. On the way it checks the session
// rules on every OSFM route: 401 without a session where one is needed (E1), 403 without the X-OSFM header (AD6).
func TestNoInternalIDsInResponses(t *testing.T) {
	app, seeded, pbRoutes, osfmRoutes, h := bootForScan(t)

	type probe struct {
		idscan.Request
		protected      bool // needs a session
		maintainerOnly bool // members get 403
	}
	var reqs []probe
	registered := map[string]bool{}
	for _, route := range osfmRoutes {
		registered[route.String()] = true
		probes, ok := osfmProbes[route.String()]
		if !ok || len(probes) == 0 {
			t.Errorf("route %q is not covered by the internal ID scan: add probes for it to osfmProbes", route)
		}
		for _, p := range probes {
			reqs = append(reqs, probe{p, needsSession(p.Path) && !publicRoutes[route.String()],
				maintainerRoutes[route.String()]})
		}
	}
	for pattern := range osfmProbes {
		if !registered[pattern] {
			t.Errorf("osfmProbes has probes for %q, which is not registered", pattern)
		}
	}
	for pattern := range publicRoutes {
		if !registered[pattern] {
			t.Errorf("publicRoutes has %q, which is not registered", pattern)
		}
	}
	for pattern := range maintainerRoutes {
		if !registered[pattern] {
			t.Errorf("maintainerRoutes has %q, which is not registered", pattern)
		}
	}

	collections := slices.Sorted(func(yield func(string) bool) {
		for name := range seeded.Records {
			if !yield(name) {
				return
			}
		}
	})
	values := map[string][]string{"collection": collections}
	for _, route := range pbRoutes {
		method, body := route.Method, ""
		switch method {
		case "":
			method = http.MethodGet
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			body = "{}"
		}
		for _, path := range idscan.Expand(route.Path, values, "x") {
			reqs = append(reqs, probe{Request: idscan.Request{Method: method, Path: path, Body: body}})
		}
	}

	for name, identity := range identities {
		t.Run(name, func(t *testing.T) {
			n := 0
			for _, p := range reqs {
				send := func(r idscan.Request) {
					header, created := identity(t, app, seeded)
					r.Header = r.Header.Clone()
					if r.Header == nil {
						r.Header = http.Header{}
					}
					for k, v := range header {
						r.Header[k] = v
					}
					n += idscan.Scan(t, h, []idscan.Request{r}, slices.Concat(seeded.IDs, created))
				}
				r := p.Request
				switch {
				case name == "anonymous" && p.protected:
					r.Status = http.StatusUnauthorized
				case name == "member" && p.maintainerOnly:
					r.Status = http.StatusForbidden
				}
				send(r)
				if name != "anonymous" && r.Header.Get(auth.CSRFHeader) != "" {
					r.Header = r.Header.Clone()
					r.Header.Del(auth.CSRFHeader)
					r.Status = http.StatusForbidden
					send(r)
				}
			}
			t.Logf("%d responses from %d OSFM and %d PocketBase routes checked for %d internal IDs", n,
				len(osfmRoutes), len(pbRoutes), len(seeded.IDs))
		})
	}
}

// needsSession reports whether a request path is one the session middleware protects.
func needsSession(path string) bool {
	return strings.HasPrefix(path, "/api/osfm/") || strings.HasPrefix(path, "/tiles/")
}

// TestPublicWithoutSession checks the paths that stay public as `osfm serve` serves them: the web app, the health
// check and the instance metadata, while the API and the tiles answer 401.
func TestPublicWithoutSession(t *testing.T) {
	_, _, _, _, h := bootForScan(t)
	for path, want := range map[string]int{
		"/":                                http.StatusOK,
		"/login":                           http.StatusOK,
		"/assets/no-such-file.js":          http.StatusNotFound, // not the app's index.html (T018)
		"/api/health":                      http.StatusOK,
		federation.WellKnownPath:           http.StatusOK,
		auth.MePath:                        http.StatusUnauthorized,
		"/tiles/":                          http.StatusUnauthorized,
		"/tiles/testcity.json":             http.StatusUnauthorized,
		"/tiles/testcity/10/1/1.mvt":       http.StatusUnauthorized,
		"/api/osfm/no/such/route":          http.StatusNotFound,
		"/api/collections/members/records": http.StatusNotFound,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("GET %s: %d, want %d", path, rec.Code, want)
		}
	}
}

// TestWellKnownServed checks /.well-known/osfm.json as the booted app serves it: public, with the key the instance
// generated in its data directory and nothing about the instance or its members.
func TestWellKnownServed(t *testing.T) {
	_, seeded, _, _, h := bootForScan(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, federation.WellKnownPath, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, Content-Type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var doc federation.Document
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.PublicKey.PublicKey(); err != nil {
		t.Error(err)
	}
	body := rec.Body.String()
	settings := seeded.Records[store.InstanceSettings][0]
	for _, text := range []string{settings.GetString("name"), "testcity", "Test City", "test_member", "Test Member",
		"Test Alias", "Test Noodle Bar"} {
		if text == "" || strings.Contains(body, text) {
			t.Errorf("the document contains %q", text)
		}
	}
}
