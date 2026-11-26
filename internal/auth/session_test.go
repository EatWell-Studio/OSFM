package auth_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/boundary"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
	"github.com/EatWell-Studio/OSFM/internal/store"
	_ "github.com/EatWell-Studio/OSFM/migrations" // the OSFM collections
)

// Test routes, besides the session layer's own: a protected one, a public one and a public sign-in stand-in.
const (
	echoPath   = boundary.OSFMPrefix + "test/echo"
	publicPath = boundary.OSFMPrefix + "test/public"
	signInPath = boundary.OSFMPrefix + "test/signin"
	spaPage    = "<!doctype html><title>test app</title>"
)

type env struct {
	t   *testing.T
	app *tests.TestApp
	h   http.Handler
}

// newEnv returns a test app with the boundary and the session layer bound, and a handler built as apis.Serve builds
// it (PocketBase's router with its middlewares, rate limiter included, then the OnServe hooks). prepare runs before
// the router is built.
func newEnv(t *testing.T, prepare ...func(core.App)) *env {
	t.Helper()
	app, err := tests.NewTestApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	store.BindHooks(app)
	boundary.Bind(app, boundary.Options{})
	auth.Bind(app)
	for _, f := range prepare {
		f(app)
	}

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.Router.GET(echoPath, func(e *core.RequestEvent) error {
			m := auth.CurrentMember(e)
			return e.JSON(http.StatusOK, map[string]any{"uuid": m.GetString(store.FieldUUID), "auth": e.Auth != nil})
		})
		se.Router.POST(echoPath, func(e *core.RequestEvent) error { return e.NoContent(http.StatusNoContent) })
		auth.Public(se.Router.GET(publicPath, func(e *core.RequestEvent) error {
			return e.JSON(http.StatusOK, map[string]any{"signedIn": auth.CurrentMember(e) != nil})
		}))
		auth.Public(se.Router.POST(publicPath, func(e *core.RequestEvent) error { return e.NoContent(http.StatusNoContent) }))
		// Stands in for the sign-in endpoints of later tasks: signs in the member with the given login name.
		auth.Public(se.Router.POST(signInPath, func(e *core.RequestEvent) error {
			m, err := e.App.FindFirstRecordByData(store.Members, "login_name", e.Request.URL.Query().Get("login"))
			if err != nil {
				return e.NotFoundError("", err)
			}
			if err := auth.SignIn(e, m); err != nil {
				if errors.Is(err, auth.ErrCannotSignIn) {
					return e.ForbiddenError("", nil)
				}
				return err
			}
			return e.NoContent(http.StatusNoContent)
		}))
		se.Router.GET("/tiles/{path...}", func(e *core.RequestEvent) error { return e.String(http.StatusOK, "tile") })
		se.Router.GET("/{path...}", func(e *core.RequestEvent) error { return e.HTML(http.StatusOK, spaPage) })
		return se.Next()
	})
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
	return &env{t: t, app: app, h: h}
}

func (en *env) member(login, role string) *core.Record {
	en.t.Helper()
	r, err := auth.NewMemberRecord(en.app)
	if err != nil {
		en.t.Fatal(err)
	}
	r.Set("login_name", login)
	r.Set("display_name", "Test "+login)
	r.Set("role", role)
	if err := en.app.Save(r); err != nil {
		en.t.Fatal(err)
	}
	return r
}

// session creates a session for member and returns its cookie token.
func (en *env) session(member *core.Record) string {
	en.t.Helper()
	token, _, err := auth.CreateSession(en.app, member)
	if err != nil {
		en.t.Fatal(err)
	}
	return token
}

