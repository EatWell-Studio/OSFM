// Package testapp is test support for integration tests of the whole instance (docs/architecture.md §6): it boots an
// app on a new data directory the way `osfm serve` does, serves requests in-process, and sends them as the four
// identities the permission tests use — anonymous, member, author and maintainer — with the X-OSFM header and the
// session cookie a browser would send. Seed helpers create invites and members who registered through the real
// routes, with password and TOTP or with a passkey (internal/auth/passkeytest), and sign them in again.
//
// The caller passes the function that registers OSFM's hooks and routes: `bind` of cmd/osfm, so that what a test
// checks is what the binary serves. Only tests import this package.
package testapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// BindFunc registers OSFM's hooks and routes on an app, as cmd/osfm's bind does.
type BindFunc func(app core.App, cfg *config.Config)

// Options configure [Start].
type Options struct {
	// Bind registers the hooks and routes (required).
	Bind BindFunc
	// Env holds configuration variables (OSFM_*) besides the data directory, which is always a new one.
	Env map[string]string
	// Unconfigured starts the instance as a first start does: no maintainer, no cities, a setup code (see
	// [App.SetupCode]). Without it, the instance has the made-up maintainer, member, author and city described at
	// [App].
	Unconfigured bool
}

// Identity is a kind of caller of the permission tests.
type Identity string

// The four identities (docs/architecture.md §6).
const (
	Anonymous Identity = "anonymous"
	// Member is a signed-in member who wrote nothing.
	Member Identity = "member"
	// Author is a signed-in member who wrote [App.Recommendation].
	Author Identity = "author"
	// Maintainer is a signed-in maintainer.
	Maintainer Identity = "maintainer"
)

// Identities lists the four identities, for table tests.
var Identities = []Identity{Anonymous, Member, Author, Maintainer}

// Made-up data of a configured instance.
const (
	InstanceName = "Test Circle"
	CitySlug     = "testtown"
	CityName     = "Test Town"
)

// App is a booted instance. Unless it was started unconfigured, it has a maintainer, a member, an author with a
// recommendation, an instance name and one city (without a basemap archive).
type App struct {
	core.App
	Config *config.Config
	// Handler serves requests as the main listener does.
	Handler http.Handler

	Maintainer, Member, Author *core.Record
	// Recommendation is a recommendation by Author, of a made-up place.
	Recommendation *core.Record

	t         testing.TB
	mu        sync.Mutex
	clients   int
	responses []*Response
}

// Start boots an app on a new data directory through opts.Bind, as `osfm serve` does without listening: the
// bootstrap with the migrations, the seed (unless opts.Unconfigured), then PocketBase's router and the OnServe hooks,
// where OSFM registers its routes. The app is torn down when the test ends.
func Start(t testing.TB, opts Options) *App {
	t.Helper()
	if opts.Bind == nil {
		t.Fatal("testapp: Options.Bind is required")
	}
	dir := t.TempDir()
	cfg, err := config.Parse(func(name string) (string, bool) {
		if name == config.EnvDataDir {
			return dir, true
		}
		v, ok := opts.Env[name]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: cfg.PBDataDir()})
	opts.Bind(app, cfg)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })

	a := &App{App: app, Config: cfg, t: t}
	if !opts.Unconfigured {
		a.seed()
	}

	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: r}, func(*core.ServeEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if a.Handler, err = r.BuildMux(); err != nil {
		t.Fatal(err)
	}
	return a
}

func (a *App) seed() {
	t := a.t
	t.Helper()
	save := func(collection string, values map[string]any) *core.Record {
		t.Helper()
		var r *core.Record
		if collection == store.Members {
			var err error
			if r, err = auth.NewMemberRecord(a); err != nil {
				t.Fatal(err)
			}
		} else {
			c, err := a.FindCachedCollectionByNameOrId(collection)
			if err != nil {
				t.Fatal(err)
			}
			r = core.NewRecord(c)
		}
		for k, v := range values {
			r.Set(k, v)
		}
		if err := a.Save(r); err != nil {
			t.Fatalf("testapp: seed %s: %v", collection, err)
		}
		return r
	}
	joined := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a.Maintainer = save(store.Members, map[string]any{"login_name": "test_maintainer",
		"display_name": "Test Maintainer", "role": auth.RoleMaintainer, "can_invite": true, "joined_at": joined})
	a.Member = save(store.Members, map[string]any{"login_name": "test_member", "display_name": "Test Member",
		"role": auth.RoleMember, "invited_by": a.Maintainer.Id, "joined_at": joined})
	a.Author = save(store.Members, map[string]any{"login_name": "test_author", "display_name": "Test Author",
		"pseudonym": "Test Pen Name", "role": auth.RoleMember, "invited_by": a.Maintainer.Id, "joined_at": joined})
	place := save(store.Places, map[string]any{"name": "Test Dumpling House", "lat": 0.34, "lon": 7.06,
		"precision": "exact", "category": "restaurant", "status": "open"})
	a.Recommendation = save(store.Recommendations, map[string]any{"place": place.Id, "author": a.Author.Id,
		"display_as": "pseudonym", "attitude": "good", "visibility": "circle", "text": "Test recommendation text"})

	settings, err := a.FindRecordById(store.InstanceSettings, store.SettingsID)
	if err != nil {
		t.Fatal(err)
	}
	settings.Set("name", InstanceName)
	settings.Set("cities", []map[string]any{{"slug": CitySlug, "name": CityName,
		"bbox": []float64{7.02, 0.31, 7.1, 0.37}, "center": []float64{7.06, 0.34}}})
	if err := a.Save(settings); err != nil {
		t.Fatal(err)
	}
}

