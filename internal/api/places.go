package api

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Creating places and recommendations (T026): a new place with its first recommendation, in one transaction (a place
// exists for a member only with a recommendation they may see, M2-Q3); a recommendation for a place the member can
// see, at most one live one per member and place (M2-Q1); and the places nearby with a similar name, for the hint "is
// it this one?" (B6). The rules of the values are in internal/domain (NormalizePlace, NormalizeRecommendation).
//
// Places carry no member fields: who created a place is recorded only in the audit log (places.create), and no
// response of these routes names a member.

// Routes of this feature.
const (
	// PlacesPath is POST: a new place with its first recommendation ([CreatePlaceRequest] → 201 [CreatedPlace]).
	PlacesPath = "/api/osfm/places"
	// PlacesNearbyPath is GET ?lat=&lon=&name=: the places the caller can see within 100 m with a similar name
	// ([NearbyPlaces]).
	PlacesNearbyPath = "/api/osfm/places/nearby"
	// PlaceRecommendationsPath is POST: a recommendation for the place {uuid} ([RecommendationInput] → 201
	// [CreatedRecommendation]; 409 [CodeAlreadyRecommended] with [AlreadyRecommended] in the error's data).
	PlaceRecommendationsPath = "/api/osfm/places/{uuid}/recommendations"
)

// Error codes of this feature. A field that is not valid otherwise answers 400 invalid_request with the field's name
// in the error's data ([InvalidField]).
const (
	// CodeOutsideCities (400): the coordinates are in none of the instance's cities (H2).
	CodeOutsideCities = "outside_cities"
	// CodeNoPseudonym (400): the recommendation is to be signed with a pseudonym, and the member has none.
	CodeNoPseudonym = "no_pseudonym"
	// CodeAlreadyRecommended (409): the member has a live recommendation of this place already; the error's data is
	// [AlreadyRecommended], the uuid of that recommendation (theirs), so that the app opens it for editing (M2-Q1).
	CodeAlreadyRecommended = "already_recommended"
)

// AuditPlaceCreate is the audit action of a new place: the actor is the member who created it, the target the place.
const AuditPlaceCreate = "places.create"

// CreatePlaceRequest is the body of POST [PlacesPath]: a new place and its first recommendation. Only the name, the
// coordinates and the recommendation's attitude and text are required (B2, C2).
type CreatePlaceRequest struct {
	// Name has 1 to 200 characters, on one line.
	Name string `json:"name"`
	// Aliases are other names of the place: at most 10, each like a name.
	Aliases []string `json:"aliases,omitempty"`
	// Lat and Lon are WGS-84 degrees, inside one of the instance's cities ([CodeOutsideCities]). They are stored
	// rounded to 5 decimals; the values as sent are kept in the place's source coordinates.
	Lat *float64 `json:"lat" tstype:"number,required"`
	Lon *float64 `json:"lon" tstype:"number,required"`
	// Precision is exact (the default), approx or area.
	Precision string `json:"precision,omitempty"`
	// PositionNote describes where the place is in the member's words (at most 500 characters, may span lines).
	PositionNote string `json:"positionNote,omitempty"`
	// Landmark is what the position is described relative to: a place the member can see, or a text.
	Landmark *LandmarkInput `json:"landmark,omitempty"`
	// Category is empty or one of the food whitelist (web/src/lib/enums.ts: categories).
	Category string `json:"category,omitempty"`
	// Cuisine lists values of the cuisine list (enums.ts: cuisines).
	Cuisine []string `json:"cuisine,omitempty"`
	// SourceCoord says how the coordinates were entered; absent means WGS-84 from the crosshair.
	SourceCoord *SourceCoordInput `json:"sourceCoord,omitempty"`
	// Recommendation is the place's first recommendation.
	Recommendation *RecommendationInput `json:"recommendation" tstype:",required"`
}

// LandmarkInput is a landmark (B4): {type: "place", place: <uuid>} or {type: "text", text}.
type LandmarkInput struct {
	Type string `json:"type"`
	// Place is the uuid of a place the member can see, for type "place".
	Place string `json:"place,omitempty"`
	// Text has 1 to 200 characters on one line, for type "text".
	Text string `json:"text,omitempty"`
}

// SourceCoordInput says how a place's coordinates were entered (B12).
type SourceCoordInput struct {
	// CRS is the coordinate system of lat and lon: only "wgs84" (the default) for now.
	CRS string `json:"crs,omitempty"`
	// Source is "crosshair" (the default: the member placed the map's crosshair) or "search" (a place search result,
	// not moved afterwards).
	Source string `json:"source,omitempty"`
}

// RecommendationInput is a recommendation as a member writes it: the body of POST [PlaceRecommendationsPath] and the
// recommendation of [CreatePlaceRequest]. Only the attitude and the text are required (C2).
type RecommendationInput struct {
	// Attitude is must_try, good, ok or avoid.
	Attitude string `json:"attitude"`
	// Text is not empty once trimmed, at most 5000 characters, may span lines.
	Text string `json:"text"`
	// Dishes are at most 20, of at most 100 characters each.
	Dishes []string `json:"dishes,omitempty"`
	// TimeSlots are values of enums.ts: timeSlots.
	TimeSlots []string `json:"timeSlots,omitempty"`
	// Season is at most 100 characters ("winter").
	Season string `json:"season,omitempty"`
	// Sources are at most 10: an optional http(s) link, a kind (enums.ts: sourceKinds) and an optional excerpt of at
	// most 500 characters.
	Sources []Source `json:"sources,omitempty"`
	// VisitedOn is the date of the last visit, YYYY-MM-DD, not later than today.
	VisitedOn string `json:"visitedOn,omitempty"`
	// DisplayAs is main (the default) or pseudonym, which needs a pseudonym ([CodeNoPseudonym]). It cannot be changed
	// later.
	DisplayAs string `json:"displayAs,omitempty"`
	// Visibility is circle (the default) or private; federated is reserved and refused.
	Visibility string `json:"visibility,omitempty"`
}

// CreatedPlace is the response of POST [PlacesPath]: the uuids of the new place and of its first recommendation.
type CreatedPlace struct {
	Place          string `json:"place"`
	Recommendation string `json:"recommendation"`
}

// CreatedRecommendation is the response of POST [PlaceRecommendationsPath]: the uuid of the new recommendation.
type CreatedRecommendation struct {
	Recommendation string `json:"recommendation"`
}

// AlreadyRecommended is the data of a [CodeAlreadyRecommended] error: the uuid of the caller's own live
// recommendation of the place.
type AlreadyRecommended struct {
	Recommendation string `json:"recommendation"`
}

// InvalidField is the data of an invalid_request error of these routes about one field: its name as in the request
// ("name", "recommendation.text", "recommendation.sources[1].url").
type InvalidField struct {
	Field string `json:"field"`
}

// NearbyPlaces is the response of GET [PlacesNearbyPath]: at most 5 places, the nearest first.
type NearbyPlaces struct {
	Places []NearbyPlace `json:"places"`
}

// NearbyPlace is a place offered by the duplicate hint: no coordinates, only how far it is.
type NearbyPlace struct {
	UUID     string `json:"uuid"`
	Name     string `json:"name"`
	Category string `json:"category"`
	// Distance is in metres, rounded to 10 m (H3).
	Distance int `json:"distance"`
}

func registerPlaces(se *core.ServeEvent, s *server) {
	se.Router.POST(PlacesPath, handleCreatePlace).Bind(s.WriteBudget())
	se.Router.POST(PlaceRecommendationsPath, handleCreateRecommendation).Bind(s.WriteBudget())
	se.Router.GET(PlacesNearbyPath, handleNearby)
}

// errLandmarkHidden: the landmark place does not exist for the caller.
var errLandmarkHidden = errors.New("api: the landmark place is not visible")

// errAlreadyRecommended carries the uuid of the caller's live recommendation of the place.
type errAlreadyRecommended struct{ uuid string }

func (e *errAlreadyRecommended) Error() string { return "api: already recommended" }

func handleCreatePlace(e *core.RequestEvent) error {
	member := auth.CurrentMember(e)
	v := ViewerOf(e)
	var req CreatePlaceRequest
	if err := readJSON(e, &req, DefaultMaxBody); err != nil {
		return requestError(e, err)
	}
	switch {
	case req.Lat == nil:
		return invalidField(e, "lat")
	case req.Lon == nil:
		return invalidField(e, "lon")
	case req.Recommendation == nil:
		return invalidField(e, "recommendation")
	}
	inst, err := settings.Load(e.App)
	if err != nil {
		return e.InternalServerError("", err)
	}
	cities := make([]domain.Bounds, 0, len(inst.Cities))
	for _, c := range inst.Cities {
		cities = append(cities, domain.Bounds(c.BBox))
	}
	draft := domain.PlaceDraft{Name: req.Name, Aliases: req.Aliases, Lat: *req.Lat, Lon: *req.Lon,
		Precision: req.Precision, PositionNote: req.PositionNote, Category: req.Category, Cuisine: req.Cuisine}
	if l := req.Landmark; l != nil {
		draft.Landmark = &domain.Landmark{Type: l.Type, PlaceUUID: l.Place, Text: l.Text}
	}
	if sc := req.SourceCoord; sc != nil {
		draft.CRS, draft.CoordSource = sc.CRS, sc.Source
	}
	place, coord, err := domain.NormalizePlace(draft, cities, store.IsUUID)
	if err != nil {
		return validationError(e, err, "")
	}

	var created CreatedPlace
	var invalid error
	err = e.App.RunInTransaction(func(tx core.App) error {
		rec, err := normalizeRecommendationIn(tx, member, *req.Recommendation)
		if err != nil {
			invalid = err
			return err
		}
		var landmark map[string]any
		if l := place.Landmark; l != nil {
			landmark = map[string]any{"type": l.Type}
			if l.Type == domain.LandmarkPlace {
				if _, err := store.VisiblePlaces(tx, v).Where(dbx.HashExp{store.Places + "." + store.FieldUUID: l.PlaceUUID}).One(); err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						return errLandmarkHidden
					}
					return err
				}
				landmark["place_uuid"] = l.PlaceUUID
			} else {
				landmark["text"] = l.Text
			}
		}
		p, err := newRecord(tx, store.Places)
		if err != nil {
			return err
		}
		p.Load(map[string]any{
			"name": place.Name, "aliases": place.Aliases, "lat": place.Lat, "lon": place.Lon,
			"precision": place.Precision, "position_note": place.PositionNote, "category": place.Category,
			"cuisine": place.Cuisine,
			"source_coord": map[string]any{"lat": coord.Lat, "lon": coord.Lon, "crs": coord.CRS,
				"source": coord.Source},
		})
		if landmark != nil {
			p.Set("landmark", landmark)
		}
		if err := tx.Save(p); err != nil {
			return err
		}
		r, err := saveRecommendation(tx, p, member, rec)
		if err != nil {
			return err
		}
		created = CreatedPlace{Place: p.GetString(store.FieldUUID), Recommendation: r.GetString(store.FieldUUID)}
		return store.WriteAudit(tx, member, AuditPlaceCreate, store.Places, created.Place, nil)
	})
	var fe *domain.FieldError
	switch {
	case errors.As(invalid, &fe):
		return validationError(e, invalid, "recommendation.")
	case errors.Is(err, errLandmarkHidden):
		return invalidField(e, "landmark.place")
	case err != nil:
		return e.InternalServerError("", err)
	}
	return respond(e, http.StatusCreated, created)
}

