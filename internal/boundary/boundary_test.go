package boundary_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/cmd"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/boundary"
	"github.com/EatWell-Studio/OSFM/internal/store"
	_ "github.com/EatWell-Studio/OSFM/migrations" // the OSFM collections and settings
)

const (
	superuserEmail    = "superuser@example.test"
	superuserPassword = "test-superuser-password"
	spaPage           = "<!doctype html><title>test app</title>"
)

// instance is a running server: apis.Serve with the boundary bound, as `osfm serve` runs it, on ports chosen by the
// system. Besides PocketBase's routes it has a stand-in for the web app (the same catch-all route as cmd/osfm) and one
// OSFM route, /api/osfm/echo, which reports what reached it.
type instance struct {
	app         *tests.TestApp
	main, admin string // base URLs; admin is empty when the admin UI is off
	memberID    string
	memberToken string
	suToken     string
	installer   func(core.App, *core.Record, string) error // the ServeEvent's installer after the boundary's hook
	log         *bytes.Buffer
}

// echo is the response of the test OSFM route.
type echo struct {
	AuthHeader bool `json:"authHeader"`
	Auth       bool `json:"auth"`
}

// start serves a test app with the boundary bound. Each prepare function runs on the app before it serves: settings
// must be changed there, since PocketBase writes request logs in the background, reading the settings, and a later
// settings save races with the log entries of the health checks below.
func start(t *testing.T, adminUI bool, prepare ...func(core.App) error) *instance {
	t.Helper()
	app, err := tests.NewTestApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.BindHooks(app)
	if err := boundary.ApplyLogSettings(app); err != nil {
		t.Fatal(err)
	}
	in := &instance{app: app, log: &bytes.Buffer{}}
	in.seed(t)
	for _, f := range prepare {
		if err := f(app); err != nil {
			t.Fatal(err)
		}
	}

	adminLn := make(chan net.Listener, 1)
	restore := *boundary.ListenAdmin
	*boundary.ListenAdmin = func(network, addr string) (net.Listener, error) {
		ln, err := net.Listen(network, addr)
		if err == nil {
			adminLn <- ln
		}
		return ln, err
	}
	t.Cleanup(func() { *boundary.ListenAdmin = restore })

	// The test app clears the installer that apis.Serve sets; put it back for the boundary to deal with. (It never runs
	// here: a superuser exists.)
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.InstallerFunc = apis.DefaultInstallerFunc
		return se.Next()
	})
	var logMu sync.Mutex
	logger := slog.New(slog.NewTextHandler(lockedWriter{&logMu, in.log}, nil))
	boundary.Bind(app, boundary.Options{AdminUI: adminUI, AdminListen: "127.0.0.1:0", Logger: logger})

	mainLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		in.installer = se.InstallerFunc
		se.Router.GET(boundary.OSFMPrefix+"echo", func(e *core.RequestEvent) error {
			return e.JSON(http.StatusOK, echo{AuthHeader: e.Request.Header.Get("Authorization") != "", Auth: e.Auth != nil})
		})
		se.Router.GET("/{path...}", apis.Static(fstest.MapFS{"index.html": {Data: []byte(spaPage)}}, true))
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

	in.main = "http://" + mainLn.Addr().String()
	waitHealthy(t, in.main)
	if adminUI {
		in.admin = "http://" + (<-adminLn).Addr().String()
		waitHealthy(t, in.admin)
	}
	return in
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// seed creates a member and a superuser, with a token each.
func (in *instance) seed(t *testing.T) {
	t.Helper()
	member, err := auth.NewMemberRecord(in.app)
	if err != nil {
		t.Fatal(err)
	}
	member.Set("login_name", "test_member")
	member.Set("display_name", "Test Member")
	member.Set("role", "member")
	member.SetEmail("member@example.test")
	if err := in.app.Save(member); err != nil {
		t.Fatal(err)
	}
	in.memberID = member.Id
	// A PocketBase auth token for the member, as PocketBase itself would issue one. OSFM never issues these (members
	// have session cookies, AD6); the tests check that such a token carries no weight anyway.
	if in.memberToken, err = member.NewAuthToken(); err != nil {
		t.Fatal(err)
	}

	superusers, err := in.app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatal(err)
	}
	su := core.NewRecord(superusers)
	su.SetEmail(superuserEmail)
	su.SetPassword(superuserPassword)
	if err := in.app.Save(su); err != nil {
		t.Fatal(err)
	}
	if in.suToken, err = su.NewAuthToken(); err != nil {
		t.Fatal(err)
	}
}

var client = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

type response struct {
	status      int
	contentType string
	body        string
}

