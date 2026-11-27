package domain

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The fixed values of places and recommendations. These lists are the canonical ones: the routes accept exactly these
// values, and the web app lists the same ones in the same order (web/src/lib/enums.ts; TestEnumsMatchWebApp fails
// when the two differ).

// Precisions of a place's point (places.precision, B3): at the door, within about 50 m, somewhere in an area.
const (
	PrecisionExact  = "exact"
	PrecisionApprox = "approx"
	PrecisionArea   = "area"
)

// Precisions lists the precisions; [PrecisionExact] is the default.
var Precisions = []string{PrecisionExact, PrecisionApprox, PrecisionArea}

// PlaceCategories is the food whitelist of places.category (B5, H1, M2-Q5). The values are OpenStreetMap tag values,
// so that a place linked to OSM maps to a category without a translation table: amenity=restaurant, fast_food, cafe,
// food_court, ice_cream, bar, pub and biergarten; shop=bakery and shop=pastry; street_vendor for stalls and carts,
// which OSM tags with street_vendor=yes rather than with an amenity or shop value of their own. Bars, pubs and beer
// gardens are included because they are commonly recommended for their food too. A place may have no category.
var PlaceCategories = []string{
	"restaurant", "fast_food", "cafe", "food_court", "ice_cream", "bakery", "pastry", "street_vendor", "bar", "pub",
	"biergarten",
}

// Cuisines are the values places.cuisine accepts (several per place, M2-Q5): common values of OpenStreetMap's
// cuisine=* tag.
var Cuisines = []string{
	"chinese", "sichuan", "cantonese", "hunan", "dongbei", "uyghur", "taiwanese", "hot_pot", "dim_sum", "dumpling",
	"noodle", "barbecue", "japanese", "sushi", "ramen", "korean", "vietnamese", "thai", "indian", "asian", "german",
	"italian", "pizza", "french", "spanish", "greek", "turkish", "kebab", "balkan", "middle_eastern", "mexican",
	"american", "burger", "sandwich", "seafood", "steak_house", "coffee_shop", "bubble_tea",
}

// TimeSlots are the times of day a place is worth going (recommendations.time_slots, A3).
var TimeSlots = []string{"breakfast", "lunch", "afternoon", "dinner", "late_night"}

// SourceKinds are the kinds of a recommendation's source (C2): the author went there, it was told in the group chat,
// it comes from a note, or something else.
var SourceKinds = []string{"visited", "chat", "note", "other"}

// WritableVisibilities are the visibilities a member may give a recommendation (C4). [VisibilityFederated] is
// reserved for federation and not accepted yet.
var WritableVisibilities = []string{VisibilityPrivate, VisibilityCircle}

// Signatures are the ways a recommendation can be signed (C3), [SignAsMain] by default.
var Signatures = []string{SignAsMain, SignAsPseudonym}

// Coordinate systems and origins of a place's coordinates as entered (places.source_coord, B12). M2 has only WGS-84:
// both the map's crosshair and the place search give WGS-84 coordinates (other systems come with converting shared
// links, B10).
const (
	CRSWGS84 = "wgs84"

	CoordFromCrosshair = "crosshair" // the member placed the map's crosshair
	CoordFromSearch    = "search"    // a place search result, not moved afterwards
)

// CoordSources lists the origins of entered coordinates.
var CoordSources = []string{CoordFromCrosshair, CoordFromSearch}

// Landmark kinds (places.landmark, B4): another place of the instance, or a text.
const (
	LandmarkPlace = "place"
	LandmarkText  = "text"
)

// Limits of places and recommendations, in characters (runes) unless said otherwise.
const (
	MaxPlaceNameLength    = 200
	MaxAliases            = 10
	MaxPositionNoteLength = 500
	MaxLandmarkTextLength = 200
	MaxTextLength         = 5000
	MaxDishes             = 20
	MaxDishLength         = 100
	MaxSeasonLength       = 100
	MaxSources            = 10
	MaxSourceURLLength    = 2000 // bytes
	MaxExcerptLength      = 500  // only a short excerpt of someone else's words is kept
)

