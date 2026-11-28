package export

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// The per-layer GeoJSON exports (F1, H5, T041). [WritePlaces] and [WriteRecommendations] write the circle's two layers
// as RFC 7946 FeatureCollections, one Point feature per place or recommendation, with the properties under the
// "osfm:" prefix plus uMap's name and description, and the format version, licence and attribution as foreign members
// of the collection (docs/data-format.md).
//
// What goes in is the circle's layer (store.InCircleLayer, M2-Q3): the recommendations every member sees, whoever
// they are (shared, not hidden, not deleted, of a live place), and the places with at least one of them. The files are
// therefore the same for every member who asks, maintainers included, and hold nothing a member could not see on the
// map. Never in them: member uuids or internal IDs, authors and editors, how a recommendation is signed, external
// links, private or hidden recommendations, tombstones (M2 exports no deletions; they are kept for federation).
//
// There is no HTTP here: internal/api serves the files, and backups (T042) write the same layers into their archive.
// Both stream: the records are read in batches of batchSize in uuid order, so an export never holds a whole layer's
// records at once.

// Format versions: the value of the collection's "osfm:schema". An incompatible change of a layer's format gets a new
// number; adding a property does not.
const (
	PlacesSchema          = "osfm-places/1"
	RecommendationsSchema = "osfm-recommendations/1"
)

// LeftMemberName is osfm:display_name of a main-name recommendation whose author has left the circle (M2-Q10). The
// API leaves the name to the client's language; a file needs one, and the instance's language is Chinese.
const LeftMemberName = "已退出成员"

// License is a licence named in an export: an SPDX identifier, its name and where to read it.
type License struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// The licences of the layers (H5): the places layer is a database under the ODbL, as OpenStreetMap is; the
// recommendations are texts of their authors under CC BY-SA.
var (
	ODbL = License{ID: "ODbL-1.0", Name: "Open Database License (ODbL) v1.0",
		URL: "https://opendatacommons.org/licenses/odbl/1-0/"}
	CCBYSA = License{ID: "CC-BY-SA-4.0", Name: "Creative Commons Attribution-ShareAlike 4.0 International",
		URL: "https://creativecommons.org/licenses/by-sa/4.0/"}
)

// OSMAttribution is the attribution OpenStreetMap asks for, with the page it links to: the positions were placed on
// OpenStreetMap's map and found with its data.
const (
	OSMAttribution    = "© OpenStreetMap contributors"
	OSMAttributionURL = "https://www.openstreetmap.org/copyright"
)

// Notices: the licence and attribution in words, as "osfm:notice".
const (
	placesNotice = "The places layer of an OSFM circle, made available under the Open Database License (ODbL) v1.0. " +
		"Positions are placed on OpenStreetMap: " + OSMAttribution + "."
	recommendationsNotice = "The recommendations layer of an OSFM circle: the texts are licensed under Creative " +
		"Commons Attribution-ShareAlike 4.0 International (CC BY-SA 4.0), attributed to the names in " +
		"osfm:display_name. The positions are those of the places layer (ODbL v1.0, " + OSMAttribution + ")."
)

// batchSize is the number of records an export reads at a time (a variable for the tests).
var batchSize int64 = 500

// BBox is minLon, minLat, maxLon, maxLat in WGS-84 degrees.
type BBox [4]float64

// Options says what to export. The zero value exports the whole layer.
type Options struct {
	// BBox, when set, keeps only the places inside it (edges included), and the recommendations of those places.
	BBox *BBox
	// City is the slug of the city BBox belongs to, written as "osfm:city"; "" writes none.
	City string
	// Now is the time written as "osfm:exported"; the zero value means the current time.
	Now time.Time
}

// Collection is the part of an exported FeatureCollection other than its features: the GeoJSON type and the foreign
// members that say what the file is and under which terms it may be used.
type Collection struct {
	Type   string `json:"type"` // "FeatureCollection"
	Schema string `json:"osfm:schema"`
	// License is the licence of the layer; Attribution and AttributionURL the attribution it requires.
	License        License `json:"osfm:license"`
	Attribution    string  `json:"osfm:attribution"`
	AttributionURL string  `json:"osfm:attribution_url"`
	// Notice says the same in words.
	Notice   string    `json:"osfm:notice"`
	Exported time.Time `json:"osfm:exported"`
	// City is the slug of the city the export is limited to; absent for the whole layer.
	City string `json:"osfm:city,omitempty"`
}

