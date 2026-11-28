package main

import (
	"net/http"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
)

// The internal ID scan's probes of the member's notices (internal/api/notices.go, T037). See registerProbes.
func init() {
	registerProbes(probeSet{
		probes: map[string][]idscan.Request{
			// Every member reads their own notices; the seeded member's name their recommendation and its place.
			"GET " + api.NoticesPath: {
				{Path: api.NoticesPath, Status: http.StatusOK},
			},
			// Every member marks their own notices read: the seeded notice is the member's, so the maintainer gets
			// 404 for it. Marking read again changes nothing, so the probes answer the same every time.
			"POST " + api.NoticesReadPath: {
				{Method: http.MethodPost, Path: api.NoticesReadPath, Header: csrfHeader,
					Body: `{"uuids":["` + idscan.NoticeUUID + `"]}`, Status: http.StatusNotFound,
					MemberStatus: http.StatusOK},
				{Method: http.MethodPost, Path: api.NoticesReadPath, Header: csrfHeader, Body: `{"all":true}`,
					Status: http.StatusOK},
				{Method: http.MethodPost, Path: api.NoticesReadPath, Header: csrfHeader, Body: `{}`,
					Status: http.StatusBadRequest},
				{Method: http.MethodPost, Path: api.NoticesReadPath, Header: csrfHeader, Body: `{`,
					Status: http.StatusBadRequest},
			},
		},
	})
}