// Errors of the validation. A validation error is a *[FieldError]; errors.Is finds these in it.
var (
	// ErrOutsideCities: the coordinates are in none of the instance's cities (H2).
	ErrOutsideCities = errors.New("domain: the coordinates are outside the instance's cities")
	// ErrNoPseudonym: the recommendation is to be signed with a pseudonym, and the member has none.
	ErrNoPseudonym = errors.New("domain: the member has no pseudonym")
	// ErrInvalid: the field has a value it may not have.
	ErrInvalid = errors.New("domain: invalid value")
)

// FieldError says which field of a draft is not valid, by its name in the API ("name", "sources[2].url").
type FieldError struct {
	Field string
	Err   error
}

func (e *FieldError) Error() string { return fmt.Sprintf("%s: %v", e.Field, e.Err) }

// Unwrap returns the reason: [ErrInvalid], [ErrOutsideCities] or [ErrNoPseudonym].
func (e *FieldError) Unwrap() error { return e.Err }

func invalid(field string) error { return &FieldError{Field: field, Err: ErrInvalid} }

// Bounds is a city's bounding box: minLon, minLat, maxLon, maxLat in WGS-84 degrees.
type Bounds [4]float64

// Contains reports whether the point is inside b, its edges included.
func (b Bounds) Contains(lat, lon float64) bool {
	return lon >= b[0] && lon <= b[2] && lat >= b[1] && lat <= b[3]
}

// Landmark is what a place's position is described relative to (B4): another place, by uuid, or a text.
type Landmark struct {
	Type      string // LandmarkPlace or LandmarkText
	PlaceUUID string // for LandmarkPlace
	Text      string // for LandmarkText
}

// SourceCoord is how a place's coordinates were entered (B12): the coordinates before rounding, their coordinate
// system and their origin.
type SourceCoord struct {
	Lat, Lon float64
	CRS      string // CRSWGS84
	Source   string // one of CoordSources
}

// PlaceDraft is a new place as a member enters it: everything of a place but its uuid.
type PlaceDraft struct {
	Name         string
	Aliases      []string
	Lat, Lon     float64
	Precision    string
	PositionNote string
	Landmark     *Landmark
	Category     string
	Cuisine      []string
	// CRS and CoordSource describe the coordinates (see [SourceCoord]); empty means WGS-84 from the crosshair.
	CRS, CoordSource string
}

// NormalizePlace checks a new place and returns it as it is stored, with its [SourceCoord]. Texts are trimmed; the
// name has 1 to [MaxPlaceNameLength] characters on one line; there are at most [MaxAliases] aliases (empty ones and
// repeats are dropped) of the same form; the coordinates are inside one of cities (else [ErrOutsideCities]); the
// precision is one of [Precisions] ([PrecisionExact] when empty); the category is empty or one of [PlaceCategories];
// the cuisines are from [Cuisines] (repeats dropped, in the order of that list); the position note has at most
// [MaxPositionNoteLength] characters and may span lines; a landmark is a place uuid (whether the caller may see it is
// checked against the database, by the caller) or a text of 1 to [MaxLandmarkTextLength] characters on one line.
//
// The coordinates are returned as given: the store rounds them to 5 decimals, and the SourceCoord keeps them unrounded.
// isUUID tells whether a string is a uuid (the store's format, which this package does not know).
func NormalizePlace(d PlaceDraft, cities []Bounds, isUUID func(string) bool) (PlaceDraft, SourceCoord, error) {
	var err error
	if d.Name, err = oneLine(d.Name, MaxPlaceNameLength, true); err != nil {
		return PlaceDraft{}, SourceCoord{}, invalid("name")
	}
	if d.Aliases, err = textList(d.Aliases, MaxAliases, MaxPlaceNameLength, "aliases"); err != nil {
		return PlaceDraft{}, SourceCoord{}, err
	}
	if !ValidCoordinates(d.Lat, d.Lon) {
		return PlaceDraft{}, SourceCoord{}, invalid("lat")
	}
	if !slices.ContainsFunc(cities, func(b Bounds) bool { return b.Contains(d.Lat, d.Lon) }) {
		return PlaceDraft{}, SourceCoord{}, &FieldError{Field: "lat", Err: ErrOutsideCities}
	}
	if d.Precision == "" {
		d.Precision = PrecisionExact
	}
	if !slices.Contains(Precisions, d.Precision) {
		return PlaceDraft{}, SourceCoord{}, invalid("precision")
	}
	if d.PositionNote, err = multiLine(d.PositionNote, MaxPositionNoteLength, false); err != nil {
		return PlaceDraft{}, SourceCoord{}, invalid("positionNote")
	}
	if d.Landmark != nil {
		l := *d.Landmark
		switch l.Type {
		case LandmarkPlace:
			if !isUUID(l.PlaceUUID) {
				return PlaceDraft{}, SourceCoord{}, invalid("landmark.place")
			}
			l.Text = ""
		case LandmarkText:
			if l.Text, err = oneLine(l.Text, MaxLandmarkTextLength, true); err != nil {
				return PlaceDraft{}, SourceCoord{}, invalid("landmark.text")
			}
			l.PlaceUUID = ""
		default:
			return PlaceDraft{}, SourceCoord{}, invalid("landmark.type")
		}
		d.Landmark = &l
	}
	if d.Category != "" && !slices.Contains(PlaceCategories, d.Category) {
		return PlaceDraft{}, SourceCoord{}, invalid("category")
	}
	if d.Cuisine, err = fromList(d.Cuisine, Cuisines, "cuisine"); err != nil {
		return PlaceDraft{}, SourceCoord{}, err
	}
	if d.CRS == "" {
		d.CRS = CRSWGS84
	}
	if d.CRS != CRSWGS84 {
		return PlaceDraft{}, SourceCoord{}, invalid("sourceCoord.crs")
	}
	if d.CoordSource == "" {
		d.CoordSource = CoordFromCrosshair
	}
	if !slices.Contains(CoordSources, d.CoordSource) {
		return PlaceDraft{}, SourceCoord{}, invalid("sourceCoord.source")
	}
	return d, SourceCoord{Lat: d.Lat, Lon: d.Lon, CRS: d.CRS, Source: d.CoordSource}, nil
}

