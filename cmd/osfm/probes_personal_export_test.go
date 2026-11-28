package main

import (
	"net/http"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
)

// The internal ID scan's probes of the personal data export (internal/api/personal_export.go, T040). See
// registerProbes.
func init() {
	registerProbes(probeSet{
		probes: map[string][]idscan.Request{
			// Every member downloads their own export: the seeded member has invites, reports and notices of their
			// own in the scan's seed, and the export names records of other collections by uuid only.
			"GET " + api.PersonalExportPath: {
				{Path: api.PersonalExportPath, Status: http.StatusOK},
			},
		},
	})
}
