package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Map data and the place card (T027): the places of a city's viewport with their counts, filtered; one place with its
// visible recommendations; and the signatures a member can filter by. Everything here is computed from what the
// caller may see (privacy invariant 4): a place exists only with a recommendation the caller may see (M2-Q3), and the
// counts, time slots, filters and signatures use only the recommendations the caller may see that are not hidden
// (domain.CountAttitudes), one per author. The query is in map_query.go.

// Routes of this feature. POST /api/osfm/places, POST /api/osfm/places/{uuid}/recommendations and
// GET /api/osfm/places/nearby belong to places.go (T026); the router prefers the literal "nearby" segment to {uuid},
// whichever is registered first (TestPlaceRoutesPrecedence).
const (
	// MapPlacesPath is GET with ?city=<slug> and optional bbox and filters: []MapPlace, sorted by name, then uuid.
	MapPlacesPath = "/api/osfm/places"
	// PlaceCardPath is GET of one place: [PlaceCard]; 404 when the caller may not see the place, as when it does not
	// exist.
	PlaceCardPath = "/api/osfm/places/{uuid}"
	// SignaturesPath is GET with ?city=<slug>: []string, the signatures to filter the map by.
	SignaturesPath = "/api/osfm/signatures"
)

// Query parameters of [MapPlacesPath] and [SignaturesPath]. A list parameter may be repeated (slots=lunch&slots=dinner);
// the ones with fixed values (slots, category, cuisine) also take comma-separated values (slots=lunch,dinner). Empty
// values are ignored. Different parameters are combined with AND, the values of one parameter with OR (A3).
const (
	// MapParamCity is the city's slug (required).
	MapParamCity = "city"
	// MapParamBBox is minLon,minLat,maxLon,maxLat in WGS-84 degrees; it is clipped to the city, and the whole city
	// when absent. A box that does not overlap the city is refused (400 outside_city).
	MapParamBBox = "bbox"
	// MapParamSlots keeps the places with a counted recommendation for one of these time slots.
	MapParamSlots = "slots"
	// MapParamCategory keeps the places of one of these categories (places without a category never match).
	MapParamCategory = "category"
	// MapParamCuisine keeps the places with one of these cuisines (lowercase OSM cuisine values).
	MapParamCuisine = "cuisine"
	// MapParamBy keeps the places with a counted recommendation signed with exactly one of these names (repeat the
	// parameter for several; names are not split at commas). A main name does not match its member's pseudonym
	// recommendations, and no name matches the main-name recommendations of a member who left.
	MapParamBy = "by"
	// MapParamMustTry, set to 1, keeps the places where someone says must try (counts.mustTry > 0).
	MapParamMustTry = "mustTry"
	// MapParamNoAvoid, set to 1, leaves out the places where someone says avoid (keeps counts.avoid == 0).
	MapParamNoAvoid = "noAvoid"
)

// Error codes of the map data, besides invalid_request (a malformed bbox or filter value, a missing city).
const (
	// CodeMapUnknownCity: the city parameter names no city of the instance.
	CodeMapUnknownCity = "unknown_city"
	// CodeMapOutsideCity: the bbox does not overlap the city.
	CodeMapOutsideCity = "outside_city"
)

// MapPlace is a place on the map (architecture §3.2): the minimal fields to draw and filter it. It carries no member
// information and no average.
type MapPlace struct {
	UUID string  `json:"uuid"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	// Precision is exact, approx or area: approx and area places are drawn as circles, not pins (H3).
	Precision string `json:"precision"`
	// Category is one of the place categories, "" when the place has none.
	Category string      `json:"category"`
	Counts   PlaceCounts `json:"counts"`
	// TimeSlots are the time slots of the counted recommendations, in the order breakfast, lunch, afternoon, dinner,
	// late_night.
	TimeSlots []string `json:"timeSlots"`
}

// PlaceCounts is the number of people per attitude at a place (A4): only the recommendations the caller may see that
// are not hidden count, each author once. Numbers only.
type PlaceCounts struct {
	MustTry int `json:"mustTry"`
	Good    int `json:"good"`
	OK      int `json:"ok"`
	Avoid   int `json:"avoid"`
}

// PlaceCard is GET [PlaceCardPath]: the place's fields, its counts and every recommendation the caller may see,
// newest first. Hidden recommendations are listed for those who may see them (their author, maintainers) with
// hidden set, but never counted.
type PlaceCard struct {
	UUID    string   `json:"uuid"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Lat     float64  `json:"lat"`
	Lon     float64  `json:"lon"`
	// Precision is exact, approx or area (H3).
	Precision    string `json:"precision"`
	PositionNote string `json:"positionNote"`
	// Landmark is absent when the place has none, or when it names a place the caller may not see.
	Landmark *PlaceLandmark `json:"landmark,omitempty"`
	// Category is one of the place categories, "" when the place has none.
	Category string   `json:"category"`
	Cuisine  []string `json:"cuisine"`
	// Status (open, maybe_closed, closed) and when it was last confirmed, absent when nobody confirmed one.
	Status            string     `json:"status,omitempty"`
	StatusConfirmedAt *time.Time `json:"statusConfirmedAt,omitempty"`
	// City is the slug of the first city of the instance whose bounding box holds the place; "" when none does.
	City            string               `json:"city"`
	Counts          PlaceCounts          `json:"counts"`
	Recommendations []RecommendationView `json:"recommendations"`
}

