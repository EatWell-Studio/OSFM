package geocode

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// A made-up city and places; the fake upstreams below stand for Photon and Nominatim. No test talks to the real
// services.
var city = BBox{7.02, 0.31, 7.1, 0.37}

const searchTerm = "test-secret-term"

// upstream is a fake search service that records the requests it gets.
type upstream struct {
	*httptest.Server
	mu       sync.Mutex
	requests []*http.Request
	times    []time.Time
}

func newUpstream(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	u := &upstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.requests = append(u.requests, r.Clone(context.Background()))
		u.times = append(u.times, time.Now())
		u.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.requests)
}

func (u *upstream) last() *http.Request {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requests[len(u.requests)-1]
}

func failing(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }
}

func respondJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

// photonAnswer has one place inside the city with a name, one address without a name, and one place outside.
const photonAnswer = `{"type":"FeatureCollection","features":[
 {"type":"Feature","geometry":{"type":"Point","coordinates":[7.05,0.34]},
  "properties":{"name":"Test Harbour Market","street":"Test Quay","housenumber":"3","district":"Test District","city":"Test Town","osm_key":"amenity"}},
 {"type":"Feature","geometry":{"type":"Point","coordinates":[7.06,0.35]},
  "properties":{"street":"Test Lane","housenumber":"12","city":"Test Town"}},
 {"type":"Feature","geometry":{"type":"Point","coordinates":[8.5,0.34]},
  "properties":{"name":"Test Far Away Market","city":"Elsewhere"}}
]}`

const nominatimAnswer = `[
 {"lat":"0.3412","lon":"7.0511","name":"Test Fallback Hall","display_name":"Test Fallback Hall, Test Road, Test Town",
  "address":{"road":"Test Road","house_number":"5","suburb":"Test Suburb","city":"Test Town"}},
 {"lat":"1.5","lon":"7.05","name":"Test Outside Hall","display_name":"Test Outside Hall"}
]`

func testGeocoder(t *testing.T, photon, nominatim *upstream, opts Options) (*Geocoder, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	if photon != nil {
		opts.PhotonURL = photon.URL + "/api"
	}
	if nominatim != nil {
		opts.NominatimURL = nominatim.URL + "/search"
	}
	opts.Version = "1.2.3-test"
	opts.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if opts.NominatimInterval == 0 {
		opts.NominatimInterval = 10 * time.Millisecond
	}
	return New(opts), &logs
}

func checkNoTerm(t *testing.T, logs *bytes.Buffer) {
	t.Helper()
	if strings.Contains(logs.String(), searchTerm) || strings.Contains(logs.String(), "test-secret") {
		t.Errorf("the log contains the search term:\n%s", logs.String())
	}
}

func TestPhoton(t *testing.T) {
	photon := newUpstream(t, respondJSON(photonAnswer))
	nominatim := newUpstream(t, failing(http.StatusInternalServerError))
	g, logs := testGeocoder(t, photon, nominatim, Options{})
	got, err := g.Search(context.Background(), Query{Text: searchTerm, BBox: city, Contact: "test@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{
		{Name: "Test Harbour Market", Label: "Test Quay 3, Test District, Test Town", Lat: 0.34, Lon: 7.05},
		{Name: "Test Lane 12", Label: "Test Town", Lat: 0.35, Lon: 7.06},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("results %+v, want %+v", got, want)
	}
	if nominatim.count() != 0 {
		t.Error("Nominatim asked although Photon answered")
	}
	r := photon.last()
	if r.URL.Path != "/api" || r.URL.Query().Get("q") != searchTerm || r.URL.Query().Get("bbox") != "7.02,0.31,7.1,0.37" ||
		r.URL.Query().Get("limit") == "" {
		t.Errorf("Photon request %s", r.URL)
	}
	checkUpstreamHeaders(t, r, "OSFM/1.2.3-test (+"+ProjectURL+"; test@example.test)")
	checkNoTerm(t, logs)
}

// checkUpstreamHeaders: only User-Agent and Accept (and what the transport adds itself) reach the upstream.
func checkUpstreamHeaders(t *testing.T, r *http.Request, ua string) {
	t.Helper()
	if got := r.Header.Get("User-Agent"); got != ua {
		t.Errorf("User-Agent %q, want %q", got, ua)
	}
	for name := range r.Header {
		switch name {
		case "User-Agent", "Accept", "Accept-Encoding":
		default:
			t.Errorf("upstream request carries %s: %q", name, r.Header.Get(name))
		}
	}
}

func TestFallbackToNominatim(t *testing.T) {
	for name, photonHandler := range map[string]http.HandlerFunc{
		"status":      failing(http.StatusServiceUnavailable),
		"invalid":     respondJSON(`<html>`),
		"no features": respondJSON(`{"type":"FeatureCollection"}`),
	} {
		t.Run(name, func(t *testing.T) {
			photon := newUpstream(t, photonHandler)
			nominatim := newUpstream(t, respondJSON(nominatimAnswer))
			g, logs := testGeocoder(t, photon, nominatim, Options{})
			got, err := g.Search(context.Background(), Query{Text: searchTerm, BBox: city})
			if err != nil {
				t.Fatal(err)
			}
			want := Result{Name: "Test Fallback Hall", Label: "Test Road 5, Test Suburb, Test Town", Lat: 0.3412, Lon: 7.0511}
			if len(got) != 1 || got[0] != want {
				t.Errorf("results %+v, want [%+v]", got, want)
			}
			q := nominatim.last().URL.Query()
			if q.Get("q") != searchTerm || q.Get("viewbox") != "7.02,0.31,7.1,0.37" || q.Get("bounded") != "1" ||
				q.Get("format") != "jsonv2" {
				t.Errorf("Nominatim request %s", nominatim.last().URL)
			}
			checkUpstreamHeaders(t, nominatim.last(), "OSFM/1.2.3-test (+"+ProjectURL+")")
			if !strings.Contains(logs.String(), "service=photon") {
				t.Errorf("Photon's failure not logged:\n%s", logs.String())
			}
			checkNoTerm(t, logs)
		})
	}
}

func TestPhotonTimeout(t *testing.T) {
	release := make(chan struct{})
	photon := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	nominatim := newUpstream(t, respondJSON(nominatimAnswer))
	g, logs := testGeocoder(t, photon, nominatim, Options{Timeout: 100 * time.Millisecond})
	got, err := g.Search(context.Background(), Query{Text: searchTerm, BBox: city})
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v, %v", got, err)
	}
	if !strings.Contains(logs.String(), "reason=timeout") {
		t.Errorf("timeout not logged:\n%s", logs.String())
	}
	checkNoTerm(t, logs)
}

