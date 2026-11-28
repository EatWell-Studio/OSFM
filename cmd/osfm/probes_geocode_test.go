package main

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// The internal ID scan's probes of place-name search (internal/api/geocode.go, T039). See registerProbes.
//
// The probes stop at the request checks. The scan's instance has its upstream services at an address nothing listens
// on (testapp.UnreachableSearch), so even a search that passed them would fail with 503 rather than reach the public
// Photon or Nominatim. A search that succeeds returns only what the upstream sent (names, labels, positions), never a
// record; internal/api's tests cover it with fake upstreams.
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

// TestSearchNeverLeavesTests: the test harnesses of this package (the scan's boot and testapp) point the place search
// at an unreachable address, never at the public services, and a valid search there fails at once.
func TestSearchNeverLeavesTests(t *testing.T) {
	_, cfg, _, _ := bootServed(t, false)
	a := testapp.Start(t, testapp.Options{Bind: bind})
	for name, got := range map[string]string{
		"scan Photon": cfg.GeocoderPhotonURL, "scan Nominatim": cfg.GeocoderNominatimURL,
		"testapp Photon": a.Config.GeocoderPhotonURL, "testapp Nominatim": a.Config.GeocoderNominatimURL,
	} {
		if got != testapp.UnreachableSearchURL {
			t.Errorf("%s at %q, want %q", name, got, testapp.UnreachableSearchURL)
		}
	}
	path := api.GeocodePath + "?" + url.Values{"q": {"test"}, "city": {testapp.CitySlug}}.Encode()
	a.As(testapp.Member).Get(path).Expect(http.StatusServiceUnavailable)
}
