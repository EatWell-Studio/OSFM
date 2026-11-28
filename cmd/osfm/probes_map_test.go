package main

import (
	"net/http"
	"strings"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
)

// The internal ID scan's probes of the map data and the place card (internal/api/map.go, T027). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
func init() {
	card := func(uuid string) string { return strings.Replace(api.PlaceCardPath, "{uuid}", uuid, 1) }
	// The seeded city is testcity, 121.3,31.1,121.6,31.4; the seeded places are in it.
	registerProbes(probeSet{probes: map[string][]idscan.Request{
		"GET " + api.MapPlacesPath: {
			{Path: api.MapPlacesPath + "?city=testcity", Status: http.StatusOK},
			{Path: api.MapPlacesPath + "?city=testcity&bbox=121.4,31.2,121.5,31.3&slots=lunch,dinner&category=restaurant" +
				"&cuisine=chinese&by=Test%20Alias&by=Test%20Maintainer&mustTry=1&noAvoid=1", Status: http.StatusOK},
			{Path: api.MapPlacesPath + "?city=testcity&by=Test%20Alias", Status: http.StatusOK},
			{Path: api.MapPlacesPath + "?city=testcity&bbox=1,1,2,2", Status: http.StatusBadRequest},
			{Path: api.MapPlacesPath + "?city=testcity&bbox=x", Status: http.StatusBadRequest},
			{Path: api.MapPlacesPath + "?city=nowhere", Status: http.StatusBadRequest},
			{Path: api.MapPlacesPath, Status: http.StatusBadRequest},
		},
		"GET " + api.PlaceCardPath: {
			{Path: card(idscan.PlaceUUID), Status: http.StatusOK},
			{Path: card("0190a1b2-0000-7000-8000-00000000ffff"), Status: http.StatusNotFound},
			{Path: card("not-a-uuid"), Status: http.StatusNotFound},
		},
		"GET " + api.SignaturesPath: {
			{Path: api.SignaturesPath + "?city=testcity", Status: http.StatusOK},
			{Path: api.SignaturesPath + "?city=nowhere", Status: http.StatusBadRequest},
		},
	}})
}