// Point is a GeoJSON Point: coordinates are [longitude, latitude] (RFC 7946 §3.1.1).
type Point struct {
	Type        string     `json:"type"` // "Point"
	Coordinates [2]float64 `json:"coordinates"`
}

// Feature is a GeoJSON Feature whose id is the uuid of its place or recommendation.
type Feature[P any] struct {
	Type       string `json:"type"` // "Feature"
	ID         string `json:"id"`
	Geometry   Point  `json:"geometry"`
	Properties P      `json:"properties"`
}

// PlaceProperties are the properties of a feature of the places layer. Nothing in them is about a member.
type PlaceProperties struct {
	// Name and Description are for uMap and other viewers: the place's name, and its aliases, position note and
	// landmark in plain text, one per line.
	Name        string `json:"name"`
	Description string `json:"description"`

	UUID    string   `json:"osfm:uuid"`
	PlName  string   `json:"osfm:name"`
	Aliases []string `json:"osfm:aliases"`
	// Precision is exact, approx or area (H3).
	Precision    string `json:"osfm:precision"`
	PositionNote string `json:"osfm:position_note"`
	// Landmark is null without one, and for a landmark place that is not in the layer.
	Landmark *Landmark `json:"osfm:landmark"`
	// Category is null for a place without one.
	Category *string  `json:"osfm:category"`
	Cuisine  []string `json:"osfm:cuisine"`
	// Status is open, maybe_closed or closed; null when nobody confirmed one.
	Status  *string   `json:"osfm:status"`
	Created time.Time `json:"osfm:created"`
	Updated time.Time `json:"osfm:updated"`
}

// Kinds of [Landmark].
const (
	LandmarkPlace = "place"
	LandmarkText  = "text"
)

// Landmark is what a place's position is described by (B4): another place of the layer, by uuid and name, or a text.
type Landmark struct {
	Type  string `json:"type"`
	Place string `json:"place,omitempty"`
	Name  string `json:"name,omitempty"`
	Text  string `json:"text,omitempty"`
}

// RecommendationProperties are the properties of a feature of the recommendations layer. The signature is the name
// readers see; who wrote or edited the recommendation, and whether the name is a main name or a pseudonym, are not
// in it (E2).
type RecommendationProperties struct {
	// Name and Description are for uMap and other viewers: the place's name with the signature, and the text.
	Name        string `json:"name"`
	Description string `json:"description"`

	UUID string `json:"osfm:uuid"`
	// Place is the uuid of the recommendation's place, a feature of the places layer.
	Place string `json:"osfm:place"`
	// DisplayName is the signature: the author's main name or pseudonym, or [LeftMemberName] for a main-name
	// recommendation of a member who left (AuthorLeft).
	DisplayName string `json:"osfm:display_name"`
	AuthorLeft  bool   `json:"osfm:author_left"`
	// Attitude is must_try, good, ok or avoid.
	Attitude string   `json:"osfm:attitude"`
	Text     string   `json:"osfm:text"`
	Dishes   []string `json:"osfm:dishes"`
	// TimeSlots are some of breakfast, lunch, afternoon, dinner, late_night.
	TimeSlots []string `json:"osfm:time_slots"`
	Season    string   `json:"osfm:season"`
	Sources   []Source `json:"osfm:sources"`
	// VisitedOn is the date of the last visit, YYYY-MM-DD; null when not given.
	VisitedOn *string   `json:"osfm:visited_on"`
	Created   time.Time `json:"osfm:created"`
	Updated   time.Time `json:"osfm:updated"`
	// EditedByMaintainerAt is when a maintainer last edited it (C5); null when none did. Which maintainer is not said.
	EditedByMaintainerAt *time.Time `json:"osfm:edited_by_maintainer_at"`
}

// Source is where a recommendation's knowledge comes from: a link ("" for none), its kind (visited, chat, note,
// other) and a short excerpt ("" for none).
type Source struct {
	URL     string `json:"url"`
	Kind    string `json:"kind"`
	Excerpt string `json:"excerpt"`
}

// PlacesCollection is a whole places layer, for reading an export back (tests, a restore).
type PlacesCollection struct {
	Collection
	Features []Feature[PlaceProperties] `json:"features"`
}

// RecommendationsCollection is a whole recommendations layer, for reading an export back.
type RecommendationsCollection struct {
	Collection
	Features []Feature[RecommendationProperties] `json:"features"`
}

