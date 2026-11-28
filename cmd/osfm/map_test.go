package main

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// Integration tests of the map data and the place card (T027), as the four identities. The seeded city is
// testapp.CitySlug with the bounding box 7.02,0.31,7.1,0.37.

func mapPath(params url.Values) string {
	return api.MapPlacesPath + "?" + params.Encode()
}

func cityParams(extra ...string) url.Values {
	q := url.Values{api.MapParamCity: {testapp.CitySlug}}
	for i := 0; i+1 < len(extra); i += 2 {
		q.Add(extra[i], extra[i+1])
	}
	return q
}

func cardPath(uuid string) string {
	return strings.Replace(api.PlaceCardPath, "{uuid}", uuid, 1)
}

func signaturesPath() string {
	return api.SignaturesPath + "?" + api.MapParamCity + "=" + testapp.CitySlug
}

func placeUUIDs(places []api.MapPlace) []string {
	out := make([]string, 0, len(places))
	for _, p := range places {
		out = append(out, p.UUID)
	}
	slices.Sort(out)
	return out
}

// seedMember writes a made-up member who may sign in.
func seedMember(t *testing.T, a *testapp.App, login, name, pseudonym string) *core.Record {
	t.Helper()
	m, err := auth.NewMemberRecord(a)
	if err != nil {
		t.Fatal(err)
	}
	m.Set("login_name", login)
	m.Set("display_name", name)
	m.Set("pseudonym", pseudonym)
	m.Set("role", auth.RoleMember)
	m.Set("invited_by", a.Maintainer.Id)
	m.Set("joined_at", "2026-01-01 00:00:00.000Z")
	if err := a.Save(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func set(t *testing.T, a *testapp.App, r *core.Record, values map[string]any) {
	t.Helper()
	for k, v := range values {
		r.Set(k, v)
	}
	if err := a.Save(r); err != nil {
		t.Fatal(err)
	}
}

// TestMapIdentities: without a session every route answers 401; each member sees the places with a recommendation
// they may see — the author's private place only the author, also on the card, where it is 404 for everyone else,
// like a place that does not exist.
func TestMapIdentities(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	missing := "0190a1b2-0000-7000-8000-00000000ffff"
	for _, path := range []string{mapPath(cityParams()), cardPath(a.Place.GetString(store.FieldUUID)), signaturesPath()} {
		a.As(testapp.Anonymous).Get(path).Expect(http.StatusUnauthorized)
	}
	for id, want := range map[testapp.Identity][]*core.Record{
		testapp.Member:     {a.Place, a.PseudonymPlace},
		testapp.Author:     {a.Place, a.PseudonymPlace, a.PrivatePlace},
		testapp.Maintainer: {a.Place, a.PseudonymPlace},
	} {
		t.Run(string(id), func(t *testing.T) {
			c := a.As(id)
			places := testapp.Decode[[]api.MapPlace](c.Get(mapPath(cityParams())).Expect(http.StatusOK))
			if got := placeUUIDs(places); !slices.Equal(got, sortedUUIDs(want...)) {
				t.Errorf("places %v, want %v", got, sortedUUIDs(want...))
			}
			privateCard := c.Get(cardPath(a.PrivatePlace.GetString(store.FieldUUID)))
			if id == testapp.Author {
				card := testapp.Decode[api.PlaceCard](privateCard.Expect(http.StatusOK))
				if len(card.Recommendations) != 1 || card.Recommendations[0].Visibility != domain.VisibilityPrivate ||
					!card.Recommendations[0].IsMine || card.Counts.OK != 1 {
					t.Errorf("the author's private card: %+v", card)
				}
			} else {
				hidden := privateCard.Expect(http.StatusNotFound).Body.String()
				absent := c.Get(cardPath(missing)).Expect(http.StatusNotFound).Body.String()
				if hidden != absent {
					t.Errorf("a place the caller may not see answers %s, a missing one %s", hidden, absent)
				}
			}
			c.Get(cardPath("not-a-uuid")).Expect(http.StatusNotFound)
			card := testapp.Decode[api.PlaceCard](c.Get(cardPath(a.Place.GetString(store.FieldUUID))).Expect(http.StatusOK))
			if card.City != testapp.CitySlug || card.Name != "Test Dumpling House" || card.Category != "restaurant" ||
				len(card.Recommendations) != 1 || card.Recommendations[0].DisplayName != "Test Author" ||
				card.Recommendations[0].IsMine != (id == testapp.Author) || card.Counts != (api.PlaceCounts{Good: 1}) {
				t.Errorf("card: %+v", card)
			}
		})
	}
	a.CheckNoInternalIDs()
}

// TestMapCacheHeaders: the map data and the signatures are private, no-cache with an ETag that answers 304 when
// unchanged and changes with the data; the card and errors are not stored.
func TestMapCacheHeaders(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	c := a.As(testapp.Member)
	for _, path := range []string{mapPath(cityParams()), signaturesPath()} {
		r := c.Get(path).Expect(http.StatusOK)
		etag := r.Header().Get("ETag")
		if cc := r.Header().Get("Cache-Control"); cc != "private, no-cache" || etag == "" ||
			!strings.HasPrefix(etag, `"`) {
			t.Fatalf("%s: Cache-Control %q, ETag %q", path, cc, etag)
		}
		if r.Header().Get("Content-Type") != "application/json" {
			t.Errorf("%s: Content-Type %q", path, r.Header().Get("Content-Type"))
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("If-None-Match", etag)
		notModified := c.Send(req).Expect(http.StatusNotModified)
		if notModified.Body.Len() != 0 || notModified.Header().Get("ETag") != etag ||
			notModified.Header().Get("Cache-Control") != "private, no-cache" {
			t.Errorf("%s: 304 with %q, headers %v", path, notModified.Body.String(), notModified.Header())
		}
		req = httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("If-None-Match", `"something-else"`)
		c.Send(req).Expect(http.StatusOK)
	}
	before := c.Get(mapPath(cityParams())).Header().Get("ETag")
	a.SeedRecommendation(testapp.RecommendationSeed{Place: a.Place, Author: a.Member, Attitude: domain.AttitudeAvoid})
	if after := c.Get(mapPath(cityParams())).Header().Get("ETag"); after == before {
		t.Error("the ETag did not change with the counts")
	}
	card := c.Get(cardPath(a.Place.GetString(store.FieldUUID))).Expect(http.StatusOK)
	if cc := card.Header().Get("Cache-Control"); cc != "no-store" || card.Header().Get("ETag") != "" {
		t.Errorf("card: Cache-Control %q, ETag %q", cc, card.Header().Get("ETag"))
	}
	bad := c.Get(mapPath(url.Values{api.MapParamCity: {"nowhere"}})).Expect(http.StatusBadRequest)
	if cc := bad.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("error: Cache-Control %q", cc)
	}
}

// TestMapArea: the city is required and must exist; the bbox is clipped to the city, refused when it does not overlap
// it or is malformed, and narrows the places to it.
func TestMapArea(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	c := a.As(testapp.Member)
	code := func(r *testapp.Response) string { return testapp.Decode[api.Error](r).Code }
	if got := code(c.Get(api.MapPlacesPath).Expect(http.StatusBadRequest)); got != api.CodeInvalidRequest {
		t.Errorf("no city: %q", got)
	}
	if got := code(c.Get(mapPath(url.Values{api.MapParamCity: {"nowhere"}})).Expect(http.StatusBadRequest)); got != api.CodeMapUnknownCity {
		t.Errorf("unknown city: %q", got)
	}
	if got := code(c.Get(api.SignaturesPath + "?city=nowhere").Expect(http.StatusBadRequest)); got != api.CodeMapUnknownCity {
		t.Errorf("signatures of an unknown city: %q", got)
	}
	for _, bbox := range []string{"8,1,9,2", "-10,-10,7.0,0.3", "7.2,0.31,7.3,0.37"} {
		r := c.Get(mapPath(cityParams(api.MapParamBBox, bbox))).Expect(http.StatusBadRequest)
		if got := code(r); got != api.CodeMapOutsideCity {
			t.Errorf("bbox %s: %q", bbox, got)
		}
	}
	for _, bbox := range []string{"7,0.3,7.1", "a,b,c,d", "7.1,0.3,7.0,0.4", "7,0.3,7,0.4", "-200,0,7,1", "NaN,0,7,1",
		"7,0.3,7.1,0.4,1"} {
		r := c.Get(mapPath(cityParams(api.MapParamBBox, bbox))).Expect(http.StatusBadRequest)
		if got := code(r); got != api.CodeInvalidRequest {
			t.Errorf("bbox %s: %q", bbox, got)
		}
	}
	// Larger than the city: clipped, both places.
	all := testapp.Decode[[]api.MapPlace](c.Get(mapPath(cityParams(api.MapParamBBox, "0,0,10,1"))).Expect(http.StatusOK))
	if len(all) != 2 {
		t.Errorf("large box: %d places", len(all))
	}
	// Place is at 7.06,0.34, PseudonymPlace at 7.065,0.345.
	some := testapp.Decode[[]api.MapPlace](c.Get(mapPath(cityParams(api.MapParamBBox, "7.05,0.33,7.062,0.342"))).Expect(http.StatusOK))
	if got := placeUUIDs(some); !slices.Equal(got, sortedUUIDs(a.Place)) {
		t.Errorf("small box: %v", got)
	}
	// A place outside the city is not on its map, even in a box that holds it.
	a.SeedRecommendation(testapp.RecommendationSeed{Author: a.Member,
		Place: a.SeedPlace(testapp.PlaceSeed{Name: "Test Faraway Inn", Lat: 0.5, Lon: 7.5})})
	if n := len(testapp.Decode[[]api.MapPlace](c.Get(mapPath(cityParams(api.MapParamBBox, "0,0,10,1"))))); n != 2 {
		t.Errorf("with a place outside the city: %d places", n)
	}
	for _, bad := range []url.Values{
		cityParams(api.MapParamSlots, "brunch"),
		cityParams(api.MapParamCategory, "casino"),
		cityParams(api.MapParamCuisine, "Not A Cuisine"),
		cityParams(api.MapParamMustTry, "yes"),
		cityParams(api.MapParamNoAvoid, "2"),
		cityParams(api.MapParamBy, strings.Repeat("x", 201)),
	} {
		if got := code(c.Get(mapPath(bad)).Expect(http.StatusBadRequest)); got != api.CodeInvalidRequest {
			t.Errorf("%v: %q", bad, got)
		}
	}
}

// countsFixture is the seeded instance with a place where several members recommend, privately, hidden and deleted
// too.
type countsFixture struct {
	*testapp.App
	place, hidden, other *core.Record
}

func newCountsFixture(t *testing.T) *countsFixture {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	f := &countsFixture{App: a}
	f.other = seedMember(t, a, "test_other", "Test Other", "Test Other Pen")
	f.place = a.SeedPlace(testapp.PlaceSeed{Name: "Test Count Kitchen", Lat: 0.33, Lon: 7.04, Category: "cafe",
		Cuisine: []string{"italian"}})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: f.place, Author: a.Member,
		Attitude: domain.AttitudeMustTry, TimeSlots: []string{"lunch"}})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: f.place, Author: a.Author,
		Attitude: domain.AttitudeAvoid, Visibility: domain.VisibilityPrivate, TimeSlots: []string{"breakfast"}})
	f.hidden = a.SeedRecommendation(testapp.RecommendationSeed{Place: f.place, Author: f.other,
		Attitude: domain.AttitudeGood, TimeSlots: []string{"dinner"}, HiddenKind: store.HiddenByMaintainer,
		HiddenReason: "Test reason", HiddenBy: a.Maintainer})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: f.place, Author: a.Maintainer,
		Attitude: domain.AttitudeOK, Deleted: true, TimeSlots: []string{"late_night"}})
	return f
}

