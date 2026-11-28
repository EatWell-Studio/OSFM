package domain

import (
	"math"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// The duplicate hint (B6): when a member enters a new place, the places nearby with a similar name are offered ("is it
// this one?"), so that they add their recommendation there instead of creating the place twice.
const (
	// NearbyRadius is how far from the entered point a place may be to be offered, in metres.
	NearbyRadius = 100
	// MaxNearby is the number of places offered at most, the nearest first.
	MaxNearby = 5
	// DistanceStep is the step distances are rounded to before they are shown, in metres (H3: nothing suggests a
	// precision better than about 10 m).
	DistanceStep = 10
)

// earthRadius is the mean radius of the Earth, in metres.
const earthRadius = 6371008.8

// DistanceMeters returns the great-circle distance between two WGS-84 points, in metres.
func DistanceMeters(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadius * math.Asin(math.Min(1, math.Sqrt(a)))
}

// RoundDistance rounds a distance in metres to [DistanceStep].
func RoundDistance(m float64) int {
	return int(math.Round(m/DistanceStep)) * DistanceStep
}

// NearbyBox returns the bounding box, in degrees, that holds every point within radius metres of (lat, lon): a query
// selects the candidates by it, and [DistanceMeters] decides.
func NearbyBox(lat, lon, radius float64) (minLat, minLon, maxLat, maxLon float64) {
	dLat := radius / (earthRadius * math.Pi / 180)
	cos := math.Cos(lat * math.Pi / 180)
	dLon := 180.0
	if cos > 1e-6 {
		dLon = math.Min(180, dLat/cos)
	}
	return lat - dLat, lon - dLon, lat + dLat, lon + dLon
}

// PlaceNameKey returns the form in which place names are compared for the duplicate hint: NFKC, lower case, without
// white space, punctuation and invisible characters. "Test Noodle-Bar" and "test noodle bar" have the same key, and so
// do full-width and half-width forms.
func PlaceNameKey(name string) string {
	s := strings.ToLower(norm.NFKC.String(name))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) ||
			unicode.Is(unicode.Mn, r) && r >= 0xFE00 && r <= 0xFE0F {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// SimilarNames reports whether two place names are similar enough for the duplicate hint: their [PlaceNameKey]s are
// equal, one contains the other (when the shorter has at least two characters), or they are a short edit distance
// apart: one edit for names of 3 to 5 characters, two for longer ones (the shorter name counts). Names whose key is
// empty are similar to nothing.
func SimilarNames(a, b string) bool {
	ka, kb := []rune(PlaceNameKey(a)), []rune(PlaceNameKey(b))
	if len(ka) == 0 || len(kb) == 0 {
		return false
	}
	if len(ka) > len(kb) {
		ka, kb = kb, ka
	}
	if string(ka) == string(kb) {
		return true
	}
	if len(ka) >= 2 && strings.Contains(string(kb), string(ka)) {
		return true
	}
	allowed := 0
	switch {
	case len(ka) >= 6:
		allowed = 2
	case len(ka) >= 3:
		allowed = 1
	}
	return allowed > 0 && len(kb)-len(ka) <= allowed && editDistance(ka, kb) <= allowed
}

// editDistance returns the Levenshtein distance between a and b, in characters.
func editDistance(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