// WritePlaces writes the places layer to w: the live places with at least one recommendation of the circle's layer
// (store.CirclePlaces), in uuid order. On an error, what was written so far is not a complete file.
func WritePlaces(w io.Writer, app core.App, opts Options) error {
	header := collection(PlacesSchema, ODbL, placesNotice, opts)
	return writeLayer(w, header, func(after string) ([]any, string, error) {
		places, err := store.CirclePlaces(app).Where(inBox(opts.BBox, store.Places)).
			Where(dbx.NewExp("[["+store.Places+"."+store.FieldUUID+"]] > {:after}", dbx.Params{"after": after})).
			OrderBy(store.Places + "." + store.FieldUUID).Limit(batchSize).All()
		if err != nil || len(places) == 0 {
			return nil, "", err
		}
		landmarks, err := landmarkNames(app, places)
		if err != nil {
			return nil, "", err
		}
		out := make([]any, len(places))
		for i, p := range places {
			out[i] = placeFeature(p, landmarks)
		}
		return out, places[len(places)-1].GetString(store.FieldUUID), nil
	})
}

// WriteRecommendations writes the recommendations layer to w: the recommendations of the circle's layer
// (store.CircleRecommendations), at most one per author and place (the newest, as the map counts them), in uuid
// order. On an error, what was written so far is not a complete file.
func WriteRecommendations(w io.Writer, app core.App, opts Options) error {
	header := collection(RecommendationsSchema, CCBYSA, recommendationsNotice, opts)
	const table = store.Recommendations
	var inPlaceBox dbx.Expression
	if opts.BBox != nil {
		inPlaceBox = existsExp{"SELECT 1 FROM {{" + store.Places + "}} AS [[bp]] WHERE [[bp.id]] = [[" + table +
			".place]] AND ", inBox(opts.BBox, "bp")}
	}
	return writeLayer(w, header, func(after string) ([]any, string, error) {
		recs, err := store.CircleRecommendations(app).Where(inPlaceBox).Where(newestOfAuthor(table)).
			Where(dbx.NewExp("[["+table+"."+store.FieldUUID+"]] > {:after}", dbx.Params{"after": after})).
			OrderBy(table + "." + store.FieldUUID).Limit(batchSize).All()
		if err != nil || len(recs) == 0 {
			return nil, "", err
		}
		places, authors, err := placesAndAuthors(app, recs)
		if err != nil {
			return nil, "", err
		}
		out := make([]any, 0, len(recs))
		for _, r := range recs {
			place, ok := places[r.GetString("place")]
			if !ok { // its place was deleted between the queries
				continue
			}
			author, ok := authors[r.GetString("author")]
			if !ok {
				author = domain.Author{Left: true} // no such member: never guess a name
			}
			out = append(out, recommendationFeature(r, place, author))
		}
		return out, recs[len(recs)-1].GetString(store.FieldUUID), nil
	})
}

func collection(schema string, license License, notice string, opts Options) Collection {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	return Collection{Type: "FeatureCollection", Schema: schema, License: license, Attribution: OSMAttribution,
		AttributionURL: OSMAttributionURL, Notice: notice, Exported: now.UTC().Truncate(time.Second), City: opts.City}
}

// writeLayer writes the collection header, then the features next returns batch by batch (next gets the uuid of the
// last record of the previous batch, "" at first, and returns that of its own last record), until it returns none.
func writeLayer(w io.Writer, header Collection, next func(after string) ([]any, string, error)) error {
	head, err := json.Marshal(header)
	if err != nil {
		return err
	}
	ew := &errWriter{w: w}
	ew.write(head[:len(head)-1]) // without the closing brace
	ew.write([]byte(`,"features":[`))
	first := true
	after := ""
	for ew.err == nil {
		features, last, err := next(after)
		if err != nil {
			return err
		}
		if last == "" {
			break
		}
		for _, f := range features {
			b, err := json.Marshal(f)
			if err != nil {
				return err
			}
			if !first {
				ew.write([]byte{','})
			}
			first = false
			ew.write([]byte{'\n'})
			ew.write(b)
		}
		after = last
	}
	if !first {
		ew.write([]byte{'\n'})
	}
	ew.write([]byte("]}\n"))
	return ew.err
}

type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) write(b []byte) {
	if e.err == nil {
		_, e.err = e.w.Write(b)
	}
}

// inBox returns the condition that the place under table lies in box (nil: no condition).
func inBox(box *BBox, table string) dbx.Expression {
	if box == nil {
		return nil
	}
	return dbx.And(dbx.Between(table+".lon", box[0], box[2]), dbx.Between(table+".lat", box[1], box[3]))
}