func do(t *testing.T, method, url, token, body string) response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{res.StatusCode, res.Header.Get("Content-Type"), string(b)}
}

func waitHealthy(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		res, err := client.Get(base + boundary.HealthPath)
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not become healthy: %v", base, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type request struct{ method, path, body string }

// closedRequests are PocketBase's endpoints, the admin UI and unknown API paths: none may be served on the main
// listener. IDs are real, so that without the boundary these requests would reach their handlers.
func closedRequests(in *instance) []request {
	m := "/api/collections/" + store.Members
	su := "/api/collections/" + core.CollectionNameSuperusers
	creds := `{"identity":"member@example.test","password":"wrong-password"}`
	reqs := []request{
		// Sign-in, token refresh and account flows of the members collection.
		{http.MethodGet, m + "/auth-methods", ""},
		{http.MethodPost, m + "/auth-with-password", creds},
		{http.MethodPost, m + "/request-otp", `{"email":"member@example.test"}`},
		{http.MethodPost, m + "/auth-with-otp", `{"otpId":"x","password":"12345678"}`},
		{http.MethodPost, m + "/auth-refresh", ""},
		{http.MethodPost, m + "/auth-with-oauth2", `{"provider":"google","code":"x","codeVerifier":"x","redirectURL":"x"}`},
		{http.MethodGet, "/api/oauth2-redirect?code=x&state=x", ""},
		{http.MethodPost, "/api/oauth2-redirect", "code=x&state=x"},
		{http.MethodPost, m + "/request-password-reset", `{"email":"member@example.test"}`},
		{http.MethodPost, m + "/confirm-password-reset", `{"token":"x","password":"x","passwordConfirm":"x"}`},
		{http.MethodPost, m + "/request-verification", `{"email":"member@example.test"}`},
		{http.MethodPost, m + "/confirm-verification", `{"token":"x"}`},
		{http.MethodPost, m + "/request-email-change", `{"newEmail":"other@example.test"}`},
		{http.MethodPost, m + "/confirm-email-change", `{"token":"x","password":"x"}`},
		{http.MethodPost, m + "/impersonate/" + in.memberID, `{}`},
		// Superuser sign-in.
		{http.MethodGet, su + "/auth-methods", ""},
		{http.MethodPost, su + "/auth-with-password",
			`{"identity":"` + superuserEmail + `","password":"` + superuserPassword + `"}`},
		{http.MethodPost, su + "/request-otp", `{"email":"` + superuserEmail + `"}`},
		{http.MethodPost, su + "/auth-refresh", ""},
		// Collection and admin APIs.
		{http.MethodGet, "/api/collections", ""},
		{http.MethodPost, "/api/collections", `{}`},
		{http.MethodGet, m, ""},
		{http.MethodPatch, m, `{}`},
		{http.MethodDelete, m, ""},
		{http.MethodDelete, m + "/truncate", ""},
		{http.MethodPut, "/api/collections/import", `{}`},
		{http.MethodGet, "/api/collections/meta/scaffolds", ""},
		{http.MethodGet, "/api/settings", ""},
		{http.MethodPatch, "/api/settings", `{}`},
		{http.MethodPost, "/api/settings/test/email", `{}`},
		{http.MethodGet, "/api/logs", ""},
		{http.MethodGet, "/api/logs/stats", ""},
		{http.MethodGet, "/api/backups", ""},
		{http.MethodPost, "/api/backups", `{}`},
		{http.MethodGet, "/api/crons", ""},
		{http.MethodPost, "/api/sql", `{"query":"SELECT 1"}`},
		{http.MethodPost, "/api/batch", `{"requests":[]}`},
		{http.MethodGet, "/api/realtime", ""},
		{http.MethodPost, "/api/realtime", `{}`},
		{http.MethodPost, "/api/files/token", ""},
		{http.MethodGet, "/api/files/" + store.Members + "/" + in.memberID + "/x.png", ""},
		// Admin UI.
		{http.MethodGet, "/_", ""},
		{http.MethodGet, "/_/", ""},
		{http.MethodGet, "/_/index.html", ""},
		{http.MethodGet, "/_/images/favicon/favicon.png", ""},
		// Unknown API paths, which would otherwise fall through to the web app.
		{http.MethodGet, "/api", ""},
		{http.MethodGet, "/api/", ""},
		{http.MethodGet, "/api/unknown", ""},
		{http.MethodPost, "/api/unknown", `{}`},
		{http.MethodGet, "/api/osfm", ""},
		{http.MethodGet, "/api/osfm/", ""},
		{http.MethodGet, "/api/osfm/unknown", ""},
		{http.MethodDelete, boundary.OSFMPrefix + "echo", ""},
		{http.MethodGet, boundary.HealthPath + "/x", ""},
		// Escaped paths that the router and URL.Path read differently.
		{http.MethodGet, "/api%2Fcollections%2Fmembers%2Frecords", ""},
		{http.MethodGet, "/%61pi/collections/" + store.Members + "/records", ""},
		{http.MethodHead, "/api/collections", ""},
	}
	// Record CRUD on every kind of collection.
	for _, c := range []string{store.Members, store.Credentials, store.InstanceSettings, core.CollectionNameSuperusers} {
		base := "/api/collections/" + c + "/records"
		reqs = append(reqs,
			request{http.MethodGet, base, ""},
			request{http.MethodGet, base + "/" + in.memberID, ""},
			request{http.MethodPost, base, `{}`},
			request{http.MethodPatch, base + "/" + in.memberID, `{}`},
			request{http.MethodDelete, base + "/" + in.memberID, ""},
		)
	}
	return reqs
}

func assertClosed(t *testing.T, in *instance) {
	t.Helper()
	for _, r := range closedRequests(in) {
		for _, who := range []struct{ name, token string }{
			{"anonymous", ""}, {"member", in.memberToken}, {"superuser", in.suToken},
		} {
			res := do(t, r.method, in.main+r.path, who.token, r.body)
			if res.status != http.StatusNotFound || !strings.HasPrefix(res.contentType, "application/json") {
				t.Errorf("%s %s %s: %d %s, want a JSON 404\n%.200s", who.name, r.method, r.path, res.status,
					res.contentType, res.body)
				continue
			}
			if r.method != http.MethodHead && !strings.Contains(res.body, `"status":404`) {
				t.Errorf("%s %s %s: body %.200s", who.name, r.method, r.path, res.body)
			}
		}
	}
}

func assertOpen(t *testing.T, in *instance) {
	t.Helper()
	if res := do(t, http.MethodGet, in.main+boundary.HealthPath, "", ""); res.status != http.StatusOK ||
		!strings.Contains(res.body, "API is healthy") {
		t.Errorf("health: %d %s", res.status, res.body)
	}
	if res := do(t, http.MethodHead, in.main+boundary.HealthPath, "", ""); res.status != http.StatusOK {
		t.Errorf("HEAD health: %d", res.status)
	}
	// The web app answers every other path, client-side routes included.
	for _, path := range []string{"/", "/some/client/route", "/apis", "/_x", "/api-docs"} {
		res := do(t, http.MethodGet, in.main+path, "", "")
		if res.status != http.StatusOK || res.body != spaPage {
			t.Errorf("GET %s: %d %.100s, want the web app", path, res.status, res.body)
		}
	}
	// An OSFM route is served, and never sees a PocketBase token.
	for _, token := range []string{"", in.memberToken, in.suToken} {
		res := do(t, http.MethodGet, in.main+boundary.OSFMPrefix+"echo", token, "")
		var got echo
		if res.status != http.StatusOK || json.Unmarshal([]byte(res.body), &got) != nil {
			t.Errorf("OSFM route: %d %s", res.status, res.body)
			continue
		}
		if got.AuthHeader || got.Auth {
			t.Errorf("OSFM route with token %.10q…: %+v, want no Authorization header and no auth", token, got)
		}
	}
}

func TestMainListenerAdminUIOff(t *testing.T) {
	in := start(t, false)
	assertClosed(t, in)
	assertOpen(t, in)
	if in.installer != nil {
		t.Error("the first-start installer is still set with the admin UI off")
	}
	if _, err := in.app.FindAuthRecordByEmail(core.CollectionNameSuperusers, core.DefaultInstallerEmail); err == nil {
		t.Error("the installer's placeholder superuser was created")
	}
}

func TestMainListenerAdminUIOn(t *testing.T) {
	in := start(t, true)
	assertClosed(t, in)
	assertOpen(t, in)
}

// TestAdminListener checks that the admin listener serves the admin UI and PocketBase's APIs to a superuser, and that
// even there the members collection has no working sign-in method.
func TestAdminListener(t *testing.T) {
	in := start(t, true)

	res := do(t, http.MethodGet, in.admin+"/_/", "", "")
	if res.status != http.StatusOK || !strings.HasPrefix(res.contentType, "text/html") {
		t.Errorf("admin UI: %d %s", res.status, res.contentType)
	}
	if res := do(t, http.MethodGet, in.admin+"/_", "", ""); res.status != http.StatusTemporaryRedirect {
		t.Errorf("admin UI without the slash: %d", res.status)
	}

	res = do(t, http.MethodPost, in.admin+"/api/collections/"+core.CollectionNameSuperusers+"/auth-with-password", "",
		`{"identity":"`+superuserEmail+`","password":"`+superuserPassword+`"}`)
	var signIn struct{ Token string }
	if res.status != http.StatusOK || json.Unmarshal([]byte(res.body), &signIn) != nil || signIn.Token == "" {
		t.Fatalf("superuser sign-in on the admin listener: %d %s", res.status, res.body)
	}
	for _, path := range []string{"/api/collections", "/api/settings", "/api/logs",
		"/api/collections/" + store.Members + "/records"} {
		if res := do(t, http.MethodGet, in.admin+path, signIn.Token, ""); res.status != http.StatusOK {
			t.Errorf("superuser GET %s on the admin listener: %d %.200s", path, res.status, res.body)
		}
		// The same token on the main listener.
		if res := do(t, http.MethodGet, in.main+path, signIn.Token, ""); res.status != http.StatusNotFound {
			t.Errorf("superuser GET %s on the main listener: %d", path, res.status)
		}
	}

	// The members collection reports every sign-in method off, and refuses them.
	res = do(t, http.MethodGet, in.admin+"/api/collections/"+store.Members+"/auth-methods", "", "")
	var methods struct {
		Password, OAuth2, MFA, OTP struct{ Enabled bool }
	}
	if res.status != http.StatusOK || json.Unmarshal([]byte(res.body), &methods) != nil {
		t.Fatalf("members auth-methods: %d %s", res.status, res.body)
	}
	if methods.Password.Enabled || methods.OAuth2.Enabled || methods.MFA.Enabled || methods.OTP.Enabled {
		t.Errorf("members auth-methods: %s", res.body)
	}
	m := "/api/collections/" + store.Members
	for _, r := range []request{
		{http.MethodPost, m + "/auth-with-password", `{"identity":"member@example.test","password":"x"}`},
		{http.MethodPost, m + "/request-otp", `{"email":"member@example.test"}`},
		{http.MethodPost, m + "/auth-with-oauth2", `{"provider":"google","code":"x","codeVerifier":"x","redirectURL":"x"}`},
		{http.MethodPost, m + "/auth-refresh", ""},
	} {
		res := do(t, r.method, in.admin+r.path, in.memberToken, r.body)
		if res.status < 400 || strings.Contains(res.body, `"token"`) {
			t.Errorf("%s %s on the admin listener: %d %.200s, want a refusal", r.method, r.path, res.status, res.body)
		}
	}
}

func TestInstallerPointsAtTheAdminListener(t *testing.T) {
	in := start(t, true)
	if in.installer == nil {
		t.Fatal("no installer with the admin UI on")
	}
	su, err := in.app.FindAuthRecordByEmail(core.CollectionNameSuperusers, superuserEmail)
	if err != nil {
		t.Fatal(err)
	}
	if err := in.installer(in.app, su, in.main); err != nil {
		t.Fatal(err)
	}
	if want := in.admin + "/_/#/pbinstall/"; !strings.Contains(in.log.String(), want) {
		t.Errorf("installer log does not contain %s:\n%s", want, in.log.String())
	}
}

// TestRateLimitCoversOSFMRoutes checks that PocketBase's rate limiter applies to the OSFM routes.
func TestRateLimitCoversOSFMRoutes(t *testing.T) {
	in := start(t, false, func(app core.App) error {
		s := app.Settings()
		if !s.RateLimits.Enabled {
			return errors.New("rate limits are off")
		}
		s.RateLimits.Rules = append([]core.RateLimitRule{{Label: boundary.OSFMPrefix, MaxRequests: 2, Duration: 60}},
			s.RateLimits.Rules...)
		return app.Save(s)
	})
	for i, want := range []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests} {
		if res := do(t, http.MethodGet, in.main+boundary.OSFMPrefix+"echo", "", ""); res.status != want {
			t.Errorf("request %d: %d, want %d", i+1, res.status, want)
		}
	}
}

