package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/boundary"
	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// Place search through fake upstream services (httptest): no test talks to the real Photon or Nominatim.

const secretTerm = "test-secret-term"

// fakeSearch is a fake Photon or Nominatim: it answers with answer (or fails with status) and records the requests.
type fakeSearch struct {
	*httptest.Server
	mu       sync.Mutex
	status   int
	answer   string
	requests []*http.Request
}

func newFakeSearch(t *testing.T, answer string) *fakeSearch {
	t.Helper()
	f := &fakeSearch{status: http.StatusOK, answer: answer}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Clone(r.Context()))
		status, answer := f.status, f.answer
		f.mu.Unlock()
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeSearch) fail(status int) {
	f.mu.Lock()
	f.status = status
	f.mu.Unlock()
}

func (f *fakeSearch) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeSearch) last() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

// Inside testapp's city (bbox 7.02,0.31 – 7.1,0.37), and one place outside it.
const (
	fakePhotonAnswer = `{"type":"FeatureCollection","features":[
	 {"type":"Feature","geometry":{"type":"Point","coordinates":[7.05,0.34]},"properties":{"name":"Test Harbour Market","street":"Test Quay","housenumber":"3","city":"Test Town"}},
	 {"type":"Feature","geometry":{"type":"Point","coordinates":[9.05,0.34]},"properties":{"name":"Test Far Market"}}]}`
	fakeNominatimAnswer = `[{"lat":"0.3412","lon":"7.0511","name":"Test Fallback Hall","address":{"road":"Test Road","city":"Test Town"}},
	 {"lat":"5.1","lon":"7.05","name":"Test Outside Hall"}]`
)

type geocodeSetup struct {
	*testapp.App
	photon, nominatim *fakeSearch
}

// startGeocode boots an app whose place search asks the fakes, with the request log as `osfm serve` writes it.
func startGeocode(t *testing.T) geocodeSetup {
	t.Helper()
	photon := newFakeSearch(t, fakePhotonAnswer)
	nominatim := newFakeSearch(t, fakeNominatimAnswer)
	a := testapp.Start(t, testapp.Options{
		Env: map[string]string{
			config.EnvGeocoderPhotonURL:    photon.URL + "/api",
			config.EnvGeocoderNominatimURL: nominatim.URL + "/search",
		},
		Bind: func(app core.App, cfg *config.Config) {
			store.BindHooks(app)
			boundary.Bind(app, boundary.Options{})
			auth.Bind(app, auth.Options{})
			api.Bind(app, api.Options{Config: cfg})
		},
	})
	return geocodeSetup{App: a, photon: photon, nominatim: nominatim}
}

func searchPath(q, city string) string {
	return api.GeocodePath + "?" + url.Values{"q": {q}, "city": {city}}.Encode()
}

func setGeocoder(t *testing.T, a *testapp.App, enabled bool, contact string) {
	t.Helper()
	a.As(testapp.Maintainer).Post(api.InstancePath, api.InstanceUpdate{GeocoderEnabled: &enabled,
		GeocoderContact: &contact}).Expect(http.StatusOK)
}

func TestGeocodeIdentities(t *testing.T) {
	t.Parallel()
	a := startGeocode(t)
	path := searchPath(secretTerm, testapp.CitySlug)
	a.As(testapp.Anonymous).Get(path).Expect(http.StatusUnauthorized)
	if a.photon.count() != 0 {
		t.Error("an anonymous search reached the upstream")
	}
	for _, id := range []testapp.Identity{testapp.Member, testapp.Author, testapp.Maintainer} {
		r := a.As(id).Get(path).Expect(http.StatusOK)
		got := testapp.Decode[[]api.GeocodeResult](r)
		want := api.GeocodeResult{Name: "Test Harbour Market", Label: "Test Quay 3, Test Town", Lat: 0.34, Lon: 7.05}
		if len(got) != 1 || got[0] != want {
			t.Errorf("%s: results %+v, want [%+v]", id, got, want)
		}
		if r.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control %q", id, r.Header().Get("Cache-Control"))
		}
	}
	// The same search again comes from the cache.
	if a.photon.count() != 1 || a.nominatim.count() != 0 {
		t.Errorf("upstream requests: Photon %d, Nominatim %d; want 1, 0", a.photon.count(), a.nominatim.count())
	}
	a.CheckNoInternalIDs()
}