// req is a request to the test handler; token is the session cookie value (none when empty); headers are name, value
// pairs.
func (en *env) req(method, path, token string, headers ...string) *httptest.ResponseRecorder {
	en.t.Helper()
	r := httptest.NewRequest(method, path, nil)
	if token != "" {
		r.Header.Add("Cookie", auth.CookieName+"="+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	en.h.ServeHTTP(rec, r)
	return rec
}

func (en *env) status(method, path, token string, headers ...string) int {
	en.t.Helper()
	return en.req(method, path, token, headers...).Code
}

func (en *env) sessionCount(member *core.Record) int64 {
	en.t.Helper()
	n, err := en.app.CountRecords(store.Sessions, dbx.HashExp{"member": member.Id})
	if err != nil {
		en.t.Fatal(err)
	}
	return n
}

// responseCookie returns the session cookie a response sets, or nil.
func responseCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	return nil
}

const csrf = auth.CSRFHeader

// TestDefaultDeny checks that without a session the OSFM API and the tiles answer 401 as JSON, while the routes
// marked public, the health check and the web app stay reachable.
func TestDefaultDeny(t *testing.T) {
	en := newEnv(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, auth.MePath},
		{http.MethodPost, auth.LogoutPath},
		{http.MethodGet, echoPath},
		{http.MethodPost, echoPath},
		{http.MethodGet, "/tiles/"},
		{http.MethodHead, "/tiles/"},
		{http.MethodGet, "/tiles/testcity.json"},
		{http.MethodGet, "/tiles/testcity/12/3370/1665.mvt"},
	} {
		rec := en.req(tc.method, tc.path, "", csrf, "1")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d, want 401", tc.method, tc.path, rec.Code)
			continue
		}
		if tc.method == http.MethodHead {
			continue
		}
		var body struct {
			Status int `json:"status"`
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") ||
			json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Status != http.StatusUnauthorized {
			t.Errorf("%s %s: %s %q, want a JSON error", tc.method, tc.path, ct, rec.Body.String())
		}
	}
	// The same with a cookie that names no session.
	if got := en.status(http.MethodGet, auth.MePath, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"); got != http.StatusUnauthorized {
		t.Errorf("unknown token: %d", got)
	}

	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, publicPath, http.StatusOK},
		{http.MethodGet, boundary.HealthPath, http.StatusOK},
		{http.MethodGet, "/", http.StatusOK},
		{http.MethodGet, "/list", http.StatusOK},
		{http.MethodGet, boundary.OSFMPrefix + "no/such/route", http.StatusNotFound},
	} {
		if got := en.status(tc.method, tc.path, ""); got != tc.want {
			t.Errorf("%s %s without a session: %d, want %d", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestMe(t *testing.T) {
	en := newEnv(t)
	for _, role := range []string{auth.RoleMember, auth.RoleMaintainer} {
		m := en.member("test_"+role, role)
		rec := en.req(http.MethodGet, auth.MePath, en.session(m))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", role, rec.Code, rec.Body)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control %q", cc)
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"uuid": m.GetString(store.FieldUUID), "displayName": "Test test_" + role, "role": role}
		if len(got) != len(want) {
			t.Errorf("%s: %v, want %v", role, got, want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: %s = %v, want %v", role, k, got[k], v)
			}
		}
	}
}

// TestCSRFHeader checks that state-changing requests to /api/osfm/* need X-OSFM: 1, on public routes too.
func TestCSRFHeader(t *testing.T) {
	en := newEnv(t)
	m := en.member("test_member", auth.RoleMember)
	token := en.session(m)

	for _, header := range [][]string{nil, {csrf, "0"}, {csrf, ""}, {csrf, "true"}} {
		for _, path := range []string{echoPath, auth.LogoutPath} {
			if got := en.status(http.MethodPost, path, token, header...); got != http.StatusForbidden {
				t.Errorf("POST %s with %v: %d, want 403", path, header, got)
			}
		}
		if got := en.status(http.MethodPost, publicPath, "", header...); got != http.StatusForbidden {
			t.Errorf("public POST with %v: %d, want 403", header, got)
		}
		if got := en.status(http.MethodPost, signInPath+"?login=test_member", "", header...); got != http.StatusForbidden {
			t.Errorf("sign-in with %v: %d, want 403", header, got)
		}
	}
	// The refused logout left the session alone.
	if got := en.status(http.MethodGet, auth.MePath, token); got != http.StatusOK {
		t.Errorf("session after refused logouts: %d", got)
	}
	if got := en.status(http.MethodPost, echoPath, token, csrf, "1"); got != http.StatusNoContent {
		t.Errorf("POST with the header: %d", got)
	}
	if got := en.status(http.MethodPost, publicPath, "", csrf, "1"); got != http.StatusNoContent {
		t.Errorf("public POST with the header: %d", got)
	}
	// Without a session, a protected route answers 401 whether or not the header is there.
	if got := en.status(http.MethodPost, echoPath, ""); got != http.StatusUnauthorized {
		t.Errorf("POST without session and header: %d", got)
	}
}

func TestSignIn(t *testing.T) {
	en := newEnv(t)
	m := en.member("test_member", auth.RoleMember)
	start := time.Now()

	rec := en.req(http.MethodPost, signInPath+"?login=test_member", "", csrf, "1")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("sign-in: %d %s", rec.Code, rec.Body)
	}
	c := responseCookie(rec)
	if c == nil {
		t.Fatal("no session cookie")
	}
	raw := rec.Header().Get("Set-Cookie")
	for _, attr := range []string{"Path=/", "HttpOnly", "Secure", "SameSite=Lax", "Max-Age=", "Expires="} {
		if !strings.Contains(raw, attr) {
			t.Errorf("Set-Cookie %q lacks %s", raw, attr)
		}
	}
	if strings.Contains(strings.ToLower(raw), "domain=") {
		t.Errorf("Set-Cookie %q has a Domain", raw)
	}
	if want := int(auth.SessionLifetime.Seconds()); c.MaxAge < want-60 || c.MaxAge > want {
		t.Errorf("Max-Age %d, want about %d", c.MaxAge, want)
	}
	if len(c.Value) != 43 {
		t.Errorf("token %q: want 43 base64url characters (256 bits)", c.Value)
	}

	// Only the token's hash is stored.
	s, err := en.app.FindFirstRecordByData(store.Sessions, "member", m.Id)
	if err != nil {
		t.Fatal(err)
	}
	if s.GetString("token_hash") != auth.HashToken(c.Value) || strings.Contains(s.GetString("token_hash"), c.Value) {
		t.Error("the session row does not hold the token's hash")
	}
	if exp := s.GetDateTime("expires_at").Time(); exp.Before(start.Add(auth.SessionLifetime-time.Minute)) ||
		exp.After(time.Now().Add(auth.SessionLifetime)) {
		t.Errorf("expires_at %v", exp)
	}

	// The cookie carries no internal ID (AD4): neither the member's nor the session's, nor a collection's.
	ids := []string{m.Id, s.Id, s.Collection().Id, m.Collection().Id}
	if leaks := idscan.Find(rec.Header(), rec.Body.Bytes(), ids); len(leaks) > 0 {
		t.Errorf("sign-in response: %v", leaks)
	}

	if got := en.status(http.MethodGet, auth.MePath, c.Value); got != http.StatusOK {
		t.Errorf("/me with the new cookie: %d", got)
	}

	// Signing in again on a request that has a session ends that session.
	rec = en.req(http.MethodPost, signInPath+"?login=test_member", c.Value, csrf, "1")
	if rec.Code != http.StatusNoContent || responseCookie(rec) == nil {
		t.Fatalf("second sign-in: %d", rec.Code)
	}
	if got := en.status(http.MethodGet, auth.MePath, c.Value); got != http.StatusUnauthorized {
		t.Errorf("the replaced session still works: %d", got)
	}
	if n := en.sessionCount(m); n != 1 {
		t.Errorf("%d sessions after signing in twice, want 1", n)
	}
}

