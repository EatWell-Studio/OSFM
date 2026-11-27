package main

import (
	"net/http"

	"github.com/EatWell-Studio/OSFM/internal/federation"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
)

// The internal ID scan's probes of the routes outside the OSFM API: the instance metadata, the tiles, the admin UI's
// root and the web app. See registerProbes.
func init() {
	registerProbes(probeSet{probes: map[string][]idscan.Request{
		"GET " + federation.WellKnownPath: {
			{Path: federation.WellKnownPath, Status: http.StatusOK},
		},
		// No tile archive in the test data directory: the tile handler reads only the tile files, never the database.
		"GET /tiles/{path...}": {
			{Path: "/tiles/", Status: http.StatusOK}, // the city index, empty here
			{Path: "/tiles/testcity.json", Status: http.StatusNotFound},
			{Path: "/tiles/testcity/12/3370/1665.mvt", Status: http.StatusNotFound},
			{Path: "/tiles/bad..name/1/1/1.mvt", Status: http.StatusNotFound},
		},
		// Registered by internal/boundary so that the admin UI's root goes through the middleware (404 on the main
		// listener) instead of net/http's own redirect.
		"GET /_": {
			{Path: "/_", Status: http.StatusNotFound},
			{Path: "/_/", Status: http.StatusNotFound},
		},
		"GET /{path...}": {
			{Path: "/", Status: http.StatusOK},
			{Path: "/index.html"},
			{Path: "/list", Status: http.StatusOK},
			{Path: "/some/client/route?next=%2Fme", Status: http.StatusOK},
			{Path: "/manifest.webmanifest"},
			{Path: "/assets/no-such-file.js", Status: http.StatusNotFound},
			{Path: "/favicon.ico", Status: http.StatusNotFound},
		},
	}})
}