// TestRequestLog checks what a request log entry holds: no IP address, no record ID and no query string.
func TestRequestLog(t *testing.T) {
	in := start(t, false)
	if res := do(t, http.MethodGet, in.main+boundary.OSFMPrefix+"echo?q=secret-search-term&lat=1.23456", "", ""); res.status != http.StatusOK {
		t.Fatalf("echo: %d", res.status)
	}
	if res := do(t, http.MethodGet, in.main+"/api/collections?filter=secret-filter", "", ""); res.status != http.StatusNotFound {
		t.Fatalf("closed path: %d", res.status)
	}

	var logs []*core.Log
	deadline := time.Now().Add(10 * time.Second)
	for len(logs) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("request log entries not written: %d", len(logs))
		}
		time.Sleep(100 * time.Millisecond)
		logs = nil
		if err := in.app.LogQuery().AndWhere(dbx.Like("message", "/api/")).All(&logs); err != nil {
			t.Fatal(err)
		}
	}
	for _, l := range logs {
		raw, err := json.Marshal(l.Data)
		if err != nil {
			t.Fatal(err)
		}
		entry := l.Message + " " + string(raw)
		for _, unwanted := range []string{"userIP", "remoteIP", "127.0.0.1", "authId", "secret-", "1.23456", "?"} {
			if strings.Contains(entry, unwanted) {
				t.Errorf("log entry contains %q: %s", unwanted, entry)
			}
		}
	}
}

