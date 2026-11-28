package main

import (
	"bytes"
	"encoding/json"
	"maps"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/export"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// Integration tests of the per-layer GeoJSON exports (T041), as the four identities. The seeded city is
// testapp.CitySlug with the bounding box 7.02,0.31,7.1,0.37.

// The property keys each layer has, exactly: anything else (an author, an editor, a signature kind) is a change of
// the format that has to be decided, documented in docs/data-format.md and added here.
var (
	placeKeys = []string{"name", "description", "osfm:uuid", "osfm:name", "osfm:aliases", "osfm:precision",
		"osfm:position_note", "osfm:landmark", "osfm:category", "osfm:cuisine", "osfm:status", "osfm:created",
		"osfm:updated"}
	recommendationKeys = []string{"name", "description", "osfm:uuid", "osfm:place", "osfm:display_name",
		"osfm:author_left", "osfm:attitude", "osfm:text", "osfm:dishes", "osfm:time_slots", "osfm:season",
		"osfm:sources", "osfm:visited_on", "osfm:created", "osfm:updated", "osfm:edited_by_maintainer_at"}
	collectionKeys = []string{"type", "features", "osfm:schema", "osfm:license", "osfm:attribution",
		"osfm:attribution_url", "osfm:notice", "osfm:exported"}
)

// exportOf downloads a layer as id and checks what every successful download has: the headers of a file to
// download, and a valid RFC 7946 FeatureCollection of Points (checkGeoJSON).
func exportOf(t *testing.T, a *testapp.App, id testapp.Identity, path string) *testapp.Response {
	t.Helper()
	r := a.As(id).Get(path).Expect(http.StatusOK)
	h := r.Header()
	if ct := h.Get("Content-Type"); ct != api.ExportContentType {
		t.Errorf("%s: Content-Type %q", path, ct)
	}
	if cc := h.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("%s: Cache-Control %q", path, cc)
	}
	cd := h.Get("Content-Disposition")
	if !strings.HasPrefix(cd, `attachment; filename="osfm-`) || !strings.HasSuffix(cd, `.geojson"`) {
		t.Errorf("%s: Content-Disposition %q", path, cd)
	}
	checkGeoJSON(t, path, r.Body.Bytes())
	return r
}

// checkGeoJSON checks body against RFC 7946 as far as these files use it: a FeatureCollection whose features are
// Features with a string id, a Point geometry of two finite numbers, longitude first, in range, and an object of
// properties; no "crs" (§4) and no "bbox" that could disagree.
func checkGeoJSON(t *testing.T, name string, body []byte) {
	t.Helper()
	if !json.Valid(body) {
		t.Fatalf("%s: not JSON", name)
	}
	var fc map[string]json.RawMessage
	if err := json.Unmarshal(body, &fc); err != nil {
		t.Fatalf("%s: not an object: %v", name, err)
	}
	if string(fc["type"]) != `"FeatureCollection"` {
		t.Errorf("%s: type %s", name, fc["type"])
	}
	if fc["crs"] != nil || fc["bbox"] != nil {
		t.Errorf("%s: crs or bbox", name)
	}
	var features []map[string]json.RawMessage
	if err := json.Unmarshal(fc["features"], &features); err != nil || features == nil {
		t.Fatalf("%s: features %s: %v", name, fc["features"], err)
	}
	for i, f := range features {
		var typ, id string
		var geom struct {
			Type        string            `json:"type"`
			Coordinates []json.RawMessage `json:"coordinates"`
		}
		var props map[string]any
		if json.Unmarshal(f["type"], &typ) != nil || typ != "Feature" ||
			json.Unmarshal(f["id"], &id) != nil || id == "" ||
			json.Unmarshal(f["geometry"], &geom) != nil || geom.Type != "Point" || len(geom.Coordinates) != 2 ||
			json.Unmarshal(f["properties"], &props) != nil || props == nil {
			t.Errorf("%s: feature %d: %v", name, i, f)
			continue
		}
		var lon, lat float64
		if json.Unmarshal(geom.Coordinates[0], &lon) != nil || json.Unmarshal(geom.Coordinates[1], &lat) != nil ||
			math.IsNaN(lon) || math.IsNaN(lat) || lon < -180 || lon > 180 || lat < -90 || lat > 90 {
			t.Errorf("%s: feature %d: coordinates %s", name, i, f["geometry"])
		}
	}
}

