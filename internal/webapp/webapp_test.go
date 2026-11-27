package webapp_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/boundary"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/webapp"
	_ "github.com/EatWell-Studio/OSFM/migrations" // the OSFM collections (sessions)
)

const indexPage = "<!doctype html><title>test app</title>"

// testFS stands in for the built web app: Vite's hashed assets, the PWA files and the map's static files.
var testFS = fstest.MapFS{
	"index.html":                             {Data: []byte(indexPage)},
	"sw.js":                                  {Data: []byte("self.precacheAndRoute([])")},
	"registerSW.js":                          {Data: []byte("navigator.serviceWorker.register('/sw.js')")},
	"workbox-2fbc6a65.js":                    {Data: []byte("// workbox")},
	"manifest.webmanifest":                   {Data: []byte(`{"name":"OSFM"}`)},
	"pwa-192x192.png":                        {Data: []byte("\x89PNG\r\n\x1a\n")},
	"assets/index-Bps8T2MF.js":               {Data: []byte("console.log(1)")},
	"assets/index-COzaOkHp.css":              {Data: []byte("body{}")},
	"assets/maplibre-gl-worker-Boc9pDRP.mjs": {Data: []byte("self.onmessage=null")},
	"assets/unhashed.js":                     {Data: []byte("console.log(2)")},
	"map/sprites/light.json":                 {Data: []byte(`{}`)},
	"map/fonts/Noto Sans Regular/0-255.pbf":  {Data: []byte{0x0a, 0x00}},
	"map/NOTICE.txt":                         {Data: []byte("notice")},
}

