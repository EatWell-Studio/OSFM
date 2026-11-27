package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/ratelimit"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

func ptr[T any](v T) *T { return &v }

// newPlace is a made-up place in testapp's city with a minimal first recommendation.
func newPlace(name string, lat, lon float64) api.CreatePlaceRequest {
	return api.CreatePlaceRequest{Name: name, Lat: ptr(lat), Lon: ptr(lon),
		Recommendation: &api.RecommendationInput{Attitude: domain.AttitudeGood, Text: "Test text"}}
}

func recommendationsPath(placeUUID string) string {
	return strings.Replace(api.PlaceRecommendationsPath, "{uuid}", placeUUID, 1)
}

func findByUUID(t *testing.T, a *testapp.App, collection, id string) *core.Record {
	t.Helper()
	r, err := store.FindByUUID(a, collection, id)
	if err != nil {
		t.Fatalf("%s %s: %v", collection, id, err)
	}
	return r
}

func expectError(t *testing.T, r *testapp.Response, status int, code string) api.Error {
	t.Helper()
	r.Expect(status)
	e := testapp.Decode[api.Error](r)
	if e.Code != code || e.Status != status {
		t.Errorf("%s %s: error %+v, want %d %s", r.Method, r.Path, e, status, code)
	}
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("%s %s: Cache-Control %q", r.Method, r.Path, r.Header().Get("Cache-Control"))
	}
	return e
}