// newestOfAuthor returns the condition that no newer recommendation of the circle's layer by the same author at the
// same place exists. M2-Q1 allows one live recommendation per member and place already (a unique index); this is the
// map's second line of defence (internal/api countedOn) for the file: a main-name and a pseudonym recommendation of
// one member at one place would tell that the two names are one person.
func newestOfAuthor(table string) dbx.Expression {
	const n = "rn"
	return notExistsExp{"SELECT 1 FROM {{" + store.Recommendations + "}} AS [[" + n + "]] WHERE ", dbx.And(
		dbx.NewExp("[["+n+".place]] = [["+table+".place]] AND [["+n+".author]] = [["+table+".author]]"),
		dbx.NewExp("([["+n+".created]] > [["+table+".created]] OR ([["+n+".created]] = [["+table+".created]] AND [["+
			n+".uuid]] > [["+table+".uuid]]))"),
		store.InCircleLayer(n),
	)}
}

// existsExp is EXISTS (prefix cond).
type existsExp struct {
	prefix string
	cond   dbx.Expression
}

// Build implements dbx.Expression.
func (e existsExp) Build(db *dbx.DB, params dbx.Params) string {
	return "EXISTS (" + e.prefix + e.cond.Build(db, params) + ")"
}

// notExistsExp is NOT EXISTS (prefix cond).
type notExistsExp existsExp

// Build implements dbx.Expression.
func (e notExistsExp) Build(db *dbx.DB, params dbx.Params) string {
	return "NOT " + existsExp(e).Build(db, params)
}

// storedLandmark is places.landmark as stored: {type: place, place_uuid} or {type: text, text}; "place" is read too
// when place_uuid is empty (as internal/api reads it).
type storedLandmark struct {
	Type      string `json:"type"`
	PlaceUUID string `json:"place_uuid"`
	Place     string `json:"place"`
	Text      string `json:"text"`
}

func landmarkOf(p *core.Record) storedLandmark {
	var l storedLandmark
	if err := p.UnmarshalJSONField("landmark", &l); err != nil {
		return storedLandmark{}
	}
	if l.PlaceUUID == "" {
		l.PlaceUUID = l.Place
	}
	return l
}

// landmarkNames returns the names of the landmark places of places that are in the places layer, by uuid. A landmark
// place outside the layer (only private or hidden recommendations, deleted) is left out, as if there were none:
// naming it would tell that it exists.
func landmarkNames(app core.App, places []*core.Record) (map[string]string, error) {
	ids := []any{}
	for _, p := range places {
		if l := landmarkOf(p); l.Type == LandmarkPlace && store.IsUUID(l.PlaceUUID) {
			ids = append(ids, l.PlaceUUID)
		}
	}
	names := map[string]string{}
	if len(ids) == 0 {
		return names, nil
	}
	found, err := store.CirclePlaces(app).Where(dbx.In(store.Places+"."+store.FieldUUID, ids...)).All()
	if err != nil {
		return nil, err
	}
	for _, p := range found {
		names[p.GetString(store.FieldUUID)] = p.GetString("name")
	}
	return names, nil
}

func placeFeature(p *core.Record, landmarks map[string]string) Feature[PlaceProperties] {
	id := p.GetString(store.FieldUUID)
	props := PlaceProperties{
		UUID:         id,
		PlName:       p.GetString("name"),
		Aliases:      stringList(p, "aliases"),
		Precision:    p.GetString("precision"),
		PositionNote: p.GetString("position_note"),
		Category:     optional(p.GetString("category")),
		Cuisine:      stringList(p, "cuisine"),
		Status:       optional(p.GetString("status")),
		Created:      p.GetDateTime(store.FieldCreated).Time().UTC(),
		Updated:      p.GetDateTime(store.FieldUpdated).Time().UTC(),
	}
	switch l := landmarkOf(p); l.Type {
	case LandmarkText:
		if text := strings.TrimSpace(l.Text); text != "" {
			props.Landmark = &Landmark{Type: LandmarkText, Text: text}
		}
	case LandmarkPlace:
		if name, ok := landmarks[l.PlaceUUID]; ok && l.PlaceUUID != id {
			props.Landmark = &Landmark{Type: LandmarkPlace, Place: l.PlaceUUID, Name: name}
		}
	}
	lines := []string{}
	if len(props.Aliases) > 0 {
		lines = append(lines, strings.Join(props.Aliases, ", "))
	}
	if props.PositionNote != "" {
		lines = append(lines, props.PositionNote)
	}
	switch {
	case props.Landmark == nil:
	case props.Landmark.Type == LandmarkText:
		lines = append(lines, props.Landmark.Text)
	default:
		lines = append(lines, props.Landmark.Name)
	}
	props.Name, props.Description = display(props.PlName), display(strings.Join(lines, "\n"))
	return Feature[PlaceProperties]{Type: "Feature", ID: id, Geometry: point(p), Properties: props}
}

