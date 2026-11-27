package main

import (
	"net/http"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
)

// The internal ID scan's probes of place-name search (internal/api/geocode.go, T039). See registerProbes.
//
// The scan's instance uses the default upstream URLs, the public Photon and Nominatim, which no test may call: the
// probes stop at the request checks before any upstream request. A search that succeeds returns only what the
// upstream sent (names, labels, positions), never a record; internal/api's tests cover it with fake upstreams.
func init() {
	registerProbes(probeSet{
		probes: map[string][]idscan.Request{
			"GET " + api.GeocodePath: {
				{Path: api.GeocodePath, Status: http.StatusBadRequest},
				{Path: api.GeocodePath + "?q=test&city=no-such-city", Status: http.StatusBadRequest},
			},
		},
	})
}
