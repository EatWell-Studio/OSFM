package main

import (
	"net/http"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
)

// The internal ID scan's probes of the member's profile (internal/api/profile.go, T033). See registerProbes.
func init() {
	registerProbes(probeSet{
		probes: map[string][]idscan.Request{
			// Every member reads their own profile.
			"GET " + api.ProfilePath: {
				{Path: api.ProfilePath, Status: http.StatusOK},
			},
			// Every member changes their own profile. The seeded member and maintainer send the same bodies: the
			// links are theirs to set, "Test Reset" is a third member's main name.
			"POST " + api.ProfilePath: {
				{Method: http.MethodPost, Path: api.ProfilePath, Header: csrfHeader, Status: http.StatusOK,
					Body: `{"externalLinks":[{"url":"https://example.test/probe","label":"Test Link"}]}`},
				{Method: http.MethodPost, Path: api.ProfilePath, Header: csrfHeader, Status: http.StatusConflict,
					Body: `{"displayName":"Test Reset"}`},
				{Method: http.MethodPost, Path: api.ProfilePath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"displayName":""}`},
				{Method: http.MethodPost, Path: api.ProfilePath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"externalLinks":[{"url":"http://example.test/probe"}]}`},
				{Method: http.MethodPost, Path: api.ProfilePath, Header: csrfHeader, Body: `{`,
					Status: http.StatusBadRequest},
			},
		},
	})
}
