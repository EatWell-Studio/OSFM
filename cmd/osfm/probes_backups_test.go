package main

import (
	"net/http"
	"strings"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
)

// The internal ID scan's probes of backups (internal/api/backups.go, T042). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
//
// All of them are for maintainers. The scan's sessions are fresh, so creating reaches its handler and writes a real
// backup. Downloading and deleting are probed with names that match no file: a downloaded backup is the database,
// internal IDs and all, by design (its success is tested in backup_test.go).
func init() {
	named := func(pattern, name string) string { return strings.Replace(pattern, "{name}", name, 1) }
	const missing = "osfm_20000101000000.zip"
	registerProbes(probeSet{
		probes: map[string][]idscan.Request{
			"GET " + api.BackupsPath: {
				{Path: api.BackupsPath, Status: http.StatusOK},
			},
			"POST " + api.BackupsPath: {
				{Method: http.MethodPost, Path: api.BackupsPath, Header: csrfHeader, Status: http.StatusCreated},
			},
			"GET " + api.BackupPath: {
				{Path: named(api.BackupPath, missing), Status: http.StatusNotFound},
				{Path: named(api.BackupPath, "x"), Status: http.StatusNotFound},
			},
			"POST " + api.BackupDeletePath: {
				{Method: http.MethodPost, Path: named(api.BackupDeletePath, missing), Header: csrfHeader,
					Status: http.StatusNotFound},
				{Method: http.MethodPost, Path: named(api.BackupDeletePath, "x"), Header: csrfHeader,
					Status: http.StatusNotFound},
			},
		},
		maintainerOnly: []string{"GET " + api.BackupsPath, "POST " + api.BackupsPath, "GET " + api.BackupPath,
			"POST " + api.BackupDeletePath},
	})
}