func decodePlaces(t *testing.T, r *testapp.Response) export.PlacesCollection {
	t.Helper()
	return testapp.Decode[export.PlacesCollection](r)
}

func decodeRecommendations(t *testing.T, r *testapp.Response) export.RecommendationsCollection {
	t.Helper()
	return testapp.Decode[export.RecommendationsCollection](r)
}

func featureIDs[P any](fs []export.Feature[P]) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.ID)
	}
	return out
}

func uuidOf(r *core.Record) string { return r.GetString(store.FieldUUID) }

// TestLayerExportIdentities: without a session both routes answer 401; every member gets both layers (M2-Q17), and
// the same files whoever asks: the author's private recommendation and its place are in neither, not even for the
// author, and the maintainer gets no hidden recommendation.
func TestLayerExportIdentities(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: a.Place, Author: a.Member,
		HiddenKind: store.HiddenByMaintainer, HiddenReason: "Test reason", HiddenBy: a.Maintainer})

	for _, path := range []string{api.ExportPlacesPath, api.ExportRecommendationsPath} {
		a.As(testapp.Anonymous).Get(path).Expect(http.StatusUnauthorized)
		var first []byte
		for _, id := range []testapp.Identity{testapp.Member, testapp.Author, testapp.Maintainer} {
			body := exportOf(t, a, id, path).Body.Bytes()
			// The files differ only in when they were made.
			var v map[string]any
			if err := json.Unmarshal(body, &v); err != nil {
				t.Fatal(err)
			}
			delete(v, "osfm:exported")
			norm, _ := json.Marshal(v)
			if first == nil {
				first = norm
			} else if !bytes.Equal(first, norm) {
				t.Errorf("%s as %s differs:\n%s\n%s", path, id, norm, first)
			}
		}
	}

	places := decodePlaces(t, exportOf(t, a, testapp.Author, api.ExportPlacesPath))
	if got, want := featureIDs(places.Features), []string{uuidOf(a.Place), uuidOf(a.PseudonymPlace)}; !slices.Equal(got, sorted(want)) {
		t.Errorf("places %v, want %v", got, sorted(want))
	}
	recs := decodeRecommendations(t, exportOf(t, a, testapp.Maintainer, api.ExportRecommendationsPath))
	want := sorted([]string{uuidOf(a.Recommendation), uuidOf(a.PseudonymRecommendation)})
	if got := featureIDs(recs.Features); !slices.Equal(got, want) {
		t.Errorf("recommendations %v, want %v", got, want)
	}
	a.CheckNoInternalIDs()
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