// TestMapCounts: the counts are numbers of the recommendations the caller may see that are not hidden — not the
// others' private ones, not hidden ones even for maintainers (who see them on the card), never deleted ones — and the
// time slots come from the same recommendations. The card's counts equal the map's.
func TestMapCounts(t *testing.T) {
	f := newCountsFixture(t)
	for id, want := range map[testapp.Identity]struct {
		counts api.PlaceCounts
		slots  []string
		recs   int
	}{
		testapp.Member:     {api.PlaceCounts{MustTry: 1}, []string{"lunch"}, 1},
		testapp.Author:     {api.PlaceCounts{MustTry: 1, Avoid: 1}, []string{"breakfast", "lunch"}, 2},
		testapp.Maintainer: {api.PlaceCounts{MustTry: 1}, []string{"lunch"}, 2},
	} {
		c := f.As(id)
		places := testapp.Decode[[]api.MapPlace](c.Get(mapPath(cityParams())).Expect(http.StatusOK))
		i := slices.IndexFunc(places, func(p api.MapPlace) bool { return p.UUID == f.place.GetString(store.FieldUUID) })
		if i < 0 {
			t.Fatalf("%s: the place is missing", id)
		}
		p := places[i]
		if p.Counts != want.counts || !slices.Equal(p.TimeSlots, want.slots) || p.Category != "cafe" ||
			p.Precision != "exact" || p.Lat != 0.33 || p.Lon != 7.04 || p.Name != "Test Count Kitchen" {
			t.Errorf("%s: %+v", id, p)
		}
		card := testapp.Decode[api.PlaceCard](c.Get(cardPath(p.UUID)).Expect(http.StatusOK))
		if card.Counts != want.counts || len(card.Recommendations) != want.recs {
			t.Errorf("%s: card counts %+v with %d recommendations", id, card.Counts, len(card.Recommendations))
		}
		if id == testapp.Maintainer {
			hidden := card.Recommendations[slices.IndexFunc(card.Recommendations, func(r api.RecommendationView) bool {
				return r.UUID == f.hidden.GetString(store.FieldUUID)
			})]
			if hidden.Hidden == nil || hidden.Hidden.Kind != store.HiddenByMaintainer {
				t.Errorf("the maintainer's view of the hidden recommendation: %+v", hidden)
			}
		}
	}
	// The hidden recommendation's author sees it on the card and the place on the map, but it does not count.
	other := f.SignedIn(f.other)
	card := testapp.Decode[api.PlaceCard](other.Get(cardPath(f.place.GetString(store.FieldUUID))).Expect(http.StatusOK))
	if card.Counts != (api.PlaceCounts{MustTry: 1}) || len(card.Recommendations) != 2 {
		t.Errorf("the hidden recommendation's author: %+v", card)
	}
}