// TestCreatePlace: as the four identities; a member creates a place with every field, the place carries no member
// fields and keeps the coordinates as sent, the audit log records who created it.
func TestCreatePlace(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())

	a.As(testapp.Anonymous).Post(api.PlacesPath, newPlace("Test Anon Place", 0.34, 7.06)).
		Expect(http.StatusUnauthorized)
	// Without X-OSFM: refused by the session layer.
	body, _ := json.Marshal(newPlace("Test No Header", 0.34, 7.06))
	req := httptest.NewRequest(http.MethodPost, api.PlacesPath, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	a.As(testapp.Member).Send(req).Expect(http.StatusForbidden)
	for _, id := range []testapp.Identity{testapp.Author, testapp.Maintainer} {
		r := a.As(id).Post(api.PlacesPath, newPlace("Test Place of "+string(id), 0.35, 7.07)).
			Expect(http.StatusCreated)
		created := testapp.Decode[api.CreatedPlace](r)
		if !store.IsUUID(created.Place) || !store.IsUUID(created.Recommendation) {
			t.Errorf("%s: %+v", id, created)
		}
	}

	full := api.CreatePlaceRequest{
		Name: " Test Full Kitchen ", Aliases: []string{"Test Other Name"}, Lat: ptr(0.341234567891),
		Lon: ptr(7.061234567891), Precision: domain.PrecisionApprox, PositionNote: "Test across the street\nleft",
		Landmark: &api.LandmarkInput{Type: domain.LandmarkPlace, Place: a.Place.GetString(store.FieldUUID)},
		Category: "restaurant", Cuisine: []string{"sichuan", "noodle"},
		SourceCoord: &api.SourceCoordInput{CRS: domain.CRSWGS84, Source: domain.CoordFromSearch},
		Recommendation: &api.RecommendationInput{Attitude: domain.AttitudeMustTry, Text: " Test full text ",
			Dishes: []string{"Test dish"}, TimeSlots: []string{"dinner", "lunch"}, Season: "Test winter",
			Sources:   []api.Source{{URL: "https://example.test/note", Kind: "chat", Excerpt: "Test words"}},
			VisitedOn: time.Now().UTC().Format(time.DateOnly), Visibility: domain.VisibilityPrivate},
	}
	r := a.As(testapp.Member).Post(api.PlacesPath, full).Expect(http.StatusCreated)
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", r.Header().Get("Cache-Control"))
	}
	var keys map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &keys); err != nil || len(keys) != 2 {
		t.Errorf("response %s", r.Body.String())
	}
	created := testapp.Decode[api.CreatedPlace](r)

	place := findByUUID(t, a, store.Places, created.Place)
	if place.GetString("name") != "Test Full Kitchen" || place.GetFloat("lat") != 0.34123 ||
		place.GetFloat("lon") != 7.06123 || place.GetString("precision") != domain.PrecisionApprox ||
		place.GetString("category") != "restaurant" || place.GetString("status") != "" ||
		place.GetString("position_note") != "Test across the street\nleft" {
		t.Errorf("place %v", place.PublicExport())
	}
	var coord struct {
		Lat, Lon    float64
		CRS, Source string
	}
	if err := json.Unmarshal([]byte(place.GetString("source_coord")), &coord); err != nil || coord.Lat != 0.341234567891 ||
		coord.Lon != 7.061234567891 || coord.CRS != domain.CRSWGS84 || coord.Source != domain.CoordFromSearch {
		t.Errorf("source_coord %s %+v (%v)", place.GetString("source_coord"), coord, err)
	}
	var landmark map[string]string
	if err := place.UnmarshalJSONField("landmark", &landmark); err != nil ||
		landmark["type"] != "place" || landmark["place_uuid"] != a.Place.GetString(store.FieldUUID) {
		t.Errorf("landmark %s", place.GetString("landmark"))
	}
	// No member fields in the places layer: no relation to members, nothing of the member in the record.
	for _, f := range place.Collection().Fields {
		if rel, ok := f.(*core.RelationField); ok {
			t.Errorf("places has a relation %s", rel.Name)
		}
	}
	export, _ := json.Marshal(place.PublicExport())
	for _, s := range []string{a.Member.Id, a.Member.GetString(store.FieldUUID), a.Member.GetString("display_name"),
		a.Member.GetString("login_name")} {
		if strings.Contains(string(export), s) || strings.Contains(r.Body.String(), s) {
			t.Errorf("the place or the response contains the member's %q", s)
		}
	}

	rec := findByUUID(t, a, store.Recommendations, created.Recommendation)
	if rec.GetString("place") != place.Id || rec.GetString("author") != a.Member.Id ||
		rec.GetString("display_as") != domain.SignAsMain || rec.GetString("visibility") != domain.VisibilityPrivate ||
		rec.GetString("attitude") != domain.AttitudeMustTry || rec.GetString("text") != "Test full text" ||
		strings.Join(rec.GetStringSlice("time_slots"), ",") != "lunch,dinner" ||
		rec.GetString("season") != "Test winter" || rec.GetDateTime("visited_on").IsZero() {
		t.Errorf("recommendation %v", rec.PublicExport())
	}

	audit, err := a.FindFirstRecordByFilter(store.AuditLog, "action = {:a} && target_uuid = {:u}",
		dbx.Params{"a": api.AuditPlaceCreate, "u": created.Place})
	if err != nil || audit.GetString("actor") != a.Member.Id || audit.GetString("target_type") != store.Places {
		t.Errorf("audit entry: %v %v", audit, err)
	}
	a.CheckNoInternalIDs()
}