// TestLayerExportContents: what is in the layers and what is not (M2-Q3, privacy invariants 1–4). Private, hidden
// and deleted recommendations are absent, and so are places whose only recommendations are such; a deleted place
// takes its recommendations with it; a main-name recommendation of a member who left is signed "已退出成员", their
// pseudonym recommendation with the pseudonym; a landmark place outside the layer is left out.
func TestLayerExportContents(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	gone := seedMember(t, a, "test_gone", "Test Gone", "Test Gone Pen")
	other := seedMember(t, a, "test_other", "Test Other", "")

	// A place with every field set, a circle recommendation with every field set.
	full := a.SeedPlace(testapp.PlaceSeed{Name: "Test Full Place", Lat: 0.351, Lon: 7.071, Precision: "approx",
		Category: "cafe", Cuisine: []string{"coffee_shop", "german"}})
	set(t, a, full, map[string]any{"aliases": []string{"Test Alias One", "Test Alias Two"},
		"position_note": "Test position note", "status": "open",
		"landmark":     map[string]any{"type": "place", "place_uuid": uuidOf(a.Place)},
		"source_coord": map[string]any{"lat": 0.3512345, "lon": 7.0712345, "crs": "wgs84", "source": "crosshair"}})
	edited := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	fullRec := a.SeedRecommendation(testapp.RecommendationSeed{Place: full, Author: other,
		Attitude: domain.AttitudeMustTry, Text: "Test full text", Dishes: []string{"Test dish"},
		TimeSlots: []string{"lunch", "dinner"}, Season: "Test season", VisitedOn: "2026-02-03",
		Sources:              []map[string]any{{"url": "https://example.test/source", "kind": "visited", "excerpt": "Test excerpt"}},
		EditedByMaintainerAt: edited})

	// Only hidden, only deleted, only private: none of these places is in the layer.
	hiddenPlace := a.SeedPlace(testapp.PlaceSeed{Name: "Test Hidden Place"})
	hiddenRec := a.SeedRecommendation(testapp.RecommendationSeed{Place: hiddenPlace, Author: other,
		HiddenKind: store.HiddenByReports})
	deletedPlace := a.SeedPlace(testapp.PlaceSeed{Name: "Test Deleted Rec Place"})
	deletedRec := a.SeedRecommendation(testapp.RecommendationSeed{Place: deletedPlace, Author: other, Deleted: true})
	privatePlace := a.SeedPlace(testapp.PlaceSeed{Name: "Test Other Private Place"})
	privateRec := a.SeedRecommendation(testapp.RecommendationSeed{Place: privatePlace, Author: other,
		Visibility: domain.VisibilityPrivate})
	// A deleted place with a circle recommendation.
	removedPlace := a.SeedPlace(testapp.PlaceSeed{Name: "Test Removed Place", Deleted: true})
	removedRec := a.SeedRecommendation(testapp.RecommendationSeed{Place: removedPlace, Author: other})
	// A landmark that is only privately recommended, and a text landmark.
	landmarked := a.SeedPlace(testapp.PlaceSeed{Name: "Test Landmarked Place"})
	set(t, a, landmarked, map[string]any{"landmark": map[string]any{"type": "place", "place_uuid": uuidOf(privatePlace)}})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: landmarked, Author: other})
	texted := a.SeedPlace(testapp.PlaceSeed{Name: "Test Texted Place"})
	set(t, a, texted, map[string]any{"landmark": map[string]any{"type": "text", "text": "Test landmark text"}})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: texted, Author: other})
	// A member who left, with a main-name and a pseudonym recommendation.
	goneMain := a.SeedRecommendation(testapp.RecommendationSeed{Place: texted, Author: gone, Text: "Test gone main"})
	goneAlias := a.SeedRecommendation(testapp.RecommendationSeed{Place: landmarked, Author: gone,
		DisplayAs: domain.SignAsPseudonym, Text: "Test gone alias"})
	set(t, a, gone, map[string]any{"left_at": "2026-05-01 00:00:00.000Z"})

	placesResp := exportOf(t, a, testapp.Member, api.ExportPlacesPath)
	places := decodePlaces(t, placesResp)
	wantPlaces := sorted([]string{uuidOf(a.Place), uuidOf(a.PseudonymPlace), uuidOf(full), uuidOf(landmarked),
		uuidOf(texted)})
	if got := featureIDs(places.Features); !slices.Equal(got, wantPlaces) {
		t.Errorf("places %v, want %v", got, wantPlaces)
	}
	recsResp := exportOf(t, a, testapp.Member, api.ExportRecommendationsPath)
	recs := decodeRecommendations(t, recsResp)
	wantRecs := sorted([]string{uuidOf(a.Recommendation), uuidOf(a.PseudonymRecommendation), uuidOf(fullRec),
		recUUIDAt(t, a, landmarked, other), recUUIDAt(t, a, texted, other), uuidOf(goneMain), uuidOf(goneAlias)})
	if got := featureIDs(recs.Features); !slices.Equal(got, wantRecs) {
		t.Errorf("recommendations %v, want %v", got, wantRecs)
	}
	for _, absent := range []*core.Record{hiddenPlace, hiddenRec, deletedPlace, deletedRec, privatePlace, privateRec,
		removedPlace, removedRec, a.PrivatePlace, a.PrivateRecommendation} {
		for _, body := range [][]byte{placesResp.Body.Bytes(), recsResp.Body.Bytes()} {
			if bytes.Contains(body, []byte(uuidOf(absent))) {
				t.Errorf("%s %q is in an export", absent.Collection().Name, uuidOf(absent))
			}
		}
	}
	for _, text := range []string{"Test private text", "Test Other Private Place", "Test Hidden Place"} {
		if bytes.Contains(recsResp.Body.Bytes(), []byte(text)) || bytes.Contains(placesResp.Body.Bytes(), []byte(text)) {
			t.Errorf("%q is in an export", text)
		}
	}

	byID := func(id string) export.Feature[export.PlaceProperties] {
		for _, f := range places.Features {
			if f.ID == id {
				return f
			}
		}
		t.Fatalf("no place %s", id)
		return export.Feature[export.PlaceProperties]{}
	}
	str := func(s string) *string { return &s }
	f := byID(uuidOf(full))
	full = reload(t, a, full) // the stored times, to the millisecond
	created := full.GetDateTime(store.FieldCreated).Time().UTC()
	updated := full.GetDateTime(store.FieldUpdated).Time().UTC()
	want := export.Feature[export.PlaceProperties]{Type: "Feature", ID: uuidOf(full),
		Geometry: export.Point{Type: "Point", Coordinates: [2]float64{7.071, 0.351}},
		Properties: export.PlaceProperties{Name: "Test Full Place",
			Description: "Test Alias One, Test Alias Two\nTest position note\nTest Dumpling House",
			UUID:        uuidOf(full), PlName: "Test Full Place", Aliases: []string{"Test Alias One", "Test Alias Two"},
			Precision: "approx", PositionNote: "Test position note",
			Landmark: &export.Landmark{Type: "place", Place: uuidOf(a.Place), Name: "Test Dumpling House"},
			Category: str("cafe"), Cuisine: []string{"coffee_shop", "german"}, Status: str("open"),
			Created: created, Updated: updated}}
	if g, w := mustJSON(t, f), mustJSON(t, want); g != w {
		t.Errorf("full place\n got %s\nwant %s", g, w)
	}
	if l := byID(uuidOf(landmarked)).Properties.Landmark; l != nil {
		t.Errorf("landmark outside the layer: %+v", l)
	}
	if l := byID(uuidOf(texted)).Properties.Landmark; l == nil || *l != (export.Landmark{Type: "text", Text: "Test landmark text"}) {
		t.Errorf("text landmark: %+v", l)
	}
	if p := byID(uuidOf(a.PseudonymPlace)).Properties; p.Category == nil || p.Status != nil || p.Landmark != nil ||
		len(p.Aliases) != 0 || p.Aliases == nil || p.Description != "" {
		t.Errorf("seeded place: %+v", p)
	}

	recByID := func(id string) export.RecommendationProperties {
		for _, f := range recs.Features {
			if f.ID == id {
				return f.Properties
			}
		}
		t.Fatalf("no recommendation %s", id)
		return export.RecommendationProperties{}
	}
	fullRec = reload(t, a, fullRec)
	var fullFeature export.Feature[export.RecommendationProperties]
	for _, f := range recs.Features {
		if f.ID == uuidOf(fullRec) {
			fullFeature = f
		}
	}
	wantRec := export.Feature[export.RecommendationProperties]{Type: "Feature", ID: uuidOf(fullRec),
		Geometry: export.Point{Type: "Point", Coordinates: [2]float64{7.071, 0.351}},
		Properties: export.RecommendationProperties{Name: "Test Full Place · Test Other", Description: "Test full text",
			UUID: uuidOf(fullRec), Place: uuidOf(full), DisplayName: "Test Other", Attitude: "must_try",
			Text: "Test full text", Dishes: []string{"Test dish"}, TimeSlots: []string{"lunch", "dinner"},
			Season:    "Test season",
			Sources:   []export.Source{{URL: "https://example.test/source", Kind: "visited", Excerpt: "Test excerpt"}},
			VisitedOn: str("2026-02-03"), Created: fullRec.GetDateTime(store.FieldCreated).Time().UTC(),
			Updated: fullRec.GetDateTime(store.FieldUpdated).Time().UTC(), EditedByMaintainerAt: &edited}}
	if g, w := mustJSON(t, fullFeature), mustJSON(t, wantRec); g != w {
		t.Errorf("full recommendation\n got %s\nwant %s", g, w)
	}
	if r := recByID(uuidOf(goneMain)); r.DisplayName != export.LeftMemberName || !r.AuthorLeft ||
		r.Name != "Test Texted Place · "+export.LeftMemberName {
		t.Errorf("left member's main-name recommendation: %+v", r)
	}
	if r := recByID(uuidOf(goneAlias)); r.DisplayName != "Test Gone Pen" || r.AuthorLeft {
		t.Errorf("left member's pseudonym recommendation: %+v", r)
	}
	if r := recByID(uuidOf(a.PseudonymRecommendation)); r.DisplayName != "Test Pen Name" ||
		r.VisitedOn != nil || r.EditedByMaintainerAt != nil || r.Sources == nil || r.Dishes == nil {
		t.Errorf("pseudonym recommendation: %+v", r)
	}
	if r := recByID(uuidOf(a.Recommendation)); r.DisplayName != "Test Author" {
		t.Errorf("main-name recommendation: %+v", r)
	}
	// The left member's main name is nowhere, nor is anyone's pseudonym next to their main name.
	if bytes.Contains(recsResp.Body.Bytes(), []byte("Test Gone\"")) {
		t.Errorf("left member's main name in the export")
	}
	a.CheckNoInternalIDs()
}