// TestMapSignatures: the filter candidates are the signatures of the recommendations the caller may see, sorted, as
// plain strings; a main name does not find its member's pseudonym recommendations and the reverse; the main name of a
// member who left is neither listed nor found (nor is an empty name), their pseudonym still is.
func TestMapSignatures(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	gone := seedMember(t, a, "test_gone", "Test Gone", "Test Gone Pen")
	gonePlace := a.SeedPlace(testapp.PlaceSeed{Name: "Test Gone Grill", Lat: 0.32, Lon: 7.03})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: gonePlace, Author: gone})
	gonePenPlace := a.SeedPlace(testapp.PlaceSeed{Name: "Test Gone Pen Grill", Lat: 0.321, Lon: 7.031})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: gonePenPlace, Author: gone,
		DisplayAs: domain.SignAsPseudonym})
	set(t, a, gone, map[string]any{"left_at": "2026-02-01 00:00:00.000Z"})
	maintainerPlace := a.SeedPlace(testapp.PlaceSeed{Name: "Test Quiet Cafe", Lat: 0.33, Lon: 7.03})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: maintainerPlace, Author: a.Maintainer,
		Visibility: domain.VisibilityPrivate})

	for id, want := range map[testapp.Identity][]string{
		testapp.Member:     {"Test Author", "Test Gone Pen", "Test Pen Name"},
		testapp.Author:     {"Test Author", "Test Gone Pen", "Test Pen Name"},
		testapp.Maintainer: {"Test Author", "Test Gone Pen", "Test Maintainer", "Test Pen Name"},
	} {
		got := testapp.Decode[[]string](a.As(id).Get(signaturesPath()).Expect(http.StatusOK))
		if !slices.Equal(got, want) {
			t.Errorf("%s: %q, want %q", id, got, want)
		}
	}

	c := a.As(testapp.Member)
	for _, tc := range []struct {
		by   []string
		want []*core.Record
	}{
		{[]string{"Test Author"}, []*core.Record{a.Place}},
		{[]string{"Test Pen Name"}, []*core.Record{a.PseudonymPlace}},
		{[]string{"Test Author", "Test Pen Name"}, []*core.Record{a.Place, a.PseudonymPlace}},
		{[]string{"Test Gone"}, nil},
		{[]string{"Test Gone Pen"}, []*core.Record{gonePenPlace}},
		{[]string{"test author"}, nil},
		{[]string{"Test Maintainer"}, nil}, // only a private recommendation, which the member may not see
		{[]string{"Test Login"}, nil},
		// An empty name is no filter, so it cannot select the recommendations of members who left.
		{[]string{""}, []*core.Record{a.Place, a.PseudonymPlace, gonePlace, gonePenPlace}},
	} {
		q := cityParams()
		for _, name := range tc.by {
			q.Add(api.MapParamBy, name)
		}
		got := placeUUIDs(testapp.Decode[[]api.MapPlace](c.Get(mapPath(q)).Expect(http.StatusOK)))
		if want := sortedUUIDs(tc.want...); !slices.Equal(got, want) {
			t.Errorf("by %q: %v, want %v", tc.by, got, want)
		}
	}
	// The card shows the left member's main-name recommendation without a name.
	card := testapp.Decode[api.PlaceCard](c.Get(cardPath(gonePlace.GetString(store.FieldUUID))).Expect(http.StatusOK))
	if r := card.Recommendations[0]; r.DisplayName != "" || !r.AuthorLeft {
		t.Errorf("left member's recommendation: %+v", r)
	}
}