// TestCreatePlaceRejects: values the rules refuse create nothing.
func TestCreatePlaceRejects(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	before, err := store.Query(a, store.Places).IncludeDeleted().Count()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*api.CreatePlaceRequest)
		code   string
		field  string
	}{
		{"empty name", func(r *api.CreatePlaceRequest) { r.Name = " " }, api.CodeInvalidRequest, "name"},
		{"no latitude", func(r *api.CreatePlaceRequest) { r.Lat = nil }, api.CodeInvalidRequest, "lat"},
		{"no recommendation", func(r *api.CreatePlaceRequest) { r.Recommendation = nil }, api.CodeInvalidRequest,
			"recommendation"},
		{"category off the whitelist", func(r *api.CreatePlaceRequest) { r.Category = "pharmacy" },
			api.CodeInvalidRequest, "category"},
		{"cuisine off the list", func(r *api.CreatePlaceRequest) { r.Cuisine = []string{"regional"} },
			api.CodeInvalidRequest, "cuisine[0]"},
		{"outside the city", func(r *api.CreatePlaceRequest) { r.Lat, r.Lon = ptr(31.23), ptr(121.47) },
			api.CodeOutsideCities, ""},
		{"empty text", func(r *api.CreatePlaceRequest) { r.Recommendation.Text = "  " }, api.CodeInvalidRequest,
			"recommendation.text"},
		{"no attitude", func(r *api.CreatePlaceRequest) { r.Recommendation.Attitude = "" }, api.CodeInvalidRequest,
			"recommendation.attitude"},
		{"federated", func(r *api.CreatePlaceRequest) { r.Recommendation.Visibility = domain.VisibilityFederated },
			api.CodeInvalidRequest, "recommendation.visibility"},
		{"pseudonym without one", func(r *api.CreatePlaceRequest) { r.Recommendation.DisplayAs = "pseudonym" },
			api.CodeNoPseudonym, ""},
		{"visit in the future", func(r *api.CreatePlaceRequest) {
			r.Recommendation.VisitedOn = time.Now().AddDate(0, 0, 3).Format(time.DateOnly)
		}, api.CodeInvalidRequest, "recommendation.visitedOn"},
		{"source link not http", func(r *api.CreatePlaceRequest) {
			r.Recommendation.Sources = []api.Source{{URL: "javascript:alert(1)", Kind: "note"}}
		}, api.CodeInvalidRequest, "recommendation.sources[0].url"},
		{"landmark only its author sees", func(r *api.CreatePlaceRequest) {
			r.Landmark = &api.LandmarkInput{Type: "place", Place: a.PrivatePlace.GetString(store.FieldUUID)}
		}, api.CodeInvalidRequest, "landmark.place"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := newPlace("Test Rejected Place", 0.34, 7.06)
			tc.change(&req)
			e := expectError(t, a.As(testapp.Member).Post(api.PlacesPath, req), http.StatusBadRequest, tc.code)
			if tc.field != "" && e.Data["field"] != tc.field {
				t.Errorf("field %v, want %s", e.Data["field"], tc.field)
			}
		})
	}
	expectError(t, a.As(testapp.Member).Post(api.PlacesPath, nil), http.StatusBadRequest, api.CodeInvalidRequest)
	big := newPlace("Test Big", 0.34, 7.06)
	big.Recommendation.Text = strings.Repeat("a", api.DefaultMaxBody)
	expectError(t, a.As(testapp.Member).Post(api.PlacesPath, big), http.StatusRequestEntityTooLarge,
		api.CodeBodyTooLarge)
	if after, _ := store.Query(a, store.Places).IncludeDeleted().Count(); after != before {
		t.Errorf("%d places after the refused requests, %d before", after, before)
	}

	// The author has a pseudonym and may sign with it; a landmark only the author sees is theirs to use.
	req := newPlace("Test Pen Name Place", 0.34, 7.06)
	req.Recommendation.DisplayAs = domain.SignAsPseudonym
	req.Landmark = &api.LandmarkInput{Type: "place", Place: a.PrivatePlace.GetString(store.FieldUUID)}
	created := testapp.Decode[api.CreatedPlace](a.As(testapp.Author).Post(api.PlacesPath, req).
		Expect(http.StatusCreated))
	if rec := findByUUID(t, a, store.Recommendations, created.Recommendation); rec.GetString("display_as") !=
		domain.SignAsPseudonym {
		t.Errorf("display_as %q", rec.GetString("display_as"))
	}
}