// TestGeocodeForwardsNothing: the upstream gets the term and the city's box, and of the member nothing (cookies,
// address, language, other headers); the User-Agent names OSFM and the maintainers' contact.
func TestGeocodeForwardsNothing(t *testing.T) {
	t.Parallel()
	a := startGeocode(t)
	setGeocoder(t, a.App, true, "ops@example.test")
	req := httptest.NewRequest(http.MethodGet, searchPath(secretTerm, testapp.CitySlug), nil)
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("Referer", "http://localhost/map")
	req.Header.Set("X-Test-Header", "test-member-value")
	a.As(testapp.Member).Send(req).Expect(http.StatusOK)

	up := a.photon.last()
	if q := up.URL.Query(); q.Get("q") != secretTerm || q.Get("bbox") != "7.02,0.31,7.1,0.37" {
		t.Errorf("upstream query %s", up.URL.RawQuery)
	}
	ua := up.Header.Get("User-Agent")
	if !strings.HasPrefix(ua, "OSFM/") || !strings.HasSuffix(ua, "; ops@example.test)") {
		t.Errorf("User-Agent %q", ua)
	}
	for name := range up.Header {
		switch name {
		case "User-Agent", "Accept", "Accept-Encoding":
		default:
			t.Errorf("upstream request carries %s: %q", name, up.Header.Get(name))
		}
	}
}

func TestGeocodeFallbackAndFailure(t *testing.T) {
	t.Parallel()
	a := startGeocode(t)
	a.photon.fail(http.StatusInternalServerError)
	r := a.As(testapp.Member).Get(searchPath(secretTerm, testapp.CitySlug)).Expect(http.StatusOK)
	got := testapp.Decode[[]api.GeocodeResult](r)
	if len(got) != 1 || got[0].Name != "Test Fallback Hall" {
		t.Errorf("fallback results %+v", got)
	}
	if q := a.nominatim.last().URL.Query(); q.Get("viewbox") != "7.02,0.31,7.1,0.37" || q.Get("bounded") != "1" {
		t.Errorf("Nominatim query %s", a.nominatim.last().URL.RawQuery)
	}

	a.nominatim.fail(http.StatusServiceUnavailable)
	r = a.As(testapp.Member).Get(searchPath(secretTerm+" two", testapp.CitySlug)).
		Expect(http.StatusServiceUnavailable)
	if e := testapp.Decode[api.Error](r); e.Code != api.CodeGeocoderUnavailable {
		t.Errorf("503 body %+v", e)
	}
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", r.Header().Get("Cache-Control"))
	}
}

func TestGeocodeDisabled(t *testing.T) {
	t.Parallel()
	a := startGeocode(t)
	setGeocoder(t, a.App, false, "")
	for _, path := range []string{searchPath(secretTerm, testapp.CitySlug), api.GeocodePath} {
		r := a.As(testapp.Member).Get(path).Expect(http.StatusNotFound)
		if e := testapp.Decode[api.Error](r); e.Code != api.CodeGeocoderDisabled {
			t.Errorf("%s: body %+v", path, e)
		}
	}
	if a.photon.count() != 0 {
		t.Error("a search reached the upstream while search is off")
	}
	// Members learn that search is off from the instance settings.
	var inst api.Instance
	if err := json.Unmarshal(a.As(testapp.Member).Get(api.InstancePath).Expect(http.StatusOK).Body.Bytes(), &inst); err != nil || inst.GeocoderEnabled {
		t.Errorf("instance %+v, %v", inst, err)
	}
}