// TestMapNoMemberData: no response of the map, the card or the signatures carries a member's uuid, login name or
// internal ID, the signature kind, or an external link next to a pseudonym; a main-name recommendation has its link.
func TestMapNoMemberData(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	var bodies []string
	for _, id := range []testapp.Identity{testapp.Member, testapp.Author, testapp.Maintainer} {
		c := a.As(id)
		bodies = append(bodies, c.Get(mapPath(cityParams())).Expect(http.StatusOK).Body.String(),
			c.Get(signaturesPath()).Expect(http.StatusOK).Body.String())
		for _, place := range []*core.Record{a.Place, a.PseudonymPlace, a.PrivatePlace} {
			r := c.Get(cardPath(place.GetString(store.FieldUUID)))
			if r.Code == http.StatusOK {
				bodies = append(bodies, r.Body.String())
			}
		}
		pen := testapp.Decode[api.PlaceCard](c.Get(cardPath(a.PseudonymPlace.GetString(store.FieldUUID))))
		if r := pen.Recommendations[0]; r.DisplayName != "Test Pen Name" || len(r.ExternalLinks) != 0 {
			t.Errorf("%s: pseudonym recommendation %+v", id, r)
		}
		main := testapp.Decode[api.PlaceCard](c.Get(cardPath(a.Place.GetString(store.FieldUUID))))
		if r := main.Recommendations[0]; len(r.ExternalLinks) != 1 || r.ExternalLinks[0].URL != testapp.AuthorLink {
			t.Errorf("%s: main-name recommendation %+v", id, r)
		}
	}
	var forbidden []string
	for _, m := range []*core.Record{a.Member, a.Author, a.Maintainer} {
		forbidden = append(forbidden, m.GetString(store.FieldUUID), m.GetString("login_name"))
	}
	forbidden = append(forbidden, `"author"`, `"authorId"`, `"display_as"`, `"displayAs"`, `"editor"`, `"reporter"`,
		domain.SignAsPseudonym)
	for _, body := range bodies {
		for _, s := range forbidden {
			if strings.Contains(body, s) {
				t.Errorf("a response contains %q: %s", s, body)
			}
		}
	}
	a.CheckNoInternalIDs()
}

