package geocode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Defaults of [Options].
const (
	// DefaultTimeout bounds each request to an upstream service.
	DefaultTimeout = 5 * time.Second
	// DefaultNominatimInterval spaces the requests of the whole instance to Nominatim: its usage policy allows one
	// per second at most.
	DefaultNominatimInterval = time.Second
	// DefaultNominatimMaxWait is how long a search waits for its turn at Nominatim before it gives up.
	DefaultNominatimMaxWait = 3 * time.Second
	// DefaultCacheTTL is how long an answer is kept (M2-Q13).
	DefaultCacheTTL = 24 * time.Hour
	// DefaultCacheSize is the number of answers kept at most.
	DefaultCacheSize = 1000
)

// MaxResults is the number of results a search returns at most.
const MaxResults = 10

// upstreamLimit is the number of results asked of an upstream service: a few more than MaxResults, since some may lie
// outside the city or lack a name.
const upstreamLimit = 15

// maxResponseBytes bounds the answer read from an upstream service.
const maxResponseBytes = 1 << 20

// ProjectURL identifies the software in the User-Agent, as the usage policies of the public services ask.
const ProjectURL = "https://github.com/EatWell-Studio/OSFM"

// ErrUnavailable is returned by [Geocoder.Search] when neither upstream service answered.
var ErrUnavailable = errors.New("geocode: the search services are unavailable")

// BBox is a bounding box: minLon, minLat, maxLon, maxLat in WGS-84 degrees.
type BBox [4]float64

// Contains reports whether the position lies in b (edges included).
func (b BBox) Contains(lon, lat float64) bool {
	return lon >= b[0] && lon <= b[2] && lat >= b[1] && lat <= b[3]
}