func TestBothFail(t *testing.T) {
	photon := newUpstream(t, failing(http.StatusBadGateway))
	nominatim := newUpstream(t, failing(http.StatusTooManyRequests))
	g, logs := testGeocoder(t, photon, nominatim, Options{})
	if _, err := g.Search(context.Background(), Query{Text: searchTerm, BBox: city}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error %v, want ErrUnavailable", err)
	}
	// A failure is not cached: the next search asks again.
	if _, err := g.Search(context.Background(), Query{Text: searchTerm, BBox: city}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error %v, want ErrUnavailable", err)
	}
	if photon.count() != 2 || nominatim.count() != 2 {
		t.Errorf("requests: Photon %d, Nominatim %d", photon.count(), nominatim.count())
	}
	for _, want := range []string{"service=photon", "status 502", "service=nominatim", "status 429"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, logs.String())
		}
	}
	checkNoTerm(t, logs)
}

// TestUnreachable: a connection error names the request URL (with the term) in its message; the log does not.
func TestUnreachable(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	var logs bytes.Buffer
	g := New(Options{PhotonURL: gone.URL + "/api", NominatimURL: gone.URL + "/search", NominatimInterval: time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	if _, err := g.Search(context.Background(), Query{Text: searchTerm, BBox: city}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error %v", err)
	}
	if strings.Count(logs.String(), "request failed") != 2 {
		t.Errorf("log:\n%s", logs.String())
	}
	checkNoTerm(t, &logs)
}

func TestNotConfigured(t *testing.T) {
	g := New(Options{})
	if _, err := g.Search(context.Background(), Query{Text: searchTerm, BBox: city}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error %v", err)
	}
}

func TestOutsideCityFiltered(t *testing.T) {
	// Every result lies outside the city: Photon's bbox is trusted no more than Nominatim's bounded.
	photon := newUpstream(t, respondJSON(`{"features":[
	 {"geometry":{"type":"Point","coordinates":[7.2,0.34]},"properties":{"name":"Test East"}},
	 {"geometry":{"type":"Point","coordinates":[7.05,0.30]},"properties":{"name":"Test South"}},
	 {"geometry":{"type":"Point","coordinates":[7.05,0.34]},"properties":{"name":""}}]}`))
	g, _ := testGeocoder(t, photon, nil, Options{})
	got, err := g.Search(context.Background(), Query{Text: searchTerm, BBox: city})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("results %+v, want none (and not nil)", got)
	}
}

func TestMaxResults(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"features":[`)
	for i := range 14 {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"geometry":{"type":"Point","coordinates":[7.05,0.3` + strconv.Itoa(1+i%6) + `]},"properties":{"name":"Test Place ` + strconv.Itoa(i) + `"}}`)
	}
	b.WriteString(`]}`)
	photon := newUpstream(t, respondJSON(b.String()))
	g, _ := testGeocoder(t, photon, nil, Options{})
	got, err := g.Search(context.Background(), Query{Text: searchTerm, BBox: city})
	if err != nil || len(got) != MaxResults {
		t.Errorf("%d results, %v", len(got), err)
	}
}