func TestOnlyCurrentMembersCanSignIn(t *testing.T) {
	en := newEnv(t)
	left := en.member("test_left", auth.RoleMember)
	left.Set("left_at", types.NowDateTime())
	if err := en.app.Save(left); err != nil {
		t.Fatal(err)
	}
	deleted := en.member("test_deleted", auth.RoleMember)
	if err := store.SoftDelete(en.app, deleted); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*core.Record{left, deleted} {
		if _, _, err := auth.CreateSession(en.app, m); !errors.Is(err, auth.ErrCannotSignIn) {
			t.Errorf("%s: %v, want ErrCannotSignIn", m.GetString("login_name"), err)
		}
		if got := en.status(http.MethodPost, signInPath+"?login="+m.GetString("login_name"), "", csrf, "1"); got != http.StatusForbidden {
			t.Errorf("sign-in of %s: %d", m.GetString("login_name"), got)
		}
	}
}

func TestLogout(t *testing.T) {
	en := newEnv(t)
	m := en.member("test_member", auth.RoleMember)
	phone, laptop := en.session(m), en.session(m)

	rec := en.req(http.MethodPost, auth.LogoutPath, phone, csrf, "1")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", rec.Code, rec.Body)
	}
	if c := responseCookie(rec); c == nil || c.Value != "" || c.MaxAge >= 0 {
		t.Errorf("logout cookie %v, want one that clears the session cookie", c)
	}
	if got := en.status(http.MethodGet, auth.MePath, phone); got != http.StatusUnauthorized {
		t.Errorf("logged-out session: %d", got)
	}
	if got := en.status(http.MethodGet, auth.MePath, laptop); got != http.StatusOK {
		t.Errorf("the member's other session: %d", got)
	}
	if n := en.sessionCount(m); n != 1 {
		t.Errorf("%d sessions after logout, want 1", n)
	}
}

