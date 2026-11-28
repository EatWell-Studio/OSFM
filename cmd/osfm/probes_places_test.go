package main

import (
	"net/http"
	"strings"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
)

// The internal ID scan's probes of creating places and recommendations (internal/api/places.go, T026). See
// registerProbes. The coordinates are inside the seeded city (idscan.Seed).
func init() {
	recommend := strings.Replace(api.PlaceRecommendationsPath, "{uuid}", idscan.UnrecommendedPlaceUUID, 1)
	const rec = `{"attitude":"good","text":"Test probe text","timeSlots":["lunch"],` +
		`"sources":[{"url":"https://example.test/probe","kind":"chat","excerpt":"Test words"}]}`
	registerProbes(probeSet{
		probes: map[string][]idscan.Request{
			"POST " + api.PlacesPath: {
				{Method: http.MethodPost, Path: api.PlacesPath, Header: csrfHeader, Status: http.StatusCreated,
					Body: `{"name":"Test Probe Place","lat":31.2312345,"lon":121.4712345,"category":"cafe",` +
						`"cuisine":["coffee_shop"],"positionNote":"Test corner","landmark":{"type":"place",` +
						`"place":"` + idscan.UnrecommendedPlaceUUID + `"},"sourceCoord":{"crs":"wgs84","source":"search"},` +
						`"recommendation":` + rec + `}`},
				{Method: http.MethodPost, Path: api.PlacesPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"name":"Test Far Place","lat":48.1,"lon":11.5,"recommendation":` + rec + `}`},
				{Method: http.MethodPost, Path: api.PlacesPath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"name":"Test Place","lat":31.23,"lon":121.47,"category":"pharmacy","recommendation":` +
						rec + `}`},
				{Method: http.MethodPost, Path: api.PlacesPath, Header: csrfHeader, Body: `{`,
					Status: http.StatusBadRequest},
			},
			// Each identity adds a recommendation once, and then gets 409 with the uuid of their own.
			"POST " + api.PlaceRecommendationsPath: {
				{Method: http.MethodPost, Path: recommend, Header: csrfHeader, Body: rec, Status: http.StatusCreated},
				{Method: http.MethodPost, Path: recommend, Header: csrfHeader, Body: rec, Status: http.StatusConflict},
				{Method: http.MethodPost, Path: recommend, Header: csrfHeader, Body: `{"attitude":"good"}`,
					Status: http.StatusConflict},
				{Method: http.MethodPost, Header: csrfHeader, Body: rec, Status: http.StatusNotFound,
					Path: strings.Replace(api.PlaceRecommendationsPath, "{uuid}", "0190a1b2-0000-7000-8000-00000000ffff", 1)},
			},
			"GET " + api.PlacesNearbyPath: {
				{Path: api.PlacesNearbyPath + "?lat=31.235&lon=121.475&name=Test+Corner+Bakery", Status: http.StatusOK},
				{Path: api.PlacesNearbyPath + "?lat=31.23&lon=121.47&name=Test+Noodle+Bar", Status: http.StatusOK},
				{Path: api.PlacesNearbyPath + "?lat=x", Status: http.StatusBadRequest},
			},
		},
	})
}