// TestPlaceCardFields: the card has the place's fields; a landmark place appears only for those who may see it, a
// landmark text as stored; status and its confirmation only when set.
func TestPlaceCardFields(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	privateUUID := a.PrivatePlace.GetString(store.FieldUUID)
	set(t, a, a.Place, map[string]any{"aliases": []string{"Test Alias One"}, "position_note": "Test note\nline two",
		"cuisine": []string{"dumpling", "chinese"}, "landmark": map[string]any{"type": "place", "place_uuid": privateUUID},
		"status": "open", "status_confirmed_at": "2026-03-01 00:00:00.000Z", "precision": "approx"})
	set(t, a, a.PseudonymPlace, map[string]any{"landmark": map[string]any{"type": "text", "text": "Test landmark text"}})

	member := testapp.Decode[api.PlaceCard](a.As(testapp.Member).Get(cardPath(a.Place.GetString(store.FieldUUID))).
		Expect(http.StatusOK))
	if member.Landmark != nil || !slices.Equal(member.Aliases, []string{"Test Alias One"}) ||
		member.PositionNote != "Test note\nline two" || !slices.Equal(member.Cuisine, []string{"dumpling", "chinese"}) ||
		member.Status != "open" || member.StatusConfirmedAt == nil || member.Precision != "approx" ||
		member.Lat != 0.34 || member.Lon != 7.06 {
		t.Errorf("member: %+v", member)
	}
	author := testapp.Decode[api.PlaceCard](a.As(testapp.Author).Get(cardPath(a.Place.GetString(store.FieldUUID))).
		Expect(http.StatusOK))
	if author.Landmark == nil || author.Landmark.Type != api.LandmarkKindPlace || author.Landmark.Place == nil ||
		author.Landmark.Place.UUID != privateUUID || author.Landmark.Place.Name != "Test Secret Bakery" {
		t.Errorf("author's landmark: %+v", author.Landmark)
	}
	pen := testapp.Decode[api.PlaceCard](a.As(testapp.Member).Get(cardPath(a.PseudonymPlace.GetString(store.FieldUUID))).
		Expect(http.StatusOK))
	if pen.Landmark == nil || pen.Landmark.Type != api.LandmarkKindText || pen.Landmark.Text != "Test landmark text" ||
		pen.Landmark.Place != nil || pen.Status != "" || pen.StatusConfirmedAt != nil || pen.Aliases == nil ||
		pen.Cuisine == nil {
		t.Errorf("text landmark card: %+v", pen)
	}
	// A deleted place is gone for everyone, its author included.
	if err := store.SoftDelete(a, a.Place); err != nil {
		t.Fatal(err)
	}
	a.As(testapp.Author).Get(cardPath(a.Place.GetString(store.FieldUUID))).Expect(http.StatusNotFound)
}

