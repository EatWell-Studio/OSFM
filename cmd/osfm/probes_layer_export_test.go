package main

import (
	"net/http"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
)

// The internal ID scan's probes of the per-layer exports (internal/api/layer_export.go, T041). See registerProbes.
// The seeded city is testcity; the files are scanned for the seeded records' internal IDs like any other response.
func init() {
	registerProbes(probeSet{probes: map[string][]idscan.Request{
		"GET " + api.ExportPlacesPath: {
			{Path: api.ExportPlacesPath, Status: http.StatusOK},
			{Path: api.ExportPlacesPath + "?city=testcity", Status: http.StatusOK},
			{Path: api.ExportPlacesPath + "?city=nowhere", Status: http.StatusBadRequest},
		},
		"GET " + api.ExportRecommendationsPath: {
			{Path: api.ExportRecommendationsPath, Status: http.StatusOK},
			{Path: api.ExportRecommendationsPath + "?city=testcity", Status: http.StatusOK},
			{Path: api.ExportRecommendationsPath + "?city=nowhere", Status: http.StatusBadRequest},
		},
	}})
}