// TestCreateRecommendation: one live recommendation per member and place (M2-Q1): the second is 409 with the
// member's own one; another member may add theirs; places the caller cannot see are 404.
func TestCreateRecommendation(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	placePath := recommendationsPath(a.Place.GetString(store.FieldUUID))
	rec := api.RecommendationInput{Attitude: domain.AttitudeOK, Text: "Test second opinion"}

	a.As(testapp.Anonymous).Post(placePath, rec).Expect(http.StatusUnauthorized)
	// The author has recommended the place: 409 with their recommendation's uuid.
	e := expectError(t, a.As(testapp.Author).Post(placePath, rec), http.StatusConflict, api.CodeAlreadyRecommended)
	if e.Data["recommendation"] != a.Recommendation.GetString(store.FieldUUID) {
		t.Errorf("409 data %v", e.Data)
	}
	// Even with a form that is not filled in yet.
	expectError(t, a.As(testapp.Author).Post(placePath, api.RecommendationInput{}), http.StatusConflict,
		api.CodeAlreadyRecommended)
	// Another member and a maintainer may (C1).
	mine := testapp.Decode[api.CreatedRecommendation](a.As(testapp.Member).Post(placePath, rec).
		Expect(http.StatusCreated))
	a.As(testapp.Maintainer).Post(placePath, rec).Expect(http.StatusCreated)
	stored := findByUUID(t, a, store.Recommendations, mine.Recommendation)
	if stored.GetString("author") != a.Member.Id || stored.GetString("place") != a.Place.Id {
		t.Errorf("%v", stored.PublicExport())
	}
	// The member's second one, signed differently: still 409 with their own.
	e = expectError(t, a.As(testapp.Member).Post(placePath, api.RecommendationInput{Attitude: "good", Text: "x"}),
		http.StatusConflict, api.CodeAlreadyRecommended)
	if e.Data["recommendation"] != mine.Recommendation {
		t.Errorf("409 data %v", e.Data)
	}
	// After deleting it, the member may write a new one.
	if err := store.SoftDelete(a, stored); err != nil {
		t.Fatal(err)
	}
	a.As(testapp.Member).Post(placePath, rec).Expect(http.StatusCreated)

	// Rules of the content.
	other := recommendationsPath(a.PseudonymPlace.GetString(store.FieldUUID))
	e = expectError(t, a.As(testapp.Member).Post(other, api.RecommendationInput{Attitude: "good", Text: " "}),
		http.StatusBadRequest, api.CodeInvalidRequest)
	if e.Data["field"] != "text" {
		t.Errorf("field %v", e.Data["field"])
	}
	expectError(t, a.As(testapp.Member).Post(other, api.RecommendationInput{Attitude: "good", Text: "x",
		DisplayAs: "pseudonym"}), http.StatusBadRequest, api.CodeNoPseudonym)
	expectError(t, a.As(testapp.Member).Post(other, api.RecommendationInput{Attitude: "good", Text: "x",
		Visibility: "federated"}), http.StatusBadRequest, api.CodeInvalidRequest)
	expectError(t, a.As(testapp.Member).Post(other, nil), http.StatusBadRequest, api.CodeInvalidRequest)

	// A place that exists only for its author (their private recommendation), one that does not exist, a path that
	// is not a uuid: all 404, for members and maintainers alike.
	private := recommendationsPath(a.PrivatePlace.GetString(store.FieldUUID))
	for _, id := range []testapp.Identity{testapp.Member, testapp.Maintainer} {
		expectError(t, a.As(id).Post(private, rec), http.StatusNotFound, api.CodeNotFound)
		expectError(t, a.As(id).Post(recommendationsPath("0190a1b2-0000-7000-8000-00000000ffff"), rec),
			http.StatusNotFound, api.CodeNotFound)
		expectError(t, a.As(id).Post(recommendationsPath(a.Place.Id), rec), http.StatusNotFound, api.CodeNotFound)
	}
	// The author sees it, and has recommended it.
	expectError(t, a.As(testapp.Author).Post(private, rec), http.StatusConflict, api.CodeAlreadyRecommended)
	a.CheckNoInternalIDs()
}

// TestPlacesWriteBudget: both write routes count against the member's write budget.
func TestPlacesWriteBudget(t *testing.T) {
	t.Parallel()
	a := start(t, api.Limits{Writes: ratelimit.Rate{Burst: 2, Every: time.Hour}})
	c := a.As(testapp.Member)
	c.Post(api.PlacesPath, newPlace("Test Budget One", 0.34, 7.06)).Expect(http.StatusCreated)
	c.Post(recommendationsPath(a.Place.GetString(store.FieldUUID)), api.RecommendationInput{Attitude: "good",
		Text: "x"}).Expect(http.StatusCreated)
	expectError(t, c.Post(api.PlacesPath, newPlace("Test Budget Three", 0.34, 7.06)), http.StatusTooManyRequests,
		api.CodeRateLimited)
}