// TestMapFiltersMatchBruteForce compares the map data with a recomputation from all records through the rules of
// internal/domain, for random made-up data and random filter combinations, as several members: which places appear,
// their counts and time slots, and the signatures.
func TestMapFiltersMatchBruteForce(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	rng := rand.New(rand.NewPCG(27, 2026)) // #nosec G404 -- reproducible made-up test data
	members := []*core.Record{a.Member, a.Author, a.Maintainer}
	for i := range 5 {
		pen := ""
		if i%2 == 0 {
			pen = fmt.Sprintf("Test Pen %d", i)
		}
		members = append(members, seedMember(t, a, fmt.Sprintf("test_m%d", i), fmt.Sprintf("Test Member %d", i), pen))
	}
	categories := []string{"", "restaurant", "cafe", "bakery"}
	cuisines := []string{"chinese", "italian", "sichuan", "noodle"}
	slots := []string{"breakfast", "lunch", "afternoon", "dinner", "late_night"}
	for i := range 40 {
		var cuisine []string
		for _, c := range cuisines {
			if rng.IntN(3) == 0 {
				cuisine = append(cuisine, c)
			}
		}
		place := a.SeedPlace(testapp.PlaceSeed{Name: fmt.Sprintf("Test Place %02d", rng.IntN(30)),
			Lat: 0.311 + rng.Float64()*0.058, Lon: 7.021 + rng.Float64()*0.078,
			Category: categories[rng.IntN(len(categories))], Cuisine: cuisine, Deleted: i%17 == 16})
		for _, m := range members {
			if rng.IntN(3) != 0 {
				continue
			}
			seed := testapp.RecommendationSeed{Place: place, Author: m,
				Attitude: domain.Attitudes[rng.IntN(len(domain.Attitudes))]}
			if m.GetString("pseudonym") != "" && rng.IntN(2) == 0 {
				seed.DisplayAs = domain.SignAsPseudonym
			}
			switch rng.IntN(8) {
			case 0:
				seed.Visibility = domain.VisibilityPrivate
			case 1:
				seed.HiddenKind, seed.HiddenReason = store.HiddenByReports, ""
			case 2:
				seed.Deleted = true
			case 3:
				seed.Visibility = domain.VisibilityFederated
			}
			seed.TimeSlots = []string{}
			for _, s := range slots {
				if rng.IntN(4) == 0 {
					seed.TimeSlots = append(seed.TimeSlots, s)
				}
			}
			a.SeedRecommendation(seed)
		}
	}
	// One member leaves: their main name disappears, their pseudonym stays.
	set(t, a, members[3], map[string]any{"left_at": "2026-02-01 00:00:00.000Z"})

	type filter struct {
		slots, categories, cuisines, by []string
		mustTry, noAvoid                bool
		bbox                            *[4]float64
	}
	pick := func(values []string) []string {
		var out []string
		for _, v := range values {
			if rng.IntN(4) == 0 {
				out = append(out, v)
			}
		}
		return out
	}
	filters := []filter{{}, {mustTry: true}, {noAvoid: true}, {mustTry: true, noAvoid: true},
		{by: []string{"Test Author"}}, {by: []string{"Test Pen Name", "Test Pen 0"}}, {by: []string{"Test Member 1"}},
		{slots: []string{"lunch", "dinner"}}, {categories: []string{"cafe"}}, {cuisines: []string{"sichuan", "noodle"}}}
	for range 40 {
		f := filter{slots: pick(slots), categories: pick(categories[1:]), cuisines: pick(cuisines),
			mustTry: rng.IntN(3) == 0, noAvoid: rng.IntN(3) == 0}
		if rng.IntN(3) == 0 {
			f.by = []string{"Test Author", "Test Pen Name", "Test Member 0", "Test Pen 2", "Test Member 3",
				"Test Pen 4"}[rng.IntN(6):][:1]
		}
		if rng.IntN(3) == 0 {
			minLon, minLat := 7.0+rng.Float64()*0.08, 0.3+rng.Float64()*0.06
			f.bbox = &[4]float64{minLon, minLat, minLon + 0.04, minLat + 0.03}
		}
		filters = append(filters, f)
	}

	allRecs, err := store.Query(a, store.Recommendations).IncludeDeleted().
		OrderBy(store.FieldCreated+" DESC", store.FieldUUID+" DESC").All()
	if err != nil {
		t.Fatal(err)
	}
	states, err := store.RecommendationStates(a, allRecs)
	if err != nil {
		t.Fatal(err)
	}
	allPlaces, err := store.Query(a, store.Places).IncludeDeleted().All()
	if err != nil {
		t.Fatal(err)
	}
	authors := map[string]domain.Author{}
	everyone, err := store.Query(a, store.Members).IncludeDeleted().All()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range everyone {
		authors[m.Id] = domain.Author{DisplayName: m.GetString("display_name"), Pseudonym: m.GetString("pseudonym"),
			Left: !m.GetDateTime("left_at").IsZero()}
	}

	type expected struct {
		place  api.MapPlace
		names  []string
		cuis   []string
		inCity bool
	}
	expect := func(v domain.Viewer) map[string]expected {
		out := map[string]expected{}
		for _, p := range allPlaces {
			var all []domain.Recommendation
			var counted []domain.CountedRecommendation
			slotSet := map[string]bool{}
			var names []string
			seen := map[string]bool{}
			for i, r := range allRecs {
				if r.GetString("place") != p.Id {
					continue
				}
				all = append(all, states[i])
				if !domain.CanSeeRecommendation(v, states[i]) || states[i].Hidden || seen[states[i].AuthorID] {
					continue
				}
				seen[states[i].AuthorID] = true
				counted = append(counted, domain.CountedRecommendation{Recommendation: states[i],
					Attitude: r.GetString("attitude")})
				for _, s := range r.GetStringSlice("time_slots") {
					slotSet[s] = true
				}
				if n := domain.ResolveSignature(r.GetString("display_as"), authors[states[i].AuthorID]).DisplayName; n != "" {
					names = append(names, n)
				}
			}
			if !domain.CanSeePlace(v, store.IsDeleted(p), all) {
				continue
			}
			c := domain.CountAttitudes(v, counted)
			mp := api.MapPlace{UUID: p.GetString(store.FieldUUID), Name: p.GetString("name"), Lat: p.GetFloat("lat"),
				Lon: p.GetFloat("lon"), Precision: p.GetString("precision"), Category: p.GetString("category"),
				Counts: api.PlaceCounts{MustTry: c.MustTry, Good: c.Good, OK: c.OK, Avoid: c.Avoid}, TimeSlots: []string{}}
			for _, s := range slots {
				if slotSet[s] {
					mp.TimeSlots = append(mp.TimeSlots, s)
				}
			}
			var cuis []string
			_ = p.UnmarshalJSONField("cuisine", &cuis)
			out[mp.UUID] = expected{place: mp, names: names, cuis: cuis,
				inCity: mp.Lon >= 7.02 && mp.Lon <= 7.1 && mp.Lat >= 0.31 && mp.Lat <= 0.37}
		}
		return out
	}
	matches := func(e expected, f filter) bool {
		p := e.place
		anyOf := func(want, have []string) bool {
			return len(want) == 0 || slices.ContainsFunc(want, func(w string) bool { return slices.Contains(have, w) })
		}
		box := [4]float64{7.02, 0.31, 7.1, 0.37}
		if f.bbox != nil {
			box = [4]float64{max(box[0], f.bbox[0]), max(box[1], f.bbox[1]), min(box[2], f.bbox[2]), min(box[3], f.bbox[3])}
		}
		return p.Lon >= box[0] && p.Lon <= box[2] && p.Lat >= box[1] && p.Lat <= box[3] &&
			anyOf(f.slots, p.TimeSlots) && anyOf(f.categories, []string{p.Category}) && anyOf(f.cuisines, e.cuis) &&
			anyOf(f.by, e.names) && (!f.mustTry || p.Counts.MustTry > 0) && (!f.noAvoid || p.Counts.Avoid == 0)
	}

	viewers := map[string]*core.Record{"member": a.Member, "author": a.Author, "maintainer": a.Maintainer,
		"other": members[4]}
	for name, m := range viewers {
		t.Run(name, func(t *testing.T) {
			v := api.MemberViewer(m)
			c := a.SignedIn(m)
			want := expect(v)
			var wantNames []string
			for _, e := range want {
				if e.inCity {
					for _, n := range e.names {
						if !slices.Contains(wantNames, n) {
							wantNames = append(wantNames, n)
						}
					}
				}
			}
			slices.SortFunc(wantNames, func(x, y string) int {
				return cmp.Or(strings.Compare(strings.ToLower(x), strings.ToLower(y)), strings.Compare(x, y))
			})
			if got := testapp.Decode[[]string](c.Get(signaturesPath()).Expect(http.StatusOK)); !slices.Equal(got, wantNames) {
				t.Errorf("signatures %q, want %q", got, wantNames)
			}
			nonEmpty := 0
			for _, f := range filters {
				q := cityParams()
				q[api.MapParamSlots] = []string{strings.Join(f.slots, ",")}
				q[api.MapParamCategory] = f.categories
				q[api.MapParamCuisine] = f.cuisines
				q[api.MapParamBy] = f.by
				if f.mustTry {
					q.Set(api.MapParamMustTry, "1")
				}
				if f.noAvoid {
					q.Set(api.MapParamNoAvoid, "true")
				}
				if f.bbox != nil {
					q.Set(api.MapParamBBox, fmt.Sprintf("%g,%g,%g,%g", f.bbox[0], f.bbox[1], f.bbox[2], f.bbox[3]))
				}
				r := c.Get(mapPath(q))
				if f.bbox != nil && r.Code == http.StatusBadRequest {
					continue // a random box outside the city
				}
				got := testapp.Decode[[]api.MapPlace](r.Expect(http.StatusOK))
				var wantPlaces []api.MapPlace
				for _, e := range want {
					if matches(e, f) {
						wantPlaces = append(wantPlaces, e.place)
					}
				}
				slices.SortFunc(wantPlaces, func(x, y api.MapPlace) int {
					return cmp.Or(strings.Compare(x.Name, y.Name), strings.Compare(x.UUID, y.UUID))
				})
				if len(wantPlaces) > 0 {
					nonEmpty++
				}
				if !slices.EqualFunc(got, wantPlaces, func(x, y api.MapPlace) bool {
					return x.UUID == y.UUID && x.Name == y.Name && x.Lat == y.Lat && x.Lon == y.Lon &&
						x.Precision == y.Precision && x.Category == y.Category && x.Counts == y.Counts &&
						slices.Equal(x.TimeSlots, y.TimeSlots)
				}) {
					t.Errorf("filter %+v:\n got %+v\nwant %+v", f, got, wantPlaces)
				}
			}
			if nonEmpty < len(filters)/3 {
				t.Errorf("only %d of %d filters select any place: the data does not exercise them", nonEmpty, len(filters))
			}
			// And every place's card agrees with the map.
			for uuid, e := range want {
				if !e.inCity {
					continue
				}
				card := testapp.Decode[api.PlaceCard](c.Get(cardPath(uuid)).Expect(http.StatusOK))
				if card.Counts != e.place.Counts {
					t.Errorf("card %s: counts %+v, map %+v", uuid, card.Counts, e.place.Counts)
				}
			}
		})
	}
}

// TestPlacesNearbyNotShadowed: once places.go (T026) registers GET /api/osfm/places/nearby, a request to it reaches
// that route, not the place card's {uuid} (which would answer 404). Until then there is nothing to check; the
// router's precedence itself is tested in internal/api (TestPlaceRoutesPrecedence).
func TestPlacesNearbyNotShadowed(t *testing.T) {
	app, seeded, _, osfmRoutes, h := bootForScan(t)
	const nearby = "/api/osfm/places/nearby"
	if !slices.ContainsFunc(osfmRoutes, func(r idscan.Route) bool { return r.String() == "GET "+nearby }) {
		t.Skip("GET " + nearby + " is not registered yet")
	}
	header, _ := sessionHeader(t, app, seeded.Member)
	req := httptest.NewRequest(http.MethodGet, nearby+"?lat=31.23&lon=121.47&name=Test", nil)
	req.Header = header
	req.RemoteAddr = idscan.ClientAddr()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Errorf("GET %s: 404 %s", nearby, rec.Body.String())
	}
}