// SetupCode returns the setup code of an unconfigured instance, from the file the server wrote (as `osfm setup-code`
// does).
func (a *App) SetupCode() string {
	a.t.Helper()
	code, err := auth.ReadSetupCode(a, filepath.Join(a.Config.SecretsDir(), auth.SetupCodeFileName))
	if err != nil {
		a.t.Fatal(err)
	}
	return code
}

// Client returns a new anonymous client: a browser without a session, with a client address of its own (so that
// clients do not share the authentication rate limits).
func (a *App) Client() *Client {
	a.mu.Lock()
	a.clients++
	n := a.clients
	a.mu.Unlock()
	// IPv6 documentation addresses, each in a /64 of its own: the rate limits count an IPv6 client by its /64.
	return &Client{app: a, addr: fmt.Sprintf("[2001:db8:%x::1]:40000", n)}
}

// SignedIn returns a new client with a new session of member, as after a sign-in.
func (a *App) SignedIn(member *core.Record) *Client {
	a.t.Helper()
	token, _, err := auth.CreateSession(a, member)
	if err != nil {
		a.t.Fatal(err)
	}
	c := a.Client()
	c.session = token
	return c
}

// As returns a new client of the identity: anonymous, or signed in with a new session of the seeded member, author
// or maintainer.
func (a *App) As(id Identity) *Client {
	a.t.Helper()
	var member *core.Record
	switch id {
	case Anonymous:
		return a.Client()
	case Member:
		member = a.Member
	case Author:
		member = a.Author
	case Maintainer:
		member = a.Maintainer
	default:
		a.t.Fatalf("testapp: unknown identity %q", id)
	}
	if member == nil {
		a.t.Fatalf("testapp: the instance has no %s (started unconfigured)", id)
	}
	return a.SignedIn(member)
}

// InternalIDs returns the PocketBase ID of every OSFM collection and of every record in them (and of the superusers)
// the app has now.
func (a *App) InternalIDs() []string {
	a.t.Helper()
	collections, err := a.FindAllCollections()
	if err != nil {
		a.t.Fatal(err)
	}
	var ids []string
	for _, c := range collections {
		switch {
		case !c.System:
			ids = append(ids, c.Id)
		case c.Name != core.CollectionNameSuperusers:
			continue
		}
		records, err := a.FindAllRecords(c)
		if err != nil {
			a.t.Fatal(err)
		}
		for _, r := range records {
			ids = append(ids, r.Id)
		}
	}
	return ids
}

// CheckNoInternalIDs fails the test for every response sent so far (by any client of the app) that contains an
// internal ID of a record that exists now (AD4), as the internal ID scan of cmd/osfm does.
func (a *App) CheckNoInternalIDs() {
	a.t.Helper()
	ids := a.InternalIDs()
	a.mu.Lock()
	responses := append([]*Response(nil), a.responses...)
	a.mu.Unlock()
	for _, r := range responses {
		for _, leak := range idscan.Find(r.Header(), r.Body.Bytes(), ids) {
			a.t.Errorf("%s %s: internal ID %s", r.Method, r.Path, leak)
		}
	}
}

// Client sends requests as one browser: from one client address, with the session cookie it was given or last set
// by a response (a sign-in sets it, a sign-out clears it).
type Client struct {
	app     *App
	addr    string
	session string // the session token, "" without a session
}

// Session returns the client's session token, "" without one.
func (c *Client) Session() string { return c.session }

// WithSession returns a client at the same address with the session token (none when empty).
func (c *Client) WithSession(token string) *Client {
	return &Client{app: c.app, addr: c.addr, session: token}
}

// Get sends a GET request.
func (c *Client) Get(path string) *Response {
	c.app.t.Helper()
	return c.Send(httptest.NewRequest(http.MethodGet, path, nil))
}

// Post sends a POST request with body as JSON (none when nil) and the X-OSFM header, as the web app does.
func (c *Client) Post(path string, body any) *Response {
	c.app.t.Helper()
	var reader io.Reader = http.NoBody
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.app.t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	r := httptest.NewRequest(http.MethodPost, path, reader)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(auth.CSRFHeader, "1")
	return c.Send(r)
}

// Send sends r as it is, from the client's address and with its session cookie, and keeps the session cookie the
// response sets or clears.
func (c *Client) Send(r *http.Request) *Response {
	c.app.t.Helper()
	r.RemoteAddr = c.addr
	name := auth.SessionCookieName(c.app)
	if c.session != "" {
		r.Header.Add("Cookie", name+"="+c.session)
	}
	rec := httptest.NewRecorder()
	c.app.Handler.ServeHTTP(rec, r)
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == name {
			if cookie.Value == "" || cookie.MaxAge < 0 {
				c.session = ""
			} else {
				c.session = cookie.Value
			}
		}
	}
	resp := &Response{ResponseRecorder: rec, Method: r.Method, Path: r.URL.RequestURI(), t: c.app.t}
	c.app.mu.Lock()
	c.app.responses = append(c.app.responses, resp)
	c.app.mu.Unlock()
	return resp
}

// Response is a recorded response.
type Response struct {
	*httptest.ResponseRecorder
	Method, Path string
	t            testing.TB
}

// Expect fails the test unless the response has the status; it returns the response.
func (r *Response) Expect(status int) *Response {
	r.t.Helper()
	if r.Code != status {
		r.t.Fatalf("%s %s: status %d, want %d: %s", r.Method, r.Path, r.Code, status,
			strings.TrimSpace(r.Body.String()))
	}
	return r
}

// Decode decodes the JSON body of r into a T, failing the test if it is not one.
func Decode[T any](r *Response) T {
	r.t.Helper()
	var v T
	if err := json.Unmarshal(r.Body.Bytes(), &v); err != nil {
		r.t.Fatalf("%s %s: decode %T: %v: %s", r.Method, r.Path, v, err, r.Body.String())
	}
	return v
}