// String formats b as the upstream services take it: "minLon,minLat,maxLon,maxLat".
func (b BBox) String() string {
	parts := make([]string, 4)
	for i, v := range b {
		parts[i] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strings.Join(parts, ",")
}

// Result is one place found.
type Result struct {
	// Name is the place's name, or its street address when it has none.
	Name string
	// Label says where it is (street, district, town), to tell places of the same name apart; may be empty.
	Label string
	Lat   float64
	Lon   float64
}

// Query is one search.
type Query struct {
	// Text is the search term, as the member typed it.
	Text string
	// BBox is the city the results must lie in.
	BBox BBox
	// Contact is the maintainers' contact for the upstream services (instance settings); may be empty.
	Contact string
}

// Options configures [New].
type Options struct {
	// PhotonURL and NominatimURL are the search endpoints of the upstream services; an empty one is not asked.
	PhotonURL    string
	NominatimURL string
	// Version is the OSFM version, for the User-Agent.
	Version string
	// Transport sends the upstream requests; nil means http.DefaultTransport.
	Transport http.RoundTripper
	// Timeout bounds each upstream request; zero means DefaultTimeout.
	Timeout time.Duration
	// NominatimInterval spaces the requests to Nominatim; zero means DefaultNominatimInterval.
	NominatimInterval time.Duration
	// NominatimMaxWait is how long a search waits for its turn at Nominatim; zero means DefaultNominatimMaxWait, a
	// negative value not at all.
	NominatimMaxWait time.Duration
	// CacheTTL and CacheSize configure the cache; zero means DefaultCacheTTL and DefaultCacheSize.
	CacheTTL  time.Duration
	CacheSize int
	// Logger receives the upstream failures (without search terms); nil means slog.Default().
	Logger *slog.Logger
	// Now is the clock of the cache; nil means time.Now.
	Now func() time.Time
}

// Geocoder searches place names through the upstream services. It is safe for concurrent use.
type Geocoder struct {
	opts   Options
	client *http.Client
	cache  *cache

	mu            sync.Mutex
	nextNominatim time.Time // when the next request to Nominatim may go out
}

// New returns a Geocoder with opts.
func New(opts Options) *Geocoder {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.NominatimInterval <= 0 {
		opts.NominatimInterval = DefaultNominatimInterval
	}
	if opts.NominatimMaxWait == 0 {
		opts.NominatimMaxWait = DefaultNominatimMaxWait
	}
	if opts.CacheTTL <= 0 {
		opts.CacheTTL = DefaultCacheTTL
	}
	if opts.CacheSize <= 0 {
		opts.CacheSize = DefaultCacheSize
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	transport := opts.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	// A client of its own: no cookie jar, and only the headers set in get.
	client := &http.Client{Transport: transport, Timeout: opts.Timeout}
	return &Geocoder{opts: opts, client: client, cache: newCache(opts.CacheSize, opts.CacheTTL, opts.Now)}
}

// UserAgent returns the User-Agent of the upstream requests: OSFM, its version, the project's address and, when
// given, the contact (printable ASCII only, without parentheses, at most 200 characters).
func UserAgent(version, contact string) string {
	if version == "" {
		version = "dev"
	}
	comment := "+" + ProjectURL
	if c := sanitizeContact(contact); c != "" {
		comment += "; " + c
	}
	return "OSFM/" + version + " (" + comment + ")"
}

func sanitizeContact(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			continue
		case r >= 0x21 && r <= 0x7e:
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte(' ')
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

// Search returns the places matching q.Text inside q.BBox, at most [MaxResults], from the cache or the upstream
// services. It returns [ErrUnavailable] when neither service answered, or the context's error when ctx ended first.
func (g *Geocoder) Search(ctx context.Context, q Query) ([]Result, error) {
	key := cacheKey(q)
	if results, ok := g.cache.get(key); ok {
		return results, nil
	}
	ua := UserAgent(g.opts.Version, q.Contact)
	var results []Result
	err := errNotConfigured
	if g.opts.PhotonURL != "" {
		results, err = g.photon(ctx, q, ua)
		if err != nil {
			g.logFailure(ctx, "photon", err)
		}
	}
	if err != nil && ctx.Err() == nil && g.opts.NominatimURL != "" {
		results, err = g.nominatim(ctx, q, ua)
		if err != nil {
			g.logFailure(ctx, "nominatim", err)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	results = inside(results, q.BBox)
	g.cache.put(key, results)
	return clone(results), nil
}

var (
	errNotConfigured = errors.New("no upstream service configured")
	errBusy          = errors.New("waiting for a turn would take too long")
)

// logFailure logs an upstream failure with the service and the kind of failure: never the request URL, which holds
// the search term.
func (g *Geocoder) logFailure(ctx context.Context, service string, err error) {
	if ctx.Err() != nil {
		return // the member went away: not the service's failure
	}
	g.opts.Logger.Warn("place search: upstream service failed", slog.String("service", service),
		slog.String("reason", reason(err)))
}

// statusError is an unsuccessful upstream status.
type statusError int

func (e statusError) Error() string { return "status " + strconv.Itoa(int(e)) }

// reason describes err without the request URL.
func reason(err error) string {
	var status statusError
	var netErr net.Error
	switch {
	case errors.As(err, &status):
		return status.Error()
	case errors.Is(err, errBusy):
		return "rate limit: no turn in time"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "timeout"
	case errors.Is(err, errInvalidResponse):
		return "invalid response"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// The inner error of a failed request (connection refused, DNS, TLS) names hosts and addresses, not the query.
		return "request failed: " + urlErr.Err.Error()
	}
	return "request failed"
}

var errInvalidResponse = errors.New("invalid response")

// get sends a GET request with params added to the endpoint's query and decodes the JSON answer into v. The request
// carries only User-Agent and Accept.
func (g *Geocoder) get(ctx context.Context, endpoint string, params url.Values, ua string, v any) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("endpoint: %w", errInvalidResponse)
	}
	query := u.Query()
	for k, vs := range params {
		query[k] = vs
	}
	u.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(ctx, g.opts.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		return statusError(resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxResponseBytes || json.Unmarshal(body, v) != nil {
		return errInvalidResponse
	}
	return nil
}

// photonResponse is the part of Photon's GeoJSON answer that is used.
type photonResponse struct {
	Features *[]struct {
		Geometry struct {
			Type        string    `json:"type"`
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties struct {
			Name        string `json:"name"`
			Street      string `json:"street"`
			HouseNumber string `json:"housenumber"`
			Locality    string `json:"locality"`
			District    string `json:"district"`
			City        string `json:"city"`
		} `json:"properties"`
	} `json:"features"`
}

func (g *Geocoder) photon(ctx context.Context, q Query, ua string) ([]Result, error) {
	params := url.Values{"q": {q.Text}, "limit": {strconv.Itoa(upstreamLimit)}, "bbox": {q.BBox.String()}}
	var resp photonResponse
	if err := g.get(ctx, g.opts.PhotonURL, params, ua, &resp); err != nil {
		return nil, err
	}
	if resp.Features == nil {
		return nil, errInvalidResponse
	}
	results := []Result{}
	for _, f := range *resp.Features {
		if f.Geometry.Type != "Point" || len(f.Geometry.Coordinates) < 2 {
			continue
		}
		p := f.Properties
		address := joinNonEmpty(" ", p.Street, p.HouseNumber)
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name, address = address, ""
		}
		results = appendResult(results, name, joinLabel(name, address, p.Locality, p.District, p.City),
			f.Geometry.Coordinates[1], f.Geometry.Coordinates[0])
	}
	return results, nil
}

// nominatimPlace is the part of an entry of Nominatim's jsonv2 answer that is used.
type nominatimPlace struct {
	Lat         string            `json:"lat"`
	Lon         string            `json:"lon"`
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name"`
	Address     map[string]string `json:"address"`
}

func (g *Geocoder) nominatim(ctx context.Context, q Query, ua string) ([]Result, error) {
	if err := g.nominatimTurn(ctx); err != nil {
		return nil, err
	}
	params := url.Values{"q": {q.Text}, "format": {"jsonv2"}, "limit": {strconv.Itoa(upstreamLimit)},
		"viewbox": {q.BBox.String()}, "bounded": {"1"}, "addressdetails": {"1"}}
	var places []nominatimPlace
	if err := g.get(ctx, g.opts.NominatimURL, params, ua, &places); err != nil {
		return nil, err
	}
	results := []Result{}
	for _, p := range places {
		lat, err1 := strconv.ParseFloat(p.Lat, 64)
		lon, err2 := strconv.ParseFloat(p.Lon, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		a := p.Address
		address := joinNonEmpty(" ", a["road"], a["house_number"])
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name, address = address, ""
		}
		if name == "" {
			name, _, _ = strings.Cut(p.DisplayName, ",")
		}
		town := firstNonEmpty(a["city"], a["town"], a["village"])
		results = appendResult(results, name,
			joinLabel(name, address, firstNonEmpty(a["suburb"], a["neighbourhood"]), a["city_district"], town), lat, lon)
	}
	return results, nil
}

// nominatimTurn waits until a request to Nominatim may go out, keeping the whole instance's requests
// NominatimInterval apart. It returns errBusy without waiting when the turn is more than NominatimMaxWait away.
func (g *Geocoder) nominatimTurn(ctx context.Context) error {
	g.mu.Lock()
	now := time.Now()
	at := g.nextNominatim
	if at.Before(now) {
		at = now
	}
	wait := at.Sub(now)
	if wait > max(0, g.opts.NominatimMaxWait) {
		g.mu.Unlock()
		return errBusy
	}
	g.nextNominatim = at.Add(g.opts.NominatimInterval)
	g.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// appendResult adds a result with a name and a valid position, without repeating one already there.
func appendResult(results []Result, name, label string, lat, lon float64) []Result {
	name = strings.TrimSpace(name)
	if name == "" || math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return results
	}
	r := Result{Name: name, Label: label, Lat: lat, Lon: lon}
	for _, have := range results {
		if have == r {
			return results
		}
	}
	return append(results, r)
}

// inside keeps the results in b, at most MaxResults.
func inside(results []Result, b BBox) []Result {
	out := []Result{}
	for _, r := range results {
		if b.Contains(r.Lon, r.Lat) && len(out) < MaxResults {
			out = append(out, r)
		}
	}
	return out
}

// joinLabel joins the parts of a label that are not empty, not the name and not repeated.
func joinLabel(name string, parts ...string) string {
	seen := map[string]bool{strings.TrimSpace(name): true}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func clone(results []Result) []Result {
	return append([]Result{}, results...)
}
