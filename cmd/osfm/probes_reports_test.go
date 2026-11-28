package main

import (
	"net/http"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
)

// The internal ID scan's probes of hiding, reports and their review (internal/api/reports.go, T036). Add a probe for
// every route the feature registers, with the routes only maintainers may use in maintainerOnly; see registerProbes.
//
// The identities run one after another on the same instance, so the probes leave the seeded recommendations as they
// found them for the next identity: the only change of hidden state (hiding and unhiding the bakery recommendation) is
// undone within the same list, and reports only add to recommendations that stay under the threshold.
func init() {
	const missing = "0190a1b2-0000-7000-8000-00000000ffff"
	registerProbes(probeSet{
		probes: map[string][]idscan.Request{
			// Every member who sees the recommendation, except its author.
			"POST " + api.ReportPath: {
				// The seeded member wrote it (400); the maintainer reported it before (409).
				{Method: http.MethodPost, Path: memberPath(api.ReportPath, idscan.MemberRecommendationUUID),
					Header: csrfHeader, Body: `{"reason":"abuse"}`, Status: http.StatusConflict,
					MemberStatus: http.StatusBadRequest},
				{Method: http.MethodPost, Path: memberPath(api.ReportPath, idscan.BakeryRecommendationUUID),
					Header: csrfHeader, Body: `{"reason":"advertising","note":"Test note"}`, Status: http.StatusCreated},
				// Hidden: only maintainers see it.
				{Method: http.MethodPost, Path: memberPath(api.ReportPath, idscan.HiddenRecommendationUUID),
					Header: csrfHeader, Body: `{"reason":"false_or_duplicate"}`, Status: http.StatusCreated,
					MemberStatus: http.StatusNotFound},
				{Method: http.MethodPost, Path: memberPath(api.ReportPath, idscan.BakeryRecommendationUUID),
					Header: csrfHeader, Body: `{"reason":"other"}`, Status: http.StatusBadRequest},
				{Method: http.MethodPost, Path: memberPath(api.ReportPath, "x"),
					Header: csrfHeader, Body: `{"reason":"abuse"}`, Status: http.StatusNotFound},
			},
			// Maintainers only.
			"POST " + api.HidePath: {
				{Method: http.MethodPost, Path: memberPath(api.HidePath, idscan.HiddenRecommendationUUID),
					Header: csrfHeader, Body: `{"reason":"Test reason"}`, Status: http.StatusConflict},
				{Method: http.MethodPost, Path: memberPath(api.HidePath, idscan.BakeryRecommendationUUID),
					Header: csrfHeader, Body: `{"reason":" "}`, Status: http.StatusBadRequest},
				{Method: http.MethodPost, Path: memberPath(api.HidePath, missing),
					Header: csrfHeader, Body: `{"reason":"Test reason"}`, Status: http.StatusNotFound},
			},
			"POST " + api.UnhidePath: {
				{Method: http.MethodPost, Path: memberPath(api.UnhidePath, idscan.BakeryRecommendationUUID),
					Header: csrfHeader, Status: http.StatusConflict},
				// Hiding and unhiding it again, in this order (the hide answers like an unhide: the projection).
				{Method: http.MethodPost, Path: memberPath(api.HidePath, idscan.BakeryRecommendationUUID),
					Header: csrfHeader, Body: `{"reason":"Test reason"}`, Status: http.StatusOK},
				{Method: http.MethodPost, Path: memberPath(api.UnhidePath, idscan.BakeryRecommendationUUID),
					Header: csrfHeader, Body: `{"reason":"Test reason"}`, Status: http.StatusOK},
				{Method: http.MethodPost, Path: memberPath(api.UnhidePath, "x"), Header: csrfHeader,
					Status: http.StatusNotFound},
			},
			"GET " + api.ReviewQueuePath: {
				{Path: api.ReviewQueuePath, Status: http.StatusOK},
			},
			"POST " + api.ResolvePath: {
				{Method: http.MethodPost, Path: memberPath(api.ResolvePath, idscan.HiddenRecommendationUUID),
					Header: csrfHeader, Body: `{"resolution":"kept_hidden"}`, Status: http.StatusOK},
				{Method: http.MethodPost, Path: memberPath(api.ResolvePath, idscan.MemberRecommendationUUID),
					Header: csrfHeader, Body: `{"resolution":"kept_hidden"}`, Status: http.StatusConflict},
				{Method: http.MethodPost, Path: memberPath(api.ResolvePath, idscan.HiddenRecommendationUUID),
					Header: csrfHeader, Body: `{"resolution":"deleted"}`, Status: http.StatusBadRequest},
				{Method: http.MethodPost, Path: memberPath(api.ResolvePath, idscan.HiddenRecommendationUUID),
					Header: csrfHeader, Body: `{"resolution":"forgotten"}`, Status: http.StatusBadRequest},
				{Method: http.MethodPost, Path: memberPath(api.ResolvePath, missing),
					Header: csrfHeader, Body: `{"resolution":"restored"}`, Status: http.StatusNotFound},
			},
			// The scan's sessions are fresh, so the route that needs a fresh proof reaches its handler.
			"POST " + api.RevealAuthorPath: {
				{Method: http.MethodPost, Path: memberPath(api.RevealAuthorPath, idscan.HiddenRecommendationUUID),
					Header: csrfHeader, Status: http.StatusOK},
				{Method: http.MethodPost, Path: memberPath(api.RevealAuthorPath, missing),
					Header: csrfHeader, Status: http.StatusNotFound},
			},
		},
		maintainerOnly: []string{"POST " + api.HidePath, "POST " + api.UnhidePath, "GET " + api.ReviewQueuePath,
			"POST " + api.ResolvePath, "POST " + api.RevealAuthorPath},
	})
}
