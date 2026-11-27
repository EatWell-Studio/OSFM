package settings

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	city := City{Slug: "test-town", Name: " Test Town ", BBox: [4]float64{-1, -1, 1, 1}, Center: [2]float64{0, 0}}
	got, err := Normalize(Instance{Name: "  Test Circle\t", Cities: []City{city}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Test Circle" || got.Cities[0].Name != "Test Town" {
		t.Errorf("not trimmed: %+v", got)
	}

	tooMany := make([]City, MaxCities+1)
	for i := range tooMany {
		tooMany[i] = city
		tooMany[i].Slug = "town-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	for name, tc := range map[string]struct {
		in   Instance
		want error
	}{
		"empty name":        {Instance{Name: "", Cities: []City{city}}, ErrInvalidName},
		"control character": {Instance{Name: "Test\u0007", Cities: []City{city}}, ErrInvalidName},
		"format character":  {Instance{Name: "Test\u200e", Cities: []City{city}}, ErrInvalidName},
		"no cities":         {Instance{Name: "Test"}, ErrInvalidCities},
		"too many cities":   {Instance{Name: "Test", Cities: tooMany}, ErrInvalidCities},
		"NaN": {Instance{Name: "Test", Cities: []City{{Slug: "x", Name: "X",
			BBox: [4]float64{math.NaN(), 0, 1, 1}, Center: [2]float64{0.5, 0.5}}}}, ErrInvalidCities},
		"slug with a dot": {Instance{Name: "Test", Cities: []City{{Slug: "x.y", Name: "X",
			BBox: [4]float64{0, 0, 1, 1}, Center: [2]float64{0.5, 0.5}}}}, ErrInvalidCities},
		"long city name": {Instance{Name: "Test", Cities: []City{{Slug: "x", Name: strings.Repeat("x", MaxCityNameLength+1),
			BBox: [4]float64{0, 0, 1, 1}, Center: [2]float64{0.5, 0.5}}}}, ErrInvalidCities},
	} {
		if _, err := Normalize(tc.in); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
}