// offset returns the point metres north of (lat, lon).
func offset(lat, lon, metres float64) (float64, float64) {
	return lat + metres/111195, lon
}

func nearby(c *testapp.Client, lat, lon float64, name string) api.NearbyPlaces {
	q := make([]string, 0, 3)
	q = append(q, "lat="+jsonNumber(lat), "lon="+jsonNumber(lon), "name="+strings.ReplaceAll(name, " ", "+"))
	return testapp.Decode[api.NearbyPlaces](c.Get(api.PlacesNearbyPath + "?" + strings.Join(q, "&")).
		Expect(http.StatusOK))
}

func jsonNumber(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// TestNearby: places within 100 m with a similar name, only those the caller can see, nearest first, at most 5, with
// distances rounded to 10 m and no coordinates.
func TestNearby(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	const lat, lon = 0.36, 7.08
	var created []string
	for i, m := range []float64{0, 30, 64, 97, 20, 46, 130} {
		la, lo := offset(lat, lon, m)
		name := "Test Ramen House"
		if i == 4 {
			name = "Test Pizza Corner" // near but another name
		}
		created = append(created, testapp.Decode[api.CreatedPlace](a.As(testapp.Author).
			Post(api.PlacesPath, newPlace(name, la, lo)).Expect(http.StatusCreated)).Place)
	}
	got := nearby(a.As(testapp.Member), lat, lon, "test ramen-house")
	want := []struct {
		uuid     string
		distance int
	}{{created[0], 0}, {created[1], 30}, {created[5], 50}, {created[2], 60}, {created[3], 100}}
	if len(got.Places) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i, w := range want {
		p := got.Places[i]
		if p.UUID != w.uuid || p.Distance != w.distance || p.Name != "Test Ramen House" {
			t.Errorf("%d: %+v, want %+v", i, p, w)
		}
	}
	r := a.As(testapp.Member).Get(api.PlacesNearbyPath + "?lat=0.36&lon=7.08&name=Test+Ramen+House")
	if strings.Contains(r.Body.String(), "lat") || r.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("%s %v", r.Body.String(), r.Header())
	}

	// A place with only the author's private recommendation exists for the author only (M2-Q3).
	pLat, pLon := a.PrivatePlace.GetFloat("lat"), a.PrivatePlace.GetFloat("lon")
	for id, n := range map[testapp.Identity]int{testapp.Member: 0, testapp.Maintainer: 0, testapp.Author: 1} {
		if got := nearby(a.As(id), pLat, pLon, "Test Secret Bakery"); len(got.Places) != n {
			t.Errorf("%s: %+v", id, got)
		}
	}
	// Aliases count as names.
	req := newPlace("Test Grill", 0.32, 7.03)
	req.Aliases = []string{"Test Barbecue Yard"}
	a.As(testapp.Member).Post(api.PlacesPath, req).Expect(http.StatusCreated)
	if got := nearby(a.As(testapp.Maintainer), 0.32, 7.03, "Test Barbecue Yard"); len(got.Places) != 1 ||
		got.Places[0].Name != "Test Grill" {
		t.Errorf("%+v", got)
	}

	a.As(testapp.Anonymous).Get(api.PlacesNearbyPath + "?lat=0.36&lon=7.08&name=x").Expect(http.StatusUnauthorized)
	for _, q := range []string{"", "?lat=0.36&lon=7.08", "?lat=x&lon=7.08&name=a", "?lat=91&lon=7.08&name=a",
		"?lat=NaN&lon=7.08&name=a", "?lat=0.36&lon=7.08&name=+++", "?lat=0.36&lon=7.08&name=" +
			strings.Repeat("a", 201)} {
		expectError(t, a.As(testapp.Member).Get(api.PlacesNearbyPath+q), http.StatusBadRequest,
			api.CodeInvalidRequest)
	}
	a.CheckNoInternalIDs()
}