// TestLogSettingsAppliedAtBootstrap checks that Bind restores the log settings at every start.
func TestLogSettingsAppliedAtBootstrap(t *testing.T) {
	dir := t.TempDir()
	bootstrap := func() core.App {
		app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
		boundary.Bind(app, boundary.Options{})
		if err := app.Bootstrap(); err != nil {
			t.Fatal(err)
		}
		return app
	}
	check := func(app core.App) {
		t.Helper()
		if l := app.Settings().Logs; l.MaxDays != boundary.LogMaxDays || l.LogIP || l.LogAuthId {
			t.Errorf("log settings %+v", l)
		}
	}

	app := bootstrap()
	check(app)
	s := app.Settings()
	s.Logs.MaxDays, s.Logs.LogIP, s.Logs.LogAuthId = 90, true, true
	if err := app.Save(s); err != nil {
		t.Fatal(err)
	}
	if err := app.ClearBootstrap(); err != nil {
		t.Fatal(err)
	}
	app = bootstrap()
	defer func() { _ = app.ClearBootstrap() }()
	check(app)
}

// TestTrustedProxyAppliedAtBootstrap: OSFM_TRUSTED_PROXY_HEADERS replaces the trusted proxy setting at every
// bootstrap, with the rightmost address; without it, a value set in the admin UI stays.
func TestTrustedProxyAppliedAtBootstrap(t *testing.T) {
	dir := t.TempDir()
	bootstrap := func(headers []string) core.App {
		app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
		boundary.Bind(app, boundary.Options{TrustedProxyHeaders: headers})
		if err := app.Bootstrap(); err != nil {
			t.Fatal(err)
		}
		return app
	}
	check := func(app core.App, want []string) {
		t.Helper()
		if p := app.Settings().TrustedProxy; !slices.Equal(p.Headers, want) || p.UseLeftmostIP {
			t.Errorf("trusted proxy settings %+v, want %q, rightmost", p, want)
		}
		if err := app.ClearBootstrap(); err != nil {
			t.Fatal(err)
		}
	}

	check(bootstrap(nil), []string{})
	app := bootstrap([]string{"X-Forwarded-For"})
	s := app.Settings()
	s.TrustedProxy.Headers, s.TrustedProxy.UseLeftmostIP = []string{"X-Real-Ip"}, true
	if err := app.Save(s); err != nil {
		t.Fatal(err)
	}
	if err := app.ClearBootstrap(); err != nil {
		t.Fatal(err)
	}
	check(bootstrap([]string{"X-Forwarded-For"}), []string{"X-Forwarded-For"})
	app = bootstrap(nil)
	s = app.Settings()
	s.TrustedProxy.Headers = []string{"X-Real-Ip"}
	if err := app.Save(s); err != nil {
		t.Fatal(err)
	}
	if err := app.ClearBootstrap(); err != nil {
		t.Fatal(err)
	}
	check(bootstrap(nil), []string{"X-Real-Ip"})
}

// TestSuperuserCommand checks that `osfm superuser upsert` still creates a superuser who can sign in on the admin
// listener.
func TestSuperuserCommand(t *testing.T) {
	in := start(t, true)
	c := cmd.NewSuperuserCommand(in.app)
	c.SetArgs([]string{"upsert", "operator@example.test", "operator-password-1"})
	c.SetOut(io.Discard)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	body := `{"identity":"operator@example.test","password":"operator-password-1"}`
	path := "/api/collections/" + core.CollectionNameSuperusers + "/auth-with-password"
	if res := do(t, http.MethodPost, in.admin+path, "", body); res.status != http.StatusOK {
		t.Errorf("sign-in on the admin listener: %d %s", res.status, res.body)
	}
	if res := do(t, http.MethodPost, in.main+path, "", body); res.status != http.StatusNotFound {
		t.Errorf("sign-in on the main listener: %d %s", res.status, res.body)
	}
}