// router builds the main listener's handler as apis.Serve does, without listening: PocketBase's router with its
// middlewares, then the OnServe hooks of the boundary, the session layer and the web app.
func router(t *testing.T, opts webapp.Options) http.Handler {
	t.Helper()
	app, err := tests.NewTestApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	store.BindHooks(app)
	boundary.Bind(app, boundary.Options{})
	auth.Bind(app, auth.Options{})
	if opts.FS == nil {
		opts.FS = testFS
	}
	webapp.Bind(app, opts)
	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: r}, func(*core.ServeEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	h, err := r.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func get(h http.Handler, method, path string, header ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// checkSecurityHeaders reports every security header of the main listener that a response lacks or gets wrong.
func checkSecurityHeaders(t *testing.T, what string, h http.Header, hsts bool) {
	t.Helper()
	want := map[string]string{
		"Content-Security-Policy":      "default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'",
		"Referrer-Policy":              "no-referrer",
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Permissions-Policy":           webapp.PermissionsPolicy,
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Strict-Transport-Security":    "",
		"X-Xss-Protection":             "", // PocketBase's default, replaced on the main listener
	}
	if hsts {
		want["Strict-Transport-Security"] = "max-age=31536000"
	}
	if !strings.Contains(webapp.PermissionsPolicy, "geolocation=(self)") {
		t.Errorf("the permissions policy does not allow geolocation: %s", webapp.PermissionsPolicy)
	}
	for name, value := range want {
		if got := h.Values(name); (value == "" && len(got) > 0) || (value != "" && (len(got) != 1 || got[0] != value)) {
			t.Errorf("%s: %s = %q, want %q", what, name, got, value)
		}
	}
	// No CORS on the main listener.
	for name := range h {
		if strings.HasPrefix(name, "Access-Control-") {
			t.Errorf("%s: CORS header %s: %q", what, name, h.Values(name))
		}
	}
}

func TestStaticFiles(t *testing.T) {
	h := router(t, webapp.Options{})
	for _, tc := range []struct {
		path, contentType, cache, body string
	}{
		{"/", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/index.html", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/sw.js", "text/javascript; charset=utf-8", webapp.CacheRevalidate, ""},
		{"/registerSW.js", "text/javascript; charset=utf-8", webapp.CacheRevalidate, ""},
		{"/workbox-2fbc6a65.js", "text/javascript; charset=utf-8", webapp.CacheRevalidate, ""},
		{"/manifest.webmanifest", "application/manifest+json", webapp.CacheRevalidate, `{"name":"OSFM"}`},
		{"/pwa-192x192.png", "image/png", webapp.CacheRevalidate, ""},
		{"/assets/index-Bps8T2MF.js", "text/javascript; charset=utf-8", webapp.CacheImmutable, "console.log(1)"},
		{"/assets/index-COzaOkHp.css", "text/css; charset=utf-8", webapp.CacheImmutable, ""},
		{"/assets/maplibre-gl-worker-Boc9pDRP.mjs", "text/javascript; charset=utf-8", webapp.CacheImmutable, ""},
		// Under assets/ but not named after its content: revalidated.
		{"/assets/unhashed.js", "text/javascript; charset=utf-8", webapp.CacheRevalidate, ""},
		{"/map/sprites/light.json", "application/json", webapp.CacheRevalidate, ""},
		{"/map/fonts/Noto%20Sans%20Regular/0-255.pbf", "application/x-protobuf", webapp.CacheRevalidate, ""},
		{"/map/NOTICE.txt", "text/plain; charset=utf-8", webapp.CacheRevalidate, "notice"},
		// Client-side routes: index.html.
		{"/list", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/me", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/admin", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		// The routes of M2 (web/src/routes.tsx): a place's address is also its share link, with ?r= for a recommendation.
		{"/new", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/p/0190a1b2-0000-7000-8000-00000000b001", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/p/0190a1b2-0000-7000-8000-00000000b001?r=0190a1b2-0000-7000-8000-00000000c001", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/p/0190a1b2-0000-7000-8000-00000000b001/edit", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/p/0190a1b2-0000-7000-8000-00000000b001/recommend", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/p/0190a1b2-0000-7000-8000-00000000b001/r/0190a1b2-0000-7000-8000-00000000c001/edit", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/p/0190a1b2-0000-7000-8000-00000000b001/r/0190a1b2-0000-7000-8000-00000000c001/history", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/admin/settings", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/admin/backup", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/login?next=%2Fme", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/register", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/some/client/route", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
		{"/list/", "text/html; charset=utf-8", webapp.CacheRevalidate, indexPage},
	} {
		rec := get(h, http.MethodGet, tc.path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: %d", tc.path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Content-Type"); got != tc.contentType {
			t.Errorf("GET %s: Content-Type %q, want %q", tc.path, got, tc.contentType)
		}
		if got := rec.Header().Get("Cache-Control"); got != tc.cache {
			t.Errorf("GET %s: Cache-Control %q, want %q", tc.path, got, tc.cache)
		}
		if tc.body != "" && rec.Body.String() != tc.body {
			t.Errorf("GET %s: body %q, want %q", tc.path, rec.Body.String(), tc.body)
		}
		checkSecurityHeaders(t, "GET "+tc.path, rec.Header(), false)

		// The ETag validates the cached copy.
		etag := rec.Header().Get("ETag")
		if !strings.HasPrefix(etag, `"`) || len(etag) != 34 {
			t.Errorf("GET %s: ETag %q", tc.path, etag)
			continue
		}
		if rec := get(h, http.MethodGet, tc.path, "If-None-Match", etag); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
			t.Errorf("GET %s with its ETag: %d, want 304", tc.path, rec.Code)
		}
		if rec := get(h, http.MethodHead, tc.path); rec.Code != http.StatusOK || rec.Body.Len() != 0 {
			t.Errorf("HEAD %s: %d with %d bytes", tc.path, rec.Code, rec.Body.Len())
		}
	}

	// Client-side routes share index.html's ETag; different files have different ones.
	if a, b := get(h, http.MethodGet, "/list").Header().Get("ETag"), get(h, http.MethodGet, "/").Header().Get("ETag"); a != b {
		t.Errorf("ETag of /list %s, of / %s", a, b)
	}
	if a, b := get(h, http.MethodGet, "/sw.js").Header().Get("ETag"), get(h, http.MethodGet, "/").Header().Get("ETag"); a == b {
		t.Error("sw.js and index.html have the same ETag")
	}
}

// TestMissingFiles checks that paths that look like files get 404, not the app's index.html.
func TestMissingFiles(t *testing.T) {
	h := router(t, webapp.Options{})
	for _, path := range []string{
		"/assets/index-AAAAAAAA.js", // a script of an earlier build
		"/assets/missing",
		"/assets/",
		"/assets",
		"/map",
		"/map/",
		"/map/fonts/",
		"/map/fonts/Noto%20Sans%20Regular/256-511.pbf", // a glyph range that is not provided
		"/map/fonts/Noto%20Sans%20Bold/0-255.pbf",
		"/map/sprites/dark.json",
		"/favicon.ico",
		"/robots.txt",
		"/list/style.css",
		"/.env",
		"/some/route/file.js",
	} {
		rec := get(h, http.MethodGet, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), indexPage) {
			t.Errorf("GET %s: the app's index.html", path)
		}
		checkSecurityHeaders(t, "GET "+path, rec.Header(), false)
	}
}

// TestHeadersOnEveryResponse checks the security headers on responses the web app does not make: the API's refusals
// (boundary, session layer), PocketBase's health check, and requests the router has no route for.
func TestHeadersOnEveryResponse(t *testing.T) {
	for _, hsts := range []bool{false, true} {
		h := router(t, webapp.Options{HSTS: hsts})
		for _, tc := range []struct {
			method, path string
			status       int
		}{
			{http.MethodGet, "/", http.StatusOK},
			{http.MethodGet, "/api/health", http.StatusOK},
			{http.MethodGet, auth.MePath, http.StatusUnauthorized},
			{http.MethodPost, auth.LogoutPath, http.StatusUnauthorized},
			{http.MethodGet, "/api/collections/members/records", http.StatusNotFound},
			{http.MethodGet, "/_/", http.StatusNotFound},
			{http.MethodDelete, "/list", http.StatusNotFound},
			{http.MethodGet, "/assets/missing.js", http.StatusNotFound},
		} {
			rec := get(h, tc.method, tc.path)
			if rec.Code != tc.status {
				t.Errorf("%s %s: %d, want %d", tc.method, tc.path, rec.Code, tc.status)
			}
			checkSecurityHeaders(t, tc.method+" "+tc.path, rec.Header(), hsts)
		}
	}
}

func TestHandlerNeedsIndex(t *testing.T) {
	if _, err := webapp.Handler(fstest.MapFS{"app.js": {Data: []byte("x")}}); err == nil {
		t.Error("no error for a web app without index.html")
	}
}

// freeAddr returns a loopback address with a port that was free a moment ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// TestServed checks the headers on a real server with the admin UI on: the main listener has the security headers
// and no CORS, for simple and preflight requests from another origin; the admin listener keeps PocketBase's own
// headers, content security policy and CORS, and serves the admin UI.
func TestServed(t *testing.T) {
	app, err := tests.NewTestApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.BindHooks(app)
	adminAddr := freeAddr(t)
	boundary.Bind(app, boundary.Options{AdminUI: true, AdminListen: adminAddr})
	auth.Bind(app, auth.Options{HostPrefix: true})
	webapp.Bind(app, webapp.Options{FS: testFS, HSTS: true})

	mainLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.Listener = mainLn
		return se.Next()
	})
	served := make(chan error, 1)
	go func() { served <- apis.Serve(app, apis.ServeConfig{HttpAddr: mainLn.Addr().String()}) }()
	t.Cleanup(func() {
		app.Cleanup()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	main, admin := "http://"+mainLn.Addr().String(), "http://"+adminAddr
	waitHealthy(t, main)
	waitHealthy(t, admin)

	const origin = "https://elsewhere.example"
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/"},
		{http.MethodGet, "/api/health"},
		{http.MethodGet, auth.MePath},
		{http.MethodGet, "/assets/index-Bps8T2MF.js"},
		{http.MethodOptions, auth.MePath},
		{http.MethodOptions, auth.LogoutPath},
	} {
		res := do(t, tc.method, main+tc.path, "Origin", origin, "Access-Control-Request-Method", "POST",
			"Access-Control-Request-Headers", "x-osfm")
		checkSecurityHeaders(t, "main listener "+tc.method+" "+tc.path, res.Header, true)
		if tc.method == http.MethodOptions && res.StatusCode < 400 {
			t.Errorf("main listener preflight %s: %d, want a refusal", tc.path, res.StatusCode)
		}
	}

	// The admin listener: PocketBase's admin UI with its own content security policy, PocketBase's headers and CORS.
	res := do(t, http.MethodGet, admin+"/_/")
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("admin UI: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if csp := res.Header.Get("Content-Security-Policy"); csp == webapp.ContentSecurityPolicy ||
		!strings.Contains(csp, "'unsafe-inline'") {
		t.Errorf("admin UI: Content-Security-Policy %q, want PocketBase's", csp)
	}
	for _, name := range []string{"Permissions-Policy", "Cross-Origin-Resource-Policy", "Strict-Transport-Security",
		"Referrer-Policy"} {
		if v := res.Header.Get(name); v != "" {
			t.Errorf("admin UI: %s = %q", name, v)
		}
	}
	if v := res.Header.Get("X-Frame-Options"); v != "SAMEORIGIN" {
		t.Errorf("admin UI: X-Frame-Options %q, want PocketBase's SAMEORIGIN", v)
	}
	res = do(t, http.MethodGet, admin+"/api/health", "Origin", origin)
	if v := res.Header.Get("Access-Control-Allow-Origin"); v != "*" {
		t.Errorf("admin listener: Access-Control-Allow-Origin %q, want PocketBase's default *", v)
	}
	if v := res.Header.Get("Content-Security-Policy"); v != "" {
		t.Errorf("admin listener API: Content-Security-Policy %q", v)
	}
}

var client = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// response is what the tests look at in a response: its status and headers (the body is read and closed by do).
type response struct {
	StatusCode int
	Header     http.Header
}

func do(t *testing.T, method, url string, header ...string) response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	return response{StatusCode: res.StatusCode, Header: res.Header}
}

func waitHealthy(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if res, err := client.Get(base + "/api/health"); err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s did not become healthy", base)
}