func TestCache(t *testing.T) {
	photon := newUpstream(t, respondJSON(photonAnswer))
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	g, _ := testGeocoder(t, photon, nil, Options{Now: clock})
	search := func(text string, b BBox) []Result {
		t.Helper()
		got, err := g.Search(context.Background(), Query{Text: text, BBox: b})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	first := search("Test Market", city)
	first[0].Name = "changed by the caller"
	again := search("  test   MARKET ", city)
	if photon.count() != 1 {
		t.Errorf("cache hit asked the upstream: %d requests", photon.count())
	}
	if again[0].Name != "Test Harbour Market" {
		t.Errorf("cached result changed: %+v", again[0])
	}
	// Another city is another search.
	search("Test Market", BBox{7.0, 0.3, 7.2, 0.4})
	if photon.count() != 2 {
		t.Errorf("requests %d, want 2", photon.count())
	}
	// After 24 hours the answer is gone.
	mu.Lock()
	now = now.Add(DefaultCacheTTL)
	mu.Unlock()
	search("Test Market", city)
	if photon.count() != 3 {
		t.Errorf("requests %d after the TTL, want 3", photon.count())
	}
}

func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := newCache(2, time.Hour, time.Now)
	c.put("a", []Result{{Name: "Test A"}})
	c.put("b", []Result{{Name: "Test B"}})
	if _, ok := c.get("a"); !ok {
		t.Fatal("a missing")
	}
	c.put("c", []Result{{Name: "Test C"}})
	if _, ok := c.get("b"); ok {
		t.Error("b kept, want it evicted")
	}
	if _, ok := c.get("a"); !ok {
		t.Error("a evicted")
	}
	if c.len() != 2 {
		t.Errorf("len %d", c.len())
	}
}

// TestNominatimSpacing: the requests of the whole instance to Nominatim are at least the interval apart, and a search
// whose turn would come too late gives up without asking.
func TestNominatimSpacing(t *testing.T) {
	photon := newUpstream(t, failing(http.StatusInternalServerError))
	nominatim := newUpstream(t, respondJSON(nominatimAnswer))
	const interval = 150 * time.Millisecond
	g, _ := testGeocoder(t, photon, nominatim, Options{NominatimInterval: interval, NominatimMaxWait: time.Second})
	var wg sync.WaitGroup
	for i := range 3 {
		wg.Go(func() {
			if _, err := g.Search(context.Background(), Query{Text: searchTerm + strconv.Itoa(i), BBox: city}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	nominatim.mu.Lock()
	times := append([]time.Time(nil), nominatim.times...)
	nominatim.mu.Unlock()
	if len(times) != 3 {
		t.Fatalf("%d Nominatim requests", len(times))
	}
	for i := 1; i < len(times); i++ {
		// A little slack for the scheduling between the turn and the request's arrival.
		if d := times[i].Sub(times[i-1]); d < interval-20*time.Millisecond {
			t.Errorf("requests %d and %d only %v apart", i-1, i, d)
		}
	}

	busy, logs := testGeocoder(t, photon, nominatim, Options{NominatimInterval: time.Hour, NominatimMaxWait: -1})
	if _, err := busy.Search(context.Background(), Query{Text: searchTerm + "x", BBox: city}); err != nil {
		t.Fatal(err)
	}
	before := nominatim.count()
	if _, err := busy.Search(context.Background(), Query{Text: searchTerm + "y", BBox: city}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error %v, want ErrUnavailable", err)
	}
	if nominatim.count() != before {
		t.Error("Nominatim asked before its turn")
	}
	if !strings.Contains(logs.String(), "no turn in time") {
		t.Errorf("log:\n%s", logs.String())
	}
}

func TestDefaults(t *testing.T) {
	g := New(Options{})
	if g.opts.NominatimInterval < time.Second || g.opts.Timeout != 5*time.Second || g.opts.CacheTTL != 24*time.Hour {
		t.Errorf("%+v", g.opts)
	}
}

func TestCanceledSearch(t *testing.T) {
	photon := newUpstream(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	nominatim := newUpstream(t, respondJSON(nominatimAnswer))
	g, logs := testGeocoder(t, photon, nominatim, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := g.Search(ctx, Query{Text: searchTerm, BBox: city}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error %v", err)
	}
	if nominatim.count() != 0 || logs.Len() != 0 {
		t.Errorf("a search the caller gave up on went on: %d Nominatim requests, log %q", nominatim.count(), logs.String())
	}
}

func TestUserAgent(t *testing.T) {
	for _, tc := range []struct{ version, contact, want string }{
		{"1.0.0", "", "OSFM/1.0.0 (+" + ProjectURL + ")"},
		{"", "", "OSFM/dev (+" + ProjectURL + ")"},
		{"1.0.0", " ops@example.test ", "OSFM/1.0.0 (+" + ProjectURL + "; ops@example.test)"},
		{"1.0.0", "测试 (ops@example.test)\t", "OSFM/1.0.0 (+" + ProjectURL + "; ops@example.test)"},
		{"1.0.0", "（）", "OSFM/1.0.0 (+" + ProjectURL + ")"},
	} {
		if got := UserAgent(tc.version, tc.contact); got != tc.want {
			t.Errorf("UserAgent(%q, %q) = %q, want %q", tc.version, tc.contact, got, tc.want)
		}
	}
}
