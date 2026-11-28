package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// placesNearbyPath is the route of places.go (T026) that shares its prefix with PlaceCardPath.
const placesNearbyPath = "/api/osfm/places/nearby"

// TestPlaceRoutesPrecedence: PocketBase's router (net/http's ServeMux) sends /api/osfm/places/nearby to its own route,
// never to PlaceCardPath's {uuid}, whichever of the two is registered first; /api/osfm/places itself and a uuid go to
// the map routes. This must keep holding once the routes of places.go are registered next to these.
func TestPlaceRoutesPrecedence(t *testing.T) {
	for _, nearbyFirst := range []bool{true, false} {
		r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
			return &core.RequestEvent{Event: router.Event{Response: w, Request: req}}, nil
		})
		marker := func(name string) func(e *core.RequestEvent) error {
			return func(e *core.RequestEvent) error { return e.String(http.StatusOK, name) }
		}
		nearby := func() { r.GET(placesNearbyPath, marker("nearby")) }
		if nearbyFirst {
			nearby()
		}
		r.GET(MapPlacesPath, marker("map"))
		r.GET(PlaceCardPath, marker("card"))
		r.POST(MapPlacesPath, marker("create")) // as places.go registers it
		if !nearbyFirst {
			nearby()
		}
		mux, err := r.BuildMux()
		if err != nil {
			t.Fatal(err)
		}
		for path, want := range map[string]string{
			placesNearbyPath + "?lat=1&lon=2&name=x":                "nearby",
			MapPlacesPath + "?city=x":                               "map",
			"/api/osfm/places/0190a1b2-0000-7000-8000-00000000e001": "card",
		} {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Body.String() != want {
				t.Errorf("nearby registered first %v: GET %s went to %q, want %q", nearbyFirst, path, rec.Body.String(), want)
			}
		}
	}
}

func TestParseMapBBox(t *testing.T) {
	b, err := parseMapBBox(" 7.02, 0.31 ,7.1,0.37")
	if err != nil || b != (mapBox{7.02, 0.31, 7.1, 0.37}) {
		t.Errorf("%v, %v", b, err)
	}
	for _, s := range []string{"", "1,2,3", "1,2,3,4,5", "1,2,1,3", "1,3,2,3", "2,1,1,2", "-181,0,1,1", "0,0,181,1",
		"0,-91,1,1", "0,0,1,91", "Inf,0,1,1", "0x1,0,1,1;", "a,0,1,1"} {
		if _, err := parseMapBBox(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
	city := mapBox{7, 0, 8, 1}
	if got, ok := city.intersect(mapBox{7.5, -1, 9, 0.5}); !ok || got != (mapBox{7.5, 0, 8, 0.5}) {
		t.Errorf("intersect: %v %v", got, ok)
	}
	if _, ok := city.intersect(mapBox{8.1, 0, 9, 1}); ok {
		t.Error("disjoint boxes overlap")
	}
}

func TestParseMapFilter(t *testing.T) {
	q := url.Values{
		MapParamSlots:    {"lunch,dinner", "lunch", ""},
		MapParamCategory: {"cafe"},
		MapParamCuisine:  {"sichuan, noodle"},
		MapParamBy:       {"Name, with comma", "", "Name, with comma", "Other"},
		MapParamMustTry:  {"1"},
		MapParamNoAvoid:  {"false"},
	}
	f, err := parseMapFilter(q)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.slots, []string{"lunch", "dinner"}) || !slices.Equal(f.categories, []string{"cafe"}) ||
		!slices.Equal(f.cuisines, []string{"sichuan", "noodle"}) ||
		!slices.Equal(f.names, []string{"Name, with comma", "Other"}) || !f.mustTry || f.noAvoid {
		t.Errorf("%+v", f)
	}
	if f, err := parseMapFilter(url.Values{}); err != nil || f.slots != nil || f.names != nil || f.mustTry {
		t.Errorf("empty: %+v %v", f, err)
	}
	tooMany := url.Values{}
	for i := range maxMapByNames + 1 {
		tooMany.Add(MapParamBy, fmt.Sprintf("name %d", i))
	}
	for _, bad := range []url.Values{
		{MapParamSlots: {"brunch"}}, {MapParamCategory: {"Cafe"}}, {MapParamCuisine: {"chinese;"}},
		{MapParamMustTry: {"yes"}}, {MapParamNoAvoid: {"2"}}, tooMany,
	} {
		if _, err := parseMapFilter(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestETagMatches(t *testing.T) {
	for header, want := range map[string]bool{
		`"abc"`: true, `W/"abc"`: true, `"x", "abc"`: true, `*`: true, ``: false, `"ab"`: false, `abc`: false,
	} {
		if got := etagMatches(header, `"abc"`); got != want {
			t.Errorf("%q: %v", header, got)
		}
	}
}