// TestLayerExportDisplayText: name and description, which uMap renders as HTML through its own formatter, carry no
// markup that would fetch anything or pull in another property; the osfm: properties keep the text as written.
func TestLayerExportDisplayText(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	raw := `Test <img src="https://example.test/t.png"> {{https://example.test/i.png}} {osfm:uuid} **bold**`
	p := a.SeedPlace(testapp.PlaceSeed{Name: "Test <b>Place</b> {name}"})
	set(t, a, p, map[string]any{"position_note": raw})
	r := a.SeedRecommendation(testapp.RecommendationSeed{Place: p, Author: a.Member, Text: raw})

	places := decodePlaces(t, exportOf(t, a, testapp.Member, api.ExportPlacesPath))
	recs := decodeRecommendations(t, exportOf(t, a, testapp.Member, api.ExportRecommendationsPath))
	shown := `Test ＜img src="https://example.test/t.png"＞ ｛｛https://example.test/i.png｝｝ ｛osfm:uuid｝ **bold**`
	for _, f := range places.Features {
		if f.ID != uuidOf(p) {
			continue
		}
		if f.Properties.Name != "Test ＜b＞Place＜/b＞ ｛name｝" || f.Properties.Description != shown ||
			f.Properties.PlName != "Test <b>Place</b> {name}" || f.Properties.PositionNote != raw {
			t.Errorf("place: %+v", f.Properties)
		}
	}
	for _, f := range recs.Features {
		if f.ID != uuidOf(r) {
			continue
		}
		if f.Properties.Description != shown || f.Properties.Text != raw ||
			f.Properties.Name != "Test ＜b＞Place＜/b＞ ｛name｝ · Test Member" {
			t.Errorf("recommendation: %+v", f.Properties)
		}
	}
}

