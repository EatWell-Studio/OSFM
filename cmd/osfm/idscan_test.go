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

	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/federation"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// osfmProbes lists, for every route OSFM registers, the requests the internal ID scan sends to it. The test fails
// for a registered route without an entry here and for an entry whose route is no longer registered, so that a new
// route cannot be left out of the scan unnoticed. Probes should reach the route's real responses (successes as well as
// errors); give a Status to show that a probe reached the handler it is meant for.
//
// T007 and later: once routes need a session, scan them as each identity too (see identities below).
var osfmProbes = map[string][]idscan.Request{
	"GET " + federation.WellKnownPath: {
		{Path: federation.WellKnownPath, Status: http.StatusOK},
	},
	// No tile archive in the test data directory: the tile handler reads only the tile files, never the database.
	"GET /tiles/{path...}": {
		{Path: "/tiles/testcity.json", Status: http.StatusNotFound},
		{Path: "/tiles/testcity/12/3370/1665.mvt", Status: http.StatusNotFound},
		{Path: "/tiles/bad..name/1/1/1.mvt", Status: http.StatusNotFound},
	},
	"GET /{path...}": {
		{Path: "/", Status: http.StatusOK},
		{Path: "/index.html"},
		{Path: "/map", Status: http.StatusOK},
		{Path: "/some/client/route?next=%2Fme", Status: http.StatusOK},
		{Path: "/manifest.webmanifest"},
	},
}

// identities are the callers the scan sends every request as: a function that adds the caller's credentials to a
// request. Anonymous only until member sessions exist (T007); add a member and a maintainer then.
var identities = map[string]func(seeded *idscan.Seeded) http.Header{
	"anonymous": func(*idscan.Seeded) http.Header { return nil },
}

// bootForScan boots an app on a new data directory through bind, as `osfm serve` does, seeds made-up records into
// every collection and builds the router with all routes. It returns the routes PocketBase registers itself, the
// routes OSFM adds and the handler.
func bootForScan(t *testing.T) (seeded *idscan.Seeded, pbRoutes, osfmRoutes []idscan.Route, h http.Handler) {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.Parse(func(name string) (string, bool) {
		if name == config.EnvDataDir {
			return dir, true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: cfg.PBDataDir()})
	bind(app, cfg)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })

	seeded = idscan.Seed(t, app)

	// What apis.Serve does, without listening: PocketBase's router, then the OnServe hooks, where OSFM registers its
	// routes (and PocketBase a few of its own).
	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: r}, noNext); err != nil {
		t.Fatal(err)
	}

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
	return seeded, pbRoutes, osfmRoutes, mux
}

func noNext(*core.ServeEvent) error { return nil }

// TestNoInternalIDsInResponses is the acceptance check of AD4: no response of any route contains the PocketBase
// internal ID of a record (or of a collection). It calls every OSFM route with the probes above and every PocketBase
// route with generic requests (each placeholder filled with every OSFM collection name, or with a dummy value), and
// searches each response's headers and body for the seeded IDs.
func TestNoInternalIDsInResponses(t *testing.T) {
	seeded, pbRoutes, osfmRoutes, h := bootForScan(t)

	var reqs []idscan.Request
	registered := map[string]bool{}
	for _, route := range osfmRoutes {
		registered[route.String()] = true
		probes, ok := osfmProbes[route.String()]
		if !ok || len(probes) == 0 {
			t.Errorf("route %q is not covered by the internal ID scan: add probes for it to osfmProbes", route)
		}
		reqs = append(reqs, probes...)
	}
	for pattern := range osfmProbes {
		if !registered[pattern] {
			t.Errorf("osfmProbes has probes for %q, which is not registered", pattern)
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
			reqs = append(reqs, idscan.Request{Method: method, Path: path, Body: body})
		}
	}

	for name, identity := range identities {
		t.Run(name, func(t *testing.T) {
			header := identity(seeded)
			scoped := make([]idscan.Request, len(reqs))
			for i, r := range reqs {
				r.Header = header
				scoped[i] = r
			}
			n := idscan.Scan(t, h, scoped, seeded.IDs)
			t.Logf("%d responses from %d OSFM and %d PocketBase routes checked for %d internal IDs", n,
				len(osfmRoutes), len(pbRoutes), len(seeded.IDs))
		})
	}
}

// TestWellKnownServed checks /.well-known/osfm.json as the booted app serves it: public, with the key the instance
// generated in its data directory and nothing about the instance or its members.
func TestWellKnownServed(t *testing.T) {
	seeded, _, _, h := bootForScan(t)
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