// Kinds of [PlaceLandmark].
const (
	LandmarkKindPlace = "place"
	LandmarkKindText  = "text"
)

// PlaceLandmark is what a place's position is described by (B4): another place of the instance, or a text.
type PlaceLandmark struct {
	// Type is LandmarkKindPlace or LandmarkKindText.
	Type string `json:"type"`
	// Place is the landmark place, for LandmarkKindPlace.
	Place *LandmarkPlace `json:"place,omitempty"`
	// Text is the landmark text, for LandmarkKindText; shown as plain text.
	Text string `json:"text,omitempty"`
}

// LandmarkPlace is a landmark place, which the caller may see: its uuid (for a link to its card) and name.
type LandmarkPlace struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

func registerMap(se *core.ServeEvent, s *server) {
	_ = s
	se.Router.GET(MapPlacesPath, handleMapPlaces)
	se.Router.GET(PlaceCardPath, handlePlaceCard)
	se.Router.GET(SignaturesPath, handleSignatures)
}

func handleMapPlaces(e *core.RequestEvent) error {
	q := e.Request.URL.Query()
	area, ok, err := mapAreaOf(e, q.Get(MapParamCity), q.Get(MapParamBBox))
	if !ok {
		return err
	}
	f, err := parseMapFilter(q)
	if err != nil {
		return badRequest(e)
	}
	places, err := queryMapPlaces(e.App, ViewerOf(e), area, f)
	if err != nil {
		return e.InternalServerError("", err)
	}
	return respondMapData(e, places)
}

func handleSignatures(e *core.RequestEvent) error {
	q := e.Request.URL.Query()
	area, ok, err := mapAreaOf(e, q.Get(MapParamCity), "")
	if !ok {
		return err
	}
	names, err := querySignatures(e.App, ViewerOf(e), area)
	if err != nil {
		return e.InternalServerError("", err)
	}
	return respondMapData(e, names)
}

// mapAreaOf returns the area of a map request: the city's bounding box, clipped to bbox when given. When ok is false
// it has answered the request already (400), and err is what the handler returns.
func mapAreaOf(e *core.RequestEvent, slug, bbox string) (area mapBox, ok bool, err error) {
	if slug == "" {
		return mapBox{}, false, badRequest(e)
	}
	inst, err := settings.Load(e.App)
	if err != nil {
		return mapBox{}, false, e.InternalServerError("", err)
	}
	var city *settings.City
	for i := range inst.Cities {
		if inst.Cities[i].Slug == slug {
			city = &inst.Cities[i]
			break
		}
	}
	if city == nil {
		return mapBox{}, false, fail(e, http.StatusBadRequest, CodeMapUnknownCity, "No such city.")
	}
	area = mapBox{city.BBox[0], city.BBox[1], city.BBox[2], city.BBox[3]}
	if bbox == "" {
		return area, true, nil
	}
	view, perr := parseMapBBox(bbox)
	if perr != nil {
		return mapBox{}, false, badRequest(e)
	}
	area, overlaps := area.intersect(view)
	if !overlaps {
		return mapBox{}, false, fail(e, http.StatusBadRequest, CodeMapOutsideCity, "The box is outside the city.")
	}
	return area, true, nil
}

// respondMapData answers the map data v with Cache-Control: private, no-cache and an ETag of the body: the web app's
// service worker asks the network first (T043), and an unchanged answer costs a 304. The body depends on who asks,
// so it is private and varies with the session cookie.
func respondMapData(e *core.RequestEvent, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return e.InternalServerError("", err)
	}
	sum := sha256.Sum256(body)
	etag := `"` + base64.RawURLEncoding.EncodeToString(sum[:18]) + `"`
	h := e.Response.Header()
	h.Set("Cache-Control", "private, no-cache")
	h.Set("ETag", etag)
	h.Add("Vary", "Cookie")
	if etagMatches(e.Request.Header.Get("If-None-Match"), etag) {
		return e.NoContent(http.StatusNotModified)
	}
	return e.Blob(http.StatusOK, "application/json", body)
}

// etagMatches reports whether the If-None-Match header value lists etag (weak comparison, as for GET).
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == etag || candidate == "*" {
			return true
		}
	}
	return false
}