func recUUIDAt(t *testing.T, a *testapp.App, place, author *core.Record) string {
	t.Helper()
	r, err := store.Query(a, store.Recommendations).Where(dbx.HashExp{"place": place.Id, "author": author.Id}).One()
	if err != nil {
		t.Fatal(err)
	}
	return uuidOf(r)
}

func reload(t *testing.T, a *testapp.App, r *core.Record) *core.Record {
	t.Helper()
	fresh, err := a.FindRecordById(r.Collection().Name, r.Id)
	if err != nil {
		t.Fatal(err)
	}
	return fresh
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestLayerExportNoMemberData: the places layer has exactly the place properties and nothing about members; the
// recommendations layer exactly its properties, with no member uuid, login name, external link or internal ID
// anywhere in either file; both carry their format version, licence and attribution (H5).
func TestLayerExportNoMemberData(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: a.Place, Author: a.Maintainer,
		Sources: []map[string]any{{"kind": "chat"}}})

	members, err := store.Query(a, store.Members).IncludeDeleted().All()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, schema, notice string
		license              export.License
		keys                 []string
	}{
		{api.ExportPlacesPath, export.PlacesSchema, "Open Database License (ODbL) v1.0", export.ODbL, placeKeys},
		{api.ExportRecommendationsPath, export.RecommendationsSchema, "(CC BY-SA 4.0)", export.CCBYSA, recommendationKeys},
	} {
		body := exportOf(t, a, testapp.Member, tc.path).Body.Bytes()
		var fc struct {
			Features []struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"features"`
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal(body, &fc); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &top); err != nil {
			t.Fatal(err)
		}
		if got := slices.Sorted(maps.Keys(top)); !slices.Equal(got, sorted(collectionKeys)) {
			t.Errorf("%s: collection members %v", tc.path, got)
		}
		var c export.Collection
		if err := json.Unmarshal(body, &c); err != nil {
			t.Fatal(err)
		}
		if c.Schema != tc.schema || c.License != tc.license || c.Attribution != "© OpenStreetMap contributors" ||
			c.AttributionURL != "https://www.openstreetmap.org/copyright" || !strings.Contains(c.Notice, tc.notice) || !strings.Contains(c.Notice, "© OpenStreetMap contributors") ||
			time.Since(c.Exported) > time.Minute || c.City != "" {
			t.Errorf("%s: collection %+v", tc.path, c)
		}
		if len(fc.Features) == 0 {
			t.Fatalf("%s: no features", tc.path)
		}
		for _, f := range fc.Features {
			if got := slices.Sorted(maps.Keys(f.Properties)); !slices.Equal(got, sorted(tc.keys)) {
				t.Errorf("%s: properties %v", tc.path, got)
			}
		}
		for _, m := range members {
			for _, s := range []string{uuidOf(m), m.Id, m.GetString("login_name")} {
				if bytes.Contains(body, []byte(s)) {
					t.Errorf("%s: member %q in the file", tc.path, s)
				}
			}
		}
		for _, s := range []string{testapp.AuthorLink, "Test Blog", `"main"`, `"pseudonym"`, `"author"`, "display_as",
			"visibility", "hidden"} {
			if bytes.Contains(body, []byte(s)) {
				t.Errorf("%s: %q in the file", tc.path, s)
			}
		}
	}
	// No signature in the places layer at all.
	body := exportOf(t, a, testapp.Member, api.ExportPlacesPath).Body.Bytes()
	for _, name := range []string{"Test Author", "Test Pen Name", "Test Maintainer"} {
		if bytes.Contains(body, []byte(name)) {
			t.Errorf("places layer: %q", name)
		}
	}
	a.CheckNoInternalIDs()
}

// TestLayerExportCity: ?city= keeps the places inside the city and their recommendations, names the city in the file
// and the file name; an unknown city is 400 unknown_city; an empty one is the whole layer.
func TestLayerExportCity(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	far := a.SeedPlace(testapp.PlaceSeed{Name: "Test Far Place", Lat: 10.5, Lon: 20.5})
	farRec := a.SeedRecommendation(testapp.RecommendationSeed{Place: far, Author: a.Member})
	settingsRec, err := a.FindRecordById(store.InstanceSettings, store.SettingsID)
	if err != nil {
		t.Fatal(err)
	}
	set(t, a, settingsRec, map[string]any{"cities": []map[string]any{
		{"slug": testapp.CitySlug, "name": testapp.CityName, "bbox": []float64{7.02, 0.31, 7.1, 0.37}, "center": []float64{7.06, 0.34}},
		{"slug": "test-far", "name": "Test Far", "bbox": []float64{20, 10, 21, 11}, "center": []float64{20.5, 10.5}},
	}})

	r := exportOf(t, a, testapp.Member, api.ExportPlacesPath+"?city=test-far")
	if cd := r.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="osfm-places-test-far-`) {
		t.Errorf("Content-Disposition %q", cd)
	}
	if p := decodePlaces(t, r); p.City != "test-far" || !slices.Equal(featureIDs(p.Features), []string{uuidOf(far)}) {
		t.Errorf("far places: %s %v", p.City, featureIDs(p.Features))
	}
	r = exportOf(t, a, testapp.Member, api.ExportRecommendationsPath+"?city=test-far")
	if p := decodeRecommendations(t, r); p.City != "test-far" || !slices.Equal(featureIDs(p.Features), []string{uuidOf(farRec)}) {
		t.Errorf("far recommendations: %s %v", p.City, featureIDs(p.Features))
	}
	r = exportOf(t, a, testapp.Member, api.ExportRecommendationsPath+"?city="+testapp.CitySlug)
	if p := decodeRecommendations(t, r); slices.Contains(featureIDs(p.Features), uuidOf(farRec)) || len(p.Features) != 2 {
		t.Errorf("city recommendations: %v", featureIDs(p.Features))
	}
	r = exportOf(t, a, testapp.Member, api.ExportPlacesPath+"?city=")
	if p := decodePlaces(t, r); p.City != "" || len(p.Features) != 3 {
		t.Errorf("whole layer: %v", featureIDs(p.Features))
	}
	if cd := r.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="osfm-places-2`) {
		t.Errorf("Content-Disposition %q", cd)
	}
	for _, path := range []string{api.ExportPlacesPath, api.ExportRecommendationsPath} {
		r := a.As(testapp.Member).Get(path + "?city=nowhere").Expect(http.StatusBadRequest)
		if e := testapp.Decode[api.Error](r); e.Code != api.CodeExportUnknownCity {
			t.Errorf("%s: %+v", path, e)
		}
	}
}

