package main

import (
	"net/http"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
)

// The internal ID scan's probes of the instance settings (internal/api/settings.go). See registerProbes.
func init() {
	registerProbes(probeSet{
		probes: map[string][]idscan.Request{
			// Members get the name, the cities and whether search is on; maintainers all the settings.
			"GET " + api.InstancePath: {
				{Path: api.InstancePath, Status: http.StatusOK},
			},
			// Maintainers only.
			"POST " + api.InstancePath: {
				{Method: http.MethodPost, Path: api.InstancePath, Header: csrfHeader, Status: http.StatusOK,
					Body: `{"name":"Test Circle","cities":[{"slug":"testcity","name":"Test City","bbox":[121.3,31.1,121.6,31.4],"center":[121.47,31.23]}]}`},
				{Method: http.MethodPost, Path: api.InstancePath, Header: csrfHeader, Status: http.StatusOK,
					Body: `{"reportThreshold":3,"inviteCooldownDays":30,"geocoderEnabled":true,"geocoderContact":"test@example.test"}`},
				{Method: http.MethodPost, Path: api.InstancePath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"name":"","cities":[]}`},
				{Method: http.MethodPost, Path: api.InstancePath, Header: csrfHeader, Status: http.StatusBadRequest,
					Body: `{"reportThreshold":0}`},
				{Method: http.MethodPost, Path: api.InstancePath, Header: csrfHeader, Body: `{`,
					Status: http.StatusBadRequest},
			},
		},
		maintainerOnly: []string{"POST " + api.InstancePath},
	})
}
