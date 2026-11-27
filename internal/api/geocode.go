package api

import (
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/geocode"
	"github.com/EatWell-Studio/OSFM/internal/ratelimit"
	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/version"
)

// Place-name search (B7, AD11, M2-Q13): the web app's search box asks this instance, which asks the upstream search
// services (internal/geocode), so that the member's browser talks to no one else (E5). Members only. The search term
// travels in the query string, which the request log drops (internal/boundary); nothing here logs it either.

// GeocodePath is GET ?q=<search term>&city=<city slug> → [GeocodeResult] list (at most 10), places inside the city.
const GeocodePath = "/api/osfm/geocode"

// Error codes of place search.
const (
	CodeGeocoderDisabled    = "geocoder_disabled"    // 404: the maintainers turned place search off
	CodeGeocoderUnavailable = "geocoder_unavailable" // 503: neither search service answered; place the pin by hand
)

// MaxGeocodeQuery is the longest search term, in characters.
const MaxGeocodeQuery = 200

// GeocodeResult is one place found by GET [GeocodePath].
type GeocodeResult struct {
	// Name is the place's name, or its street address when it has none.
	Name string `json:"name"`
	// Label says where it is (street, district, town), to tell places of the same name apart; may be empty.
	Label string  `json:"label"`
	Lat   float64 `json:"lat"`
	Lon   float64 `json:"lon"`
}

// geocodeBudgetID is the route middleware of the per-member search budget.
const geocodeBudgetID = "osfmAPIGeocodeBudget"

// geocodeRate is every member's search budget: 10 searches at once, then one every 6 seconds (10 a minute). A person
// searching for a place needs a few; the cache answers repeats without asking the services, but they count here too.
var geocodeRate = ratelimit.Rate{Burst: 10, Every: 6 * time.Second}

func registerGeocode(se *core.ServeEvent, s *server) {
	opts := geocode.Options{Version: version.String(), Logger: se.App.Logger()}
	// Without a configuration (some tests), no upstream is asked: searches answer 503.
	if cfg := s.Config(); cfg != nil {
		opts.PhotonURL, opts.NominatimURL = cfg.GeocoderPhotonURL, cfg.GeocoderNominatimURL
	}
	g := geocode.New(opts)
	se.Router.GET(GeocodePath, func(e *core.RequestEvent) error { return handleGeocode(e, g) }).
		Bind(s.MemberBudget(geocodeBudgetID, geocodeRate))
}

func handleGeocode(e *core.RequestEvent, g *geocode.Geocoder) error {
	circle, err := settings.LoadCircle(e.App)
	if err != nil {
		return e.InternalServerError("", err)
	}
	if !circle.GeocoderEnabled {
		return fail(e, http.StatusNotFound, CodeGeocoderDisabled, "Place search is turned off.")
	}
	text, ok := geocodeQuery(e.Request.URL.Query().Get("q"))
	if !ok {
		return badRequest(e)
	}
	inst, err := settings.Load(e.App)
	if err != nil {
		return e.InternalServerError("", err)
	}
	slug := e.Request.URL.Query().Get("city")
	var bbox geocode.BBox
	found := false
	for _, c := range inst.Cities {
		if c.Slug == slug {
			bbox, found = geocode.BBox(c.BBox), true
			break
		}
	}
	if !found {
		return badRequest(e)
	}
	results, err := g.Search(e.Request.Context(), geocode.Query{Text: text, BBox: bbox, Contact: circle.GeocoderContact})
	if err != nil {
		// geocode.ErrUnavailable, or the member went away. Not e.InternalServerError(err): PocketBase would log the
		// error with the request.
		return fail(e, http.StatusServiceUnavailable, CodeGeocoderUnavailable, "Place search is unavailable.")
	}
	out := make([]GeocodeResult, 0, len(results))
	for _, r := range results {
		out = append(out, GeocodeResult{Name: r.Name, Label: r.Label, Lat: r.Lat, Lon: r.Lon})
	}
	return respond(e, http.StatusOK, out)
}

// geocodeQuery trims the search term; ok is false when it is empty, longer than [MaxGeocodeQuery] characters, not
// UTF-8 or contains control characters.
func geocodeQuery(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxGeocodeQuery {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return s, true
}