// TestLayerExportBudget: exports count against a per-member budget of both layers together (429 with Retry-After),
// and another member has a budget of their own.
func TestLayerExportBudget(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	for i := range 10 {
		path := api.ExportPlacesPath
		if i%2 == 1 {
			path = api.ExportRecommendationsPath
		}
		a.As(testapp.Member).Get(path).Expect(http.StatusOK)
	}
	r := a.As(testapp.Member).Get(api.ExportRecommendationsPath).Expect(http.StatusTooManyRequests)
	if e := testapp.Decode[api.Error](r); e.Code != api.CodeRateLimited || r.Header().Get("Retry-After") == "" {
		t.Errorf("429: %+v, Retry-After %q", e, r.Header().Get("Retry-After"))
	}
	a.As(testapp.Author).Get(api.ExportPlacesPath).Expect(http.StatusOK)
}

// TestCircleLayerRule: store.InCircleLayer selects exactly the recommendations domain.InCircleLayer accepts, over
// every combination of visibility, hidden, deleted and deleted place; and those are the ones a member who wrote none
// of them sees.
func TestCircleLayerRule(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	for _, vis := range []string{domain.VisibilityPrivate, domain.VisibilityCircle, domain.VisibilityFederated} {
		for _, hidden := range []string{"", store.HiddenByMaintainer, store.HiddenByReports, store.HiddenMemberRemoved} {
			for _, deleted := range []bool{false, true} {
				for _, placeDeleted := range []bool{false, true} {
					p := a.SeedPlace(testapp.PlaceSeed{})
					r := a.SeedRecommendation(testapp.RecommendationSeed{Place: p, Author: a.Author, Visibility: vis,
						HiddenKind: hidden, Deleted: deleted})
					if placeDeleted {
						if err := store.SoftDelete(a, p); err != nil {
							t.Fatal(err)
						}
					}
					_ = r
				}
			}
		}
	}
	// A row written inconsistently (hidden without a kind) counts as hidden.
	odd := a.SeedRecommendation(testapp.RecommendationSeed{Place: a.SeedPlace(testapp.PlaceSeed{}), Author: a.Author})
	set(t, a, odd, map[string]any{"hidden": true})

	all, err := store.Query(a, store.Recommendations).IncludeDeleted().All()
	if err != nil {
		t.Fatal(err)
	}
	states, err := store.RecommendationStates(a, all)
	if err != nil {
		t.Fatal(err)
	}
	inLayer, err := store.CircleRecommendations(a).All()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range inLayer {
		got[r.Id] = true
	}
	member := domain.Viewer{MemberID: a.Member.Id}
	for i, r := range all {
		want := domain.InCircleLayer(states[i])
		if got[r.Id] != want {
			t.Errorf("%+v: query %v, domain %v", states[i], got[r.Id], want)
		}
		if want != domain.CanSeeRecommendation(member, states[i]) {
			t.Errorf("%+v: in the layer %v, but a member sees it: %v", states[i], want, !want)
		}
	}
	if len(got) == 0 {
		t.Fatal("empty layer")
	}
}