// ValidCoordinates reports whether lat and lon are finite WGS-84 degrees.
func ValidCoordinates(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// Source is one source of a recommendation (C2): an optional http(s) link, its kind and an optional short excerpt.
type Source struct {
	URL     string
	Kind    string
	Excerpt string
}

// RecommendationDraft is a recommendation as a member writes it.
type RecommendationDraft struct {
	Attitude  string
	Text      string
	Dishes    []string
	TimeSlots []string
	Season    string
	Sources   []Source
	// VisitedOn is the date of the last visit, YYYY-MM-DD, or empty.
	VisitedOn  string
	DisplayAs  string
	Visibility string
}

// NormalizeRecommendation checks a recommendation and returns it as it is stored. The attitude is one of [Attitudes];
// the text, trimmed, is not empty and has at most [MaxTextLength] characters (it may span lines); at most [MaxDishes]
// dishes of at most [MaxDishLength] characters (empty ones and repeats dropped); time slots from [TimeSlots] (repeats
// dropped, in the order of that list); a season of at most [MaxSeasonLength] characters; at most [MaxSources] sources,
// each of one of [SourceKinds], with an optional absolute http(s) link and an optional excerpt of at most
// [MaxExcerptLength] characters; a visit date, if any, that is a date and not later than today anywhere on Earth
// (now, seen from the latest time zone, UTC+14); the signature one of [Signatures] ([SignAsMain] when empty), and
// [SignAsPseudonym] only when the member has a pseudonym (else [ErrNoPseudonym]); the visibility one of
// [WritableVisibilities] ([VisibilityCircle] when empty).
func NormalizeRecommendation(d RecommendationDraft, hasPseudonym bool, now time.Time) (RecommendationDraft, error) {
	var err error
	if !slices.Contains(Attitudes, d.Attitude) {
		return RecommendationDraft{}, invalid("attitude")
	}
	if d.Text, err = multiLine(d.Text, MaxTextLength, true); err != nil {
		return RecommendationDraft{}, invalid("text")
	}
	if d.Dishes, err = textList(d.Dishes, MaxDishes, MaxDishLength, "dishes"); err != nil {
		return RecommendationDraft{}, err
	}
	if d.TimeSlots, err = fromList(d.TimeSlots, TimeSlots, "timeSlots"); err != nil {
		return RecommendationDraft{}, err
	}
	if d.Season, err = oneLine(d.Season, MaxSeasonLength, false); err != nil {
		return RecommendationDraft{}, invalid("season")
	}
	if len(d.Sources) > MaxSources {
		return RecommendationDraft{}, invalid("sources")
	}
	sources := make([]Source, 0, len(d.Sources))
	for i, s := range d.Sources {
		field := fmt.Sprintf("sources[%d]", i)
		if !slices.Contains(SourceKinds, s.Kind) {
			return RecommendationDraft{}, invalid(field + ".kind")
		}
		if s.URL = strings.TrimSpace(s.URL); s.URL != "" && !webURL(s.URL) {
			return RecommendationDraft{}, invalid(field + ".url")
		}
		if s.Excerpt, err = multiLine(s.Excerpt, MaxExcerptLength, false); err != nil {
			return RecommendationDraft{}, invalid(field + ".excerpt")
		}
		sources = append(sources, s)
	}
	d.Sources = sources
	if d.VisitedOn = strings.TrimSpace(d.VisitedOn); d.VisitedOn != "" {
		day, err := time.Parse(time.DateOnly, d.VisitedOn)
		latestToday := now.In(time.FixedZone("UTC+14", 14*60*60)).Format(time.DateOnly)
		if err != nil || day.Year() < 1900 || d.VisitedOn > latestToday {
			return RecommendationDraft{}, invalid("visitedOn")
		}
	}
	if d.DisplayAs == "" {
		d.DisplayAs = SignAsMain
	}
	if !slices.Contains(Signatures, d.DisplayAs) {
		return RecommendationDraft{}, invalid("displayAs")
	}
	if d.DisplayAs == SignAsPseudonym && !hasPseudonym {
		return RecommendationDraft{}, &FieldError{Field: "displayAs", Err: ErrNoPseudonym}
	}
	if d.Visibility == "" {
		d.Visibility = VisibilityCircle
	}
	if !slices.Contains(WritableVisibilities, d.Visibility) {
		return RecommendationDraft{}, invalid("visibility")
	}
	return d, nil
}

// webURL reports whether s is an absolute http or https URL with a host and without credentials, of at most
// [MaxSourceURLLength] bytes.
func webURL(s string) bool {
	if len(s) > MaxSourceURLLength {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

// oneLine trims s and checks that it has at most maxLen characters, no control characters (no line breaks) and, when
// required, is not empty.
func oneLine(s string, maxLen int, required bool) (string, error) {
	s = strings.TrimSpace(s)
	if err := checkText(s, maxLen, required); err != nil {
		return "", err
	}
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", ErrInvalid
	}
	return s, nil
}

// multiLine trims s, makes its line breaks \n and checks that it has at most maxLen characters, no control characters
// other than line breaks and tabs and, when required, is not empty.
func multiLine(s string, maxLen int, required bool) (string, error) {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"))
	if err := checkText(s, maxLen, required); err != nil {
		return "", err
	}
	if strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
		return "", ErrInvalid
	}
	return s, nil
}

func checkText(s string, maxLen int, required bool) error {
	if (required && s == "") || !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxLen {
		return ErrInvalid
	}
	return nil
}

// textList trims each entry, drops empty ones and repeats, and checks that at most maxItems remain, each a line of at
// most maxLen characters.
func textList(items []string, maxItems, maxLen int, field string) ([]string, error) {
	out := []string{}
	for i, item := range items {
		s, err := oneLine(item, maxLen, false)
		if err != nil {
			return nil, invalid(fmt.Sprintf("%s[%d]", field, i))
		}
		if s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	if len(out) > maxItems {
		return nil, invalid(field)
	}
	return out, nil
}

// fromList checks that every value is one of allowed and returns them without repeats, in the order of allowed.
func fromList(values, allowed []string, field string) ([]string, error) {
	for i, v := range values {
		if !slices.Contains(allowed, v) {
			return nil, invalid(fmt.Sprintf("%s[%d]", field, i))
		}
	}
	out := []string{}
	for _, a := range allowed {
		if slices.Contains(values, a) {
			out = append(out, a)
		}
	}
	return out, nil
}