func TestExpiry(t *testing.T) {
	en := newEnv(t)
	m := en.member("test_member", auth.RoleMember)
	token := en.session(m)
	signedIn := time.Now()

	auth.SetNow(t, func() time.Time { return signedIn.Add(auth.SessionLifetime - time.Minute) })
	if got := en.status(http.MethodGet, auth.MePath, token); got != http.StatusOK {
		t.Errorf("a minute before expiry: %d", got)
	}
	auth.SetNow(t, func() time.Time { return signedIn.Add(auth.SessionLifetime + time.Minute) })
	rec := en.req(http.MethodGet, auth.MePath, token)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("after expiry: %d", rec.Code)
	}
	if c := responseCookie(rec); c == nil || c.MaxAge >= 0 {
		t.Errorf("an expired session's cookie is not cleared: %v", c)
	}
	if n := en.sessionCount(m); n != 0 {
		t.Errorf("the expired session was not deleted (%d left)", n)
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	en := newEnv(t)
	m := en.member("test_member", auth.RoleMember)
	en.session(m)
	auth.SetNow(t, func() time.Time { return time.Now().Add(-auth.SessionLifetime - time.Hour) })
	en.session(m) // already expired by now
	auth.SetNow(t, time.Now)

	n, err := auth.DeleteExpiredSessions(en.app)
	if err != nil || n != 1 {
		t.Errorf("deleted %d, %v; want 1", n, err)
	}
	if left := en.sessionCount(m); left != 1 {
		t.Errorf("%d sessions left, want 1", left)
	}
}