func handleCreateRecommendation(e *core.RequestEvent) error {
	member := auth.CurrentMember(e)
	v := ViewerOf(e)
	id, ok := pathUUID(e, "uuid")
	if !ok {
		return notFound(e)
	}
	var req RecommendationInput
	if err := readJSON(e, &req, DefaultMaxBody); err != nil {
		return requestError(e, err)
	}
	var created CreatedRecommendation
	var invalid error
	err := e.App.RunInTransaction(func(tx core.App) error {
		place, err := store.VisiblePlaces(tx, v).Where(dbx.HashExp{store.Places + "." + store.FieldUUID: id}).One()
		if err != nil {
			return err // sql.ErrNoRows: 404
		}
		// The caller's own recommendation, which they always see: telling them about it reveals nothing (M2-Q1).
		if mine, err := liveRecommendationOf(tx, place, member); err == nil {
			return &errAlreadyRecommended{uuid: mine.GetString(store.FieldUUID)}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		rec, err := normalizeRecommendationIn(tx, member, req)
		if err != nil {
			invalid = err
			return err
		}
		r, err := saveRecommendation(tx, place, member, rec)
		if err != nil {
			// The unique index on (place, author) is the last line: a recommendation saved since the check above.
			if mine, findErr := liveRecommendationOf(tx, place, member); findErr == nil {
				return &errAlreadyRecommended{uuid: mine.GetString(store.FieldUUID)}
			}
			return err
		}
		created = CreatedRecommendation{Recommendation: r.GetString(store.FieldUUID)}
		return nil
	})
	var already *errAlreadyRecommended
	var fe *domain.FieldError
	switch {
	case errors.As(invalid, &fe):
		return validationError(e, invalid, "")
	case errors.As(err, &already):
		return failWith(e, http.StatusConflict, CodeAlreadyRecommended,
			"You have recommended this place already: edit your recommendation.",
			map[string]any{"recommendation": already.uuid})
	case errors.Is(err, sql.ErrNoRows):
		return notFound(e)
	case err != nil:
		return e.InternalServerError("", err)
	}
	return respond(e, http.StatusCreated, created)
}

func handleNearby(e *core.RequestEvent) error {
	v := ViewerOf(e)
	q := e.Request.URL.Query()
	lat, latErr := strconv.ParseFloat(q.Get("lat"), 64)
	lon, lonErr := strconv.ParseFloat(q.Get("lon"), 64)
	name := strings.TrimSpace(q.Get("name"))
	if latErr != nil || lonErr != nil || !domain.ValidCoordinates(lat, lon) || name == "" ||
		utf8.RuneCountInString(name) > domain.MaxPlaceNameLength {
		return badRequest(e)
	}
	// Candidates by the (lat, lon) index, with a margin for the stored coordinates' rounding; the distance decides.
	minLat, minLon, maxLat, maxLon := domain.NearbyBox(lat, lon, domain.NearbyRadius+5)
	candidates, err := store.VisiblePlaces(e.App, v).
		Where(dbx.Between(store.Places+".lat", minLat, maxLat)).
		Where(dbx.Between(store.Places+".lon", minLon, maxLon)).All()
	if err != nil {
		return e.InternalServerError("", err)
	}
	type match struct {
		place    NearbyPlace
		distance float64
	}
	var matches []match
	for _, p := range candidates {
		d := domain.DistanceMeters(lat, lon, p.GetFloat("lat"), p.GetFloat("lon"))
		if d > domain.NearbyRadius {
			continue
		}
		names := append([]string{p.GetString("name")}, stringList(p, "aliases")...)
		if !slices.ContainsFunc(names, func(n string) bool { return domain.SimilarNames(name, n) }) {
			continue
		}
		matches = append(matches, match{NearbyPlace{UUID: p.GetString(store.FieldUUID), Name: p.GetString("name"),
			Category: p.GetString("category"), Distance: domain.RoundDistance(d)}, d})
	}
	slices.SortFunc(matches, func(a, b match) int {
		if a.distance != b.distance {
			if a.distance < b.distance {
				return -1
			}
			return 1
		}
		return strings.Compare(a.place.UUID, b.place.UUID)
	})
	out := NearbyPlaces{Places: []NearbyPlace{}}
	for i, m := range matches {
		if i == domain.MaxNearby {
			break
		}
		out.Places = append(out.Places, m.place)
	}
	return respond(e, http.StatusOK, out)
}

// recommendationDraft is req as the domain rules take it.
func recommendationDraft(req RecommendationInput) domain.RecommendationDraft {
	d := domain.RecommendationDraft{Attitude: req.Attitude, Text: req.Text, Dishes: req.Dishes,
		TimeSlots: req.TimeSlots, Season: req.Season, VisitedOn: req.VisitedOn, DisplayAs: req.DisplayAs,
		Visibility: req.Visibility}
	for _, s := range req.Sources {
		d.Sources = append(d.Sources, domain.Source{URL: s.URL, Kind: s.Kind, Excerpt: s.Excerpt})
	}
	return d
}

// normalizeRecommendationIn checks req as domain.NormalizeRecommendation does, inside the transaction tx that saves it:
// whether member has a pseudonym to sign with is read from the database in tx, not from the record the session loaded,
// so that a pseudonym cleared in the meantime (which is refused only while pseudonym recommendations exist) cannot end
// up on a new recommendation. Errors other than a *domain.FieldError are database errors.
func normalizeRecommendationIn(tx core.App, member *core.Record, req RecommendationInput) (domain.RecommendationDraft,
	error) {
	fresh, err := store.Query(tx, store.Members).Where(dbx.HashExp{store.Members + ".id": member.Id}).One()
	if err != nil {
		return domain.RecommendationDraft{}, err
	}
	hasPseudonym := strings.TrimSpace(fresh.GetString("pseudonym")) != ""
	return domain.NormalizeRecommendation(recommendationDraft(req), hasPseudonym, time.Now())
}

// liveRecommendationOf returns member's live recommendation of place, or an error wrapping sql.ErrNoRows.
func liveRecommendationOf(app core.App, place, member *core.Record) (*core.Record, error) {
	return store.Query(app, store.Recommendations).
		Where(dbx.HashExp{store.Recommendations + ".place": place.Id, store.Recommendations + ".author": member.Id}).
		One()
}

// saveRecommendation saves rec, checked by domain.NormalizeRecommendation, as member's recommendation of place.
func saveRecommendation(tx core.App, place, member *core.Record, rec domain.RecommendationDraft) (*core.Record, error) {
	r, err := newRecord(tx, store.Recommendations)
	if err != nil {
		return nil, err
	}
	sources := make([]map[string]any, 0, len(rec.Sources))
	for _, s := range rec.Sources {
		src := map[string]any{"kind": s.Kind}
		if s.URL != "" {
			src["url"] = s.URL
		}
		if s.Excerpt != "" {
			src["excerpt"] = s.Excerpt
		}
		sources = append(sources, src)
	}
	r.Load(map[string]any{
		"place": place.Id, "author": member.Id, "display_as": rec.DisplayAs, "attitude": rec.Attitude,
		"visibility": rec.Visibility, "text": rec.Text, "dishes": rec.Dishes, "time_slots": rec.TimeSlots,
		"season": rec.Season, "sources": sources,
	})
	if rec.VisitedOn != "" {
		r.Set("visited_on", rec.VisitedOn+" 00:00:00.000Z")
	}
	if err := tx.Save(r); err != nil {
		return nil, err
	}
	return r, nil
}

func newRecord(app core.App, collection string) (*core.Record, error) {
	c, err := app.FindCachedCollectionByNameOrId(collection)
	if err != nil {
		return nil, err
	}
	return core.NewRecord(c), nil
}

// validationError answers an error of domain.NormalizePlace or domain.NormalizeRecommendation: 400 with
// [CodeOutsideCities], [CodeNoPseudonym] or invalid_request and the field (prefixed with prefix) as [InvalidField].
func validationError(e *core.RequestEvent, err error, prefix string) error {
	var fe *domain.FieldError
	if !errors.As(err, &fe) {
		return e.InternalServerError("", err)
	}
	switch {
	case errors.Is(err, domain.ErrOutsideCities):
		return fail(e, http.StatusBadRequest, CodeOutsideCities, "The place is outside the instance's cities.")
	case errors.Is(err, domain.ErrNoPseudonym):
		return fail(e, http.StatusBadRequest, CodeNoPseudonym, "You have no pseudonym to sign with.")
	default:
		return invalidField(e, prefix+fe.Field)
	}
}

// invalidField answers 400 invalid_request naming the field ([InvalidField]).
func invalidField(e *core.RequestEvent, field string) error {
	return failWith(e, http.StatusBadRequest, CodeInvalidRequest, "The request is not valid.",
		map[string]any{"field": field})
}

// failWith answers an [Error] with data, not cached.
func failWith(e *core.RequestEvent, status int, code, message string, data map[string]any) error {
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(status, Error{Status: status, Message: message, Data: data, Code: code})
}