// placesAndAuthors loads the places and the authors of recs (deleted authors too: their main-name recommendations
// are signed as by a member who left), by internal ID.
func placesAndAuthors(app core.App, recs []*core.Record) (map[string]*core.Record, map[string]domain.Author, error) {
	placeIDs, authorIDs := []any{}, []any{}
	for _, r := range recs {
		placeIDs = append(placeIDs, r.GetString("place"))
		authorIDs = append(authorIDs, r.GetString("author"))
	}
	places := map[string]*core.Record{}
	found, err := store.Query(app, store.Places).Where(dbx.In(store.Places+".id", placeIDs...)).All()
	if err != nil {
		return nil, nil, err
	}
	for _, p := range found {
		places[p.Id] = p
	}
	authors := map[string]domain.Author{}
	members, err := store.Query(app, store.Members).IncludeDeleted().
		Where(dbx.In(store.Members+".id", authorIDs...)).All()
	if err != nil {
		return nil, nil, err
	}
	for _, m := range members {
		authors[m.Id] = store.AuthorOf(m)
	}
	return places, authors, nil
}

func recommendationFeature(r, place *core.Record, author domain.Author) Feature[RecommendationProperties] {
	id := r.GetString(store.FieldUUID)
	sig := domain.ResolveSignature(r.GetString("display_as"), author)
	name := sig.DisplayName
	if sig.AuthorLeft {
		name = LeftMemberName
	}
	props := RecommendationProperties{
		UUID:        id,
		Place:       place.GetString(store.FieldUUID),
		DisplayName: name,
		AuthorLeft:  sig.AuthorLeft,
		Attitude:    r.GetString("attitude"),
		Text:        r.GetString("text"),
		Dishes:      stringList(r, "dishes"),
		TimeSlots:   r.GetStringSlice("time_slots"),
		Season:      r.GetString("season"),
		Sources:     sources(r),
		Created:     r.GetDateTime(store.FieldCreated).Time().UTC(),
		Updated:     r.GetDateTime(store.FieldUpdated).Time().UTC(),
	}
	if props.TimeSlots == nil {
		props.TimeSlots = []string{}
	}
	if d := r.GetDateTime("visited_on"); !d.IsZero() {
		props.VisitedOn = optional(d.Time().UTC().Format(time.DateOnly))
	}
	if d := r.GetDateTime("edited_by_maintainer_at"); !d.IsZero() {
		t := d.Time().UTC()
		props.EditedByMaintainerAt = &t
	}
	title := place.GetString("name")
	if name != "" {
		title += " · " + name
	}
	props.Name, props.Description = display(title), display(props.Text)
	return Feature[RecommendationProperties]{Type: "Feature", ID: id, Geometry: point(place), Properties: props}
}

// displayMarkup replaces the characters that uMap reads as markup in name and description, by their full-width forms:
// uMap renders a description as HTML through its own formatter, so "<img …>" or "{{https://…}}" in a member's text
// would make every viewer of the map fetch that URL, and "{field}" would be replaced by another property. The osfm:
// properties carry the text unchanged.
var displayMarkup = strings.NewReplacer("<", "＜", ">", "＞", "{", "｛", "}", "｝")

// display returns s for the display properties name and description: plain text that uMap shows as text.
func display(s string) string { return displayMarkup.Replace(s) }

func point(place *core.Record) Point {
	return Point{Type: "Point", Coordinates: [2]float64{place.GetFloat("lon"), place.GetFloat("lat")}}
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// stringList returns the JSON field of r as a list of strings without empty ones; empty when it is not one.
func stringList(r *core.Record, field string) []string {
	var raw []string
	_ = r.UnmarshalJSONField(field, &raw)
	out := []string{}
	for _, s := range raw {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// sources returns the sources of r; empty when the field holds anything else.
func sources(r *core.Record) []Source {
	var raw []Source
	_ = r.UnmarshalJSONField("sources", &raw)
	if raw == nil {
		return []Source{}
	}
	return raw
}