func handlePlaceCard(e *core.RequestEvent) error {
	id, ok := pathUUID(e, "uuid")
	if !ok {
		return notFound(e)
	}
	card, err := LoadPlaceCard(e.App, ViewerOf(e), id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return notFound(e)
	case err != nil:
		return e.InternalServerError("", err)
	}
	return respond(e, http.StatusOK, card)
}

// LoadPlaceCard returns the card of the place with the uuid id for v, or an error wrapping sql.ErrNoRows when v may
// not see such a place (M2-Q3), whether or not it exists.
func LoadPlaceCard(app core.App, v domain.Viewer, id string) (*PlaceCard, error) {
	place, err := store.VisiblePlaces(app, v).Where(dbx.HashExp{store.Places + "." + store.FieldUUID: id}).One()
	if err != nil {
		return nil, err
	}
	recs, err := store.VisibleRecommendations(app, v).
		Where(dbx.HashExp{store.Recommendations + ".place": place.Id}).
		OrderBy(store.FieldCreated+" DESC", store.FieldUUID+" DESC").All()
	if err != nil {
		return nil, err
	}
	views, err := RecommendationViews(app, v, recs)
	if err != nil {
		return nil, err
	}
	counts, err := countsOf(app, v, recs)
	if err != nil {
		return nil, err
	}
	landmark, err := landmarkOf(app, v, place)
	if err != nil {
		return nil, err
	}
	inst, err := settings.Load(app)
	if err != nil {
		return nil, err
	}
	card := &PlaceCard{
		UUID:            place.GetString(store.FieldUUID),
		Name:            place.GetString("name"),
		Aliases:         stringList(place, "aliases"),
		Lat:             place.GetFloat("lat"),
		Lon:             place.GetFloat("lon"),
		Precision:       place.GetString("precision"),
		PositionNote:    place.GetString("position_note"),
		Landmark:        landmark,
		Category:        place.GetString("category"),
		Cuisine:         stringList(place, "cuisine"),
		Status:          place.GetString("status"),
		City:            cityOfPoint(inst.Cities, place.GetFloat("lon"), place.GetFloat("lat")),
		Counts:          counts,
		Recommendations: views,
	}
	if card.Status != "" {
		if d := place.GetDateTime("status_confirmed_at"); !d.IsZero() {
			t := d.Time().UTC()
			card.StatusConfirmedAt = &t
		}
	}
	return card, nil
}

// countsOf counts the attitudes of recs, a place's recommendations newest first, for v (domain.CountAttitudes).
func countsOf(app core.App, v domain.Viewer, recs []*core.Record) (PlaceCounts, error) {
	states, err := store.RecommendationStates(app, recs)
	if err != nil {
		return PlaceCounts{}, err
	}
	counted := make([]domain.CountedRecommendation, len(recs))
	for i, r := range recs {
		counted[i] = domain.CountedRecommendation{Recommendation: states[i], Attitude: r.GetString("attitude")}
	}
	c := domain.CountAttitudes(v, counted)
	return PlaceCounts{MustTry: c.MustTry, Good: c.Good, OK: c.OK, Avoid: c.Avoid}, nil
}

// storedLandmark is places.landmark as stored: {type: place, place_uuid} or {type: text, text}. "place", the name of
// the field in the create request, is read too when place_uuid is empty.
type storedLandmark struct {
	Type      string `json:"type"`
	PlaceUUID string `json:"place_uuid"`
	Place     string `json:"place"`
	Text      string `json:"text"`
}

// landmarkOf returns the landmark of place for v: a landmark place only if v may see it (otherwise none at all, as if
// the place had no landmark), a text as stored.
func landmarkOf(app core.App, v domain.Viewer, place *core.Record) (*PlaceLandmark, error) {
	var l storedLandmark
	if err := place.UnmarshalJSONField("landmark", &l); err != nil {
		return nil, nil //nolint:nilerr // anything else stored is no landmark
	}
	switch l.Type {
	case LandmarkKindText:
		if text := strings.TrimSpace(l.Text); text != "" {
			return &PlaceLandmark{Type: LandmarkKindText, Text: text}, nil
		}
	case LandmarkKindPlace:
		id := l.PlaceUUID
		if id == "" {
			id = l.Place
		}
		if !store.IsUUID(id) || id == place.GetString(store.FieldUUID) {
			return nil, nil
		}
		other, err := store.VisiblePlaces(app, v).Where(dbx.HashExp{store.Places + "." + store.FieldUUID: id}).One()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &PlaceLandmark{Type: LandmarkKindPlace,
			Place: &LandmarkPlace{UUID: id, Name: other.GetString("name")}}, nil
	}
	return nil, nil
}

// cityOfPoint returns the slug of the first city whose bounding box holds lon, lat; "" when none does.
func cityOfPoint(cities []settings.City, lon, lat float64) string {
	for _, c := range cities {
		if lon >= c.BBox[0] && lon <= c.BBox[2] && lat >= c.BBox[1] && lat <= c.BBox[3] {
			return c.Slug
		}
	}
	return ""
}