func TestLastSeenIsADay(t *testing.T) {
	en := newEnv(t)
	m := en.member("test_member", auth.RoleMember)
	token := en.session(m)
	later := time.Date(2030, 1, 2, 15, 4, 5, 0, time.UTC)
	auth.SetNow(t, func() time.Time { return later })
	// CreateSession recorded today; a request on a later day records that day.
	if err := en.app.RunInTransaction(func(tx core.App) error {
		_, err := tx.DB().Update(store.Sessions, dbx.Params{"expires_at": "2031-01-01 00:00:00.000Z"}, nil).Execute()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := en.status(http.MethodGet, auth.MePath, token); got != http.StatusOK {
		t.Fatalf("/me: %d", got)
	}
	s, err := en.app.FindFirstRecordByData(store.Sessions, "member", m.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.GetDateTime("last_seen_at").Time(); !got.Equal(time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("last_seen_at %v, want the start of the day", got)
	}
}

func TestMalformedCookie(t *testing.T) {
	en := newEnv(t)
	for _, token := range []string{"x", "not a token", strings.Repeat("A", 44), "eyJhbGciOiJIUzI1NiJ9.e30.x"} {
		rec := en.req(http.MethodGet, auth.MePath, token)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%q: %d", token, rec.Code)
		}
		if c := responseCookie(rec); c == nil || c.MaxAge >= 0 {
			t.Errorf("%q: cookie not cleared", token)
		}
	}
}

// TestLeavingEndsSessions covers leaving or removal (left_at) and soft deletion: the member's sessions are deleted at
// once, an audit entry is written, and the session check refuses the member even when the change bypassed the hooks.
func TestLeavingEndsSessions(t *testing.T) {
	en := newEnv(t)
	other := en.member("test_other", auth.RoleMember)
	otherToken := en.session(other)

	for _, tc := range []struct {
		name, reason string
		change       func(*core.Record) error
	}{
		{"left", auth.RevokeLeft, func(m *core.Record) error {
			m.Set("left_at", types.NowDateTime())
			return en.app.Save(m)
		}},
		{"deleted", auth.RevokeDeleted, func(m *core.Record) error { return store.SoftDelete(en.app, m) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := en.member("test_"+tc.name, auth.RoleMember)
			a, b := en.session(m), en.session(m)
			if err := tc.change(m); err != nil {
				t.Fatal(err)
			}
			if n := en.sessionCount(m); n != 0 {
				t.Errorf("%d sessions left", n)
			}
			for _, token := range []string{a, b} {
				if got := en.status(http.MethodGet, auth.MePath, token); got != http.StatusUnauthorized {
					t.Errorf("old session: %d", got)
				}
			}
			entry, err := en.app.FindFirstRecordByFilter(store.AuditLog, "action = {:a} && target_uuid = {:u}",
				dbx.Params{"a": auth.AuditRevokeSessions, "u": m.GetString(store.FieldUUID)})
			if err != nil {
				t.Fatalf("audit entry: %v", err)
			}
			var details map[string]any
			if err := entry.UnmarshalJSONField("details", &details); err != nil {
				t.Fatal(err)
			}
			if details["reason"] != tc.reason || details["sessions"] != float64(2) || entry.GetString("actor") != "" {
				t.Errorf("audit entry: actor %q, details %v", entry.GetString("actor"), details)
			}
		})
	}

	// The same changes made in SQL, past the hooks: the sessions remain, but are refused.
	for _, column := range []string{"left_at", store.FieldDeletedAt} {
		m := en.member("test_sql_"+strings.ReplaceAll(column, "_", ""), auth.RoleMember)
		token := en.session(m)
		if _, err := en.app.DB().Update(store.Members, dbx.Params{column: types.NowDateTime().String()},
			dbx.HashExp{"id": m.Id}).Execute(); err != nil {
			t.Fatal(err)
		}
		if got := en.status(http.MethodGet, auth.MePath, token); got != http.StatusUnauthorized {
			t.Errorf("%s set in SQL: %d", column, got)
		}
	}
	// A member whose row is gone altogether.
	gone := en.member("test_gone", auth.RoleMember)
	goneToken := en.session(gone)
	if _, err := en.app.DB().Delete(store.Members, dbx.HashExp{"id": gone.Id}).Execute(); err != nil {
		t.Fatal(err)
	}
	if got := en.status(http.MethodGet, auth.MePath, goneToken); got != http.StatusUnauthorized {
		t.Errorf("member row removed: %d", got)
	}

	if got := en.status(http.MethodGet, auth.MePath, otherToken); got != http.StatusOK {
		t.Errorf("another member's session: %d", got)
	}
}

// TestRevokeSessions is the credential reset's part (D5): every session of the member ends, others are unaffected.
func TestRevokeSessions(t *testing.T) {
	en := newEnv(t)
	maintainer := en.member("test_maintainer", auth.RoleMaintainer)
	m := en.member("test_member", auth.RoleMember)
	other := en.member("test_other", auth.RoleMember)
	a, b := en.session(m), en.session(m)
	otherToken, maintainerToken := en.session(other), en.session(maintainer)

	n, err := auth.RevokeSessions(en.app, m, maintainer, auth.RevokeCredentialsReset)
	if err != nil || n != 2 {
		t.Fatalf("revoked %d, %v; want 2", n, err)
	}
	for _, token := range []string{a, b} {
		if got := en.status(http.MethodGet, auth.MePath, token); got != http.StatusUnauthorized {
			t.Errorf("revoked session: %d", got)
		}
	}
	for _, token := range []string{otherToken, maintainerToken} {
		if got := en.status(http.MethodGet, auth.MePath, token); got != http.StatusOK {
			t.Errorf("unrelated session: %d", got)
		}
	}
	entry, err := en.app.FindFirstRecordByData(store.AuditLog, "action", auth.AuditRevokeSessions)
	if err != nil {
		t.Fatal(err)
	}
	if entry.GetString("actor") != maintainer.Id || entry.GetString("target_type") != store.Members ||
		entry.GetString("target_uuid") != m.GetString(store.FieldUUID) {
		t.Errorf("audit entry %v", entry.PublicExport())
	}
	// A new sign-in after the reset works.
	if got := en.status(http.MethodGet, auth.MePath, en.session(m)); got != http.StatusOK {
		t.Errorf("new session after the reset: %d", got)
	}
}

// TestRateLimitSeesMember checks that the session sets e.Auth before PocketBase's rate limiter runs: a rule for
// signed-in callers only (audience @auth) applies to a member's requests and not to anonymous ones.
func TestRateLimitSeesMember(t *testing.T) {
	en := newEnv(t, func(app core.App) {
		s := app.Settings()
		s.RateLimits.Enabled = true
		s.RateLimits.Rules = append([]core.RateLimitRule{{
			Label: boundary.OSFMPrefix, Audience: core.RateLimitRuleAudienceAuth, MaxRequests: 2, Duration: 60,
		}}, s.RateLimits.Rules...)
		if err := app.Save(s); err != nil {
			t.Fatal(err)
		}
	})
	m := en.member("test_member", auth.RoleMember)
	token := en.session(m)
	var got []int
	for range 3 {
		got = append(got, en.status(http.MethodGet, echoPath, token))
	}
	if got[0] != http.StatusOK || got[1] != http.StatusOK || got[2] != http.StatusTooManyRequests {
		t.Errorf("member: %v, want 200 200 429", got)
	}
	for range 3 {
		if code := en.status(http.MethodGet, publicPath, ""); code != http.StatusOK {
			t.Errorf("anonymous: %d, want 200 (the @auth rule does not apply)", code)
		}
	}
}

// TestAuthIsNotASession checks that the session check relies on the session, not on e.Auth, which PocketBase's own
// token loader can set to a superuser on the admin listener.
func TestAuthIsNotASession(t *testing.T) {
	en := newEnv(t, func(app core.App) {
		app.OnServe().BindFunc(func(se *core.ServeEvent) error {
			// Where PocketBase loads tokens: after the session, before the session check.
			se.Router.Bind(&hook.Handler[*core.RequestEvent]{
				Priority: apis.DefaultLoadAuthTokenMiddlewarePriority,
				Func: func(e *core.RequestEvent) error {
					if e.Request.Header.Get("X-Test-Superuser") != "" {
						e.Auth = core.NewRecord(core.NewAuthCollection(core.CollectionNameSuperusers))
					}
					return e.Next()
				},
			})
			return se.Next()
		})
	})
	if got := en.status(http.MethodGet, auth.MePath, "", "X-Test-Superuser", "1"); got != http.StatusUnauthorized {
		t.Errorf("e.Auth without a session: %d, want 401", got)
	}
}