func TestGeocodeInvalid(t *testing.T) {
	t.Parallel()
	a := startGeocode(t)
	for _, path := range []string{
		api.GeocodePath,
		searchPath("", testapp.CitySlug),
		searchPath("   ", testapp.CitySlug),
		searchPath(strings.Repeat("测", api.MaxGeocodeQuery+1), testapp.CitySlug),
		searchPath("test\x00term", testapp.CitySlug),
		searchPath(secretTerm, ""),
		searchPath(secretTerm, "no-such-city"),
		api.GeocodePath + "?q=%ff&city=" + testapp.CitySlug,
	} {
		r := a.As(testapp.Member).Get(path).Expect(http.StatusBadRequest)
		if e := testapp.Decode[api.Error](r); e.Code != api.CodeInvalidRequest {
			t.Errorf("%s: body %+v", path, e)
		}
	}
	a.As(testapp.Member).Get(searchPath(strings.Repeat("测", api.MaxGeocodeQuery), testapp.CitySlug)).
		Expect(http.StatusOK)
	if a.photon.count() != 1 {
		t.Errorf("%d upstream requests, want 1", a.photon.count())
	}
}

// TestGeocodeBudget: every member has a search budget of their own (10 at once), answered with 429 and Retry-After.
func TestGeocodeBudget(t *testing.T) {
	t.Parallel()
	a := startGeocode(t)
	member := a.As(testapp.Member)
	for i := range 10 {
		member.Get(searchPath(secretTerm+strconv.Itoa(i), testapp.CitySlug)).Expect(http.StatusOK)
	}
	r := a.As(testapp.Member).Get(searchPath(secretTerm, testapp.CitySlug)).Expect(http.StatusTooManyRequests)
	if e := testapp.Decode[api.Error](r); e.Code != api.CodeRateLimited || r.Header().Get("Retry-After") == "" {
		t.Errorf("429: %+v, Retry-After %q", e, r.Header().Get("Retry-After"))
	}
	if a.photon.count() != 10 {
		t.Errorf("%d upstream requests, want 10", a.photon.count())
	}
	// Another member is not affected.
	a.As(testapp.Author).Get(searchPath(secretTerm, testapp.CitySlug)).Expect(http.StatusOK)
}

// TestGeocodeNoTermInLogs: neither the request log nor the application log holds the search term, whether the search
// succeeds, falls back or fails.
func TestGeocodeNoTermInLogs(t *testing.T) {
	t.Parallel()
	a := startGeocode(t)
	a.As(testapp.Member).Get(searchPath(secretTerm, testapp.CitySlug)).Expect(http.StatusOK)
	a.photon.fail(http.StatusBadGateway)
	a.As(testapp.Member).Get(searchPath(secretTerm+"-fallback", testapp.CitySlug)).Expect(http.StatusOK)
	a.nominatim.fail(http.StatusBadGateway)
	a.As(testapp.Member).Get(searchPath(secretTerm+"-down", testapp.CitySlug)).Expect(http.StatusServiceUnavailable)

	// PocketBase writes the log in the background: wait for the three request entries and the upstream failures.
	var logs []*core.Log
	deadline := time.Now().Add(15 * time.Second)
	for {
		logs = nil
		if err := a.LogQuery().All(&logs); err != nil {
			t.Fatal(err)
		}
		requests, failures := 0, 0
		for _, l := range logs {
			if strings.Contains(l.Message, api.GeocodePath) {
				requests++
			}
			if strings.Contains(l.Message, "upstream service failed") {
				failures++
			}
		}
		if requests >= 3 && failures >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("log entries not written: %d requests, %d failures", requests, failures)
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, l := range logs {
		raw, err := json.Marshal(l.Data)
		if err != nil {
			t.Fatal(err)
		}
		entry := l.Message + " " + string(raw)
		if strings.Contains(entry, "test-secret") || strings.Contains(entry, "q=") {
			t.Errorf("log entry holds the search: %s", entry)
		}
	}
}
