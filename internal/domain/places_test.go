package domain_test

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EatWell-Studio/OSFM/internal/domain"
)

// testCity is a made-up city's bounding box.
var testCity = domain.Bounds{7.02, 0.31, 7.1, 0.37}

const testUUID = "0190a1b2-0000-7000-8000-000000000001"

func isUUID(s string) bool { return s == testUUID }

func validPlace() domain.PlaceDraft {
	return domain.PlaceDraft{Name: "  Test Noodle Bar ", Lat: 0.3412345678, Lon: 7.0612345678}
}

func TestNormalizePlace(t *testing.T) {
	got, coord, err := domain.NormalizePlace(validPlace(), []domain.Bounds{{1, 1, 2, 2}, testCity}, isUUID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Test Noodle Bar" || got.Precision != domain.PrecisionExact || got.Category != "" ||
		len(got.Aliases) != 0 || len(got.Cuisine) != 0 || got.Landmark != nil {
		t.Errorf("%+v", got)
	}
	want := domain.SourceCoord{Lat: 0.3412345678, Lon: 7.0612345678, CRS: domain.CRSWGS84,
		Source: domain.CoordFromCrosshair}
	if coord != want {
		t.Errorf("source coord %+v, want %+v", coord, want)
	}

	full := domain.PlaceDraft{Name: "Test Cafe", Aliases: []string{" Test Alias ", "", "Test Alias", "Other Alias"},
		Lat: 0.31, Lon: 7.1, Precision: domain.PrecisionArea, PositionNote: " Test line one\r\nline two ",
		Landmark: &domain.Landmark{Type: domain.LandmarkText, Text: " Test corner ", PlaceUUID: "ignored"},
		Category: "cafe", Cuisine: []string{"bubble_tea", "chinese", "bubble_tea"}, CoordSource: domain.CoordFromSearch}
	got, coord, err = domain.NormalizePlace(full, []domain.Bounds{testCity}, isUUID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Aliases, []string{"Test Alias", "Other Alias"}) ||
		got.PositionNote != "Test line one\nline two" ||
		*got.Landmark != (domain.Landmark{Type: domain.LandmarkText, Text: "Test corner"}) ||
		!slices.Equal(got.Cuisine, []string{"chinese", "bubble_tea"}) || coord.Source != domain.CoordFromSearch {
		t.Errorf("%+v %+v", got, coord)
	}
	got, _, err = domain.NormalizePlace(domain.PlaceDraft{Name: "x", Lat: 0.34, Lon: 7.06, Landmark: &domain.Landmark{
		Type: domain.LandmarkPlace, PlaceUUID: testUUID, Text: "ignored"}}, []domain.Bounds{testCity}, isUUID)
	if err != nil || got.Landmark.Text != "" || got.Landmark.PlaceUUID != testUUID {
		t.Errorf("%+v %v", got.Landmark, err)
	}
}

func TestNormalizePlaceRejects(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*domain.PlaceDraft)
		field  string
		reason error
	}{
		{"empty name", func(d *domain.PlaceDraft) { d.Name = "   " }, "name", domain.ErrInvalid},
		{"long name", func(d *domain.PlaceDraft) { d.Name = strings.Repeat("面", 201) }, "name", domain.ErrInvalid},
		{"name with a line break", func(d *domain.PlaceDraft) { d.Name = "Test\nPlace" }, "name", domain.ErrInvalid},
		{"too many aliases", func(d *domain.PlaceDraft) {
			for i := range 11 {
				d.Aliases = append(d.Aliases, strings.Repeat("a", i+1))
			}
		}, "aliases", domain.ErrInvalid},
		{"long alias", func(d *domain.PlaceDraft) { d.Aliases = []string{"ok", strings.Repeat("a", 201)} },
			"aliases[1]", domain.ErrInvalid},
		{"outside the cities", func(d *domain.PlaceDraft) { d.Lat, d.Lon = 31.23, 121.47 }, "lat",
			domain.ErrOutsideCities},
		{"just outside", func(d *domain.PlaceDraft) { d.Lon = 7.10001 }, "lat", domain.ErrOutsideCities},
		{"not a number", func(d *domain.PlaceDraft) { d.Lat = math.NaN() }, "lat", domain.ErrInvalid},
		{"out of range", func(d *domain.PlaceDraft) { d.Lat = 91 }, "lat", domain.ErrInvalid},
		{"precision", func(d *domain.PlaceDraft) { d.Precision = "door" }, "precision", domain.ErrInvalid},
		{"category off the whitelist", func(d *domain.PlaceDraft) { d.Category = "pharmacy" }, "category",
			domain.ErrInvalid},
		{"cuisine off the list", func(d *domain.PlaceDraft) { d.Cuisine = []string{"chinese", "regional"} },
			"cuisine[1]", domain.ErrInvalid},
		{"long position note", func(d *domain.PlaceDraft) { d.PositionNote = strings.Repeat("a", 501) },
			"positionNote", domain.ErrInvalid},
		{"control character in the note", func(d *domain.PlaceDraft) { d.PositionNote = "a\x00b" }, "positionNote",
			domain.ErrInvalid},
		{"landmark kind", func(d *domain.PlaceDraft) { d.Landmark = &domain.Landmark{Type: "osm"} }, "landmark.type",
			domain.ErrInvalid},
		{"landmark place not a uuid", func(d *domain.PlaceDraft) {
			d.Landmark = &domain.Landmark{Type: domain.LandmarkPlace, PlaceUUID: "abc"}
		}, "landmark.place", domain.ErrInvalid},
		{"empty landmark text", func(d *domain.PlaceDraft) {
			d.Landmark = &domain.Landmark{Type: domain.LandmarkText, Text: " "}
		}, "landmark.text", domain.ErrInvalid},
		{"coordinate system", func(d *domain.PlaceDraft) { d.CRS = "gcj02" }, "sourceCoord.crs", domain.ErrInvalid},
		{"coordinate source", func(d *domain.PlaceDraft) { d.CoordSource = "gps" }, "sourceCoord.source",
			domain.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := validPlace()
			tc.change(&d)
			_, _, err := domain.NormalizePlace(d, []domain.Bounds{testCity}, isUUID)
			var fe *domain.FieldError
			if !errors.As(err, &fe) || fe.Field != tc.field || !errors.Is(err, tc.reason) {
				t.Errorf("error %v, want %s: %v", err, tc.field, tc.reason)
			}
		})
	}
	if _, _, err := domain.NormalizePlace(validPlace(), nil, isUUID); !errors.Is(err, domain.ErrOutsideCities) {
		t.Errorf("no cities: %v", err)
	}
}

var now = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

func validRecommendation() domain.RecommendationDraft {
	return domain.RecommendationDraft{Attitude: domain.AttitudeGood, Text: " Test text "}
}

func TestNormalizeRecommendation(t *testing.T) {
	got, err := domain.NormalizeRecommendation(validRecommendation(), false, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Test text" || got.DisplayAs != domain.SignAsMain || got.Visibility != domain.VisibilityCircle ||
		len(got.Dishes) != 0 || len(got.TimeSlots) != 0 || len(got.Sources) != 0 {
		t.Errorf("%+v", got)
	}
	full := domain.RecommendationDraft{Attitude: domain.AttitudeMustTry, Text: "Test\r\nline", Dishes: []string{
		" Test dish ", "", "Test dish", "Other dish"}, TimeSlots: []string{"late_night", "lunch", "lunch"},
		Season: " winter ", Sources: []domain.Source{{URL: " https://example.test/a ", Kind: "chat",
			Excerpt: " Test words "}, {Kind: "visited"}},
		// Tomorrow in UTC is already today east of it.
		VisitedOn: "2026-11-28", DisplayAs: domain.SignAsPseudonym, Visibility: domain.VisibilityPrivate}
	got, err = domain.NormalizeRecommendation(full, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Test\nline" || !slices.Equal(got.Dishes, []string{"Test dish", "Other dish"}) ||
		!slices.Equal(got.TimeSlots, []string{"lunch", "late_night"}) || got.Season != "winter" ||
		got.Sources[0] != (domain.Source{URL: "https://example.test/a", Kind: "chat", Excerpt: "Test words"}) ||
		got.Sources[1] != (domain.Source{Kind: "visited"}) || got.VisitedOn != "2026-11-28" {
		t.Errorf("%+v", got)
	}
}

func TestNormalizeRecommendationRejects(t *testing.T) {
	for _, tc := range []struct {
		name         string
		change       func(*domain.RecommendationDraft)
		hasPseudonym bool
		field        string
		reason       error
	}{
		{"no attitude", func(d *domain.RecommendationDraft) { d.Attitude = "" }, false, "attitude", domain.ErrInvalid},
		{"unknown attitude", func(d *domain.RecommendationDraft) { d.Attitude = "great" }, false, "attitude",
			domain.ErrInvalid},
		{"empty text", func(d *domain.RecommendationDraft) { d.Text = " \n\t " }, false, "text", domain.ErrInvalid},
		{"long text", func(d *domain.RecommendationDraft) { d.Text = strings.Repeat("好", 5001) }, false, "text",
			domain.ErrInvalid},
		{"too many dishes", func(d *domain.RecommendationDraft) {
			for i := range 21 {
				d.Dishes = append(d.Dishes, strings.Repeat("d", i+1))
			}
		}, false, "dishes", domain.ErrInvalid},
		{"time slot", func(d *domain.RecommendationDraft) { d.TimeSlots = []string{"brunch"} }, false, "timeSlots[0]",
			domain.ErrInvalid},
		{"long season", func(d *domain.RecommendationDraft) { d.Season = strings.Repeat("s", 101) }, false, "season",
			domain.ErrInvalid},
		{"too many sources", func(d *domain.RecommendationDraft) {
			d.Sources = slices.Repeat([]domain.Source{{Kind: "note"}}, 11)
		}, false, "sources", domain.ErrInvalid},
		{"source kind", func(d *domain.RecommendationDraft) { d.Sources = []domain.Source{{Kind: "rumour"}} }, false,
			"sources[0].kind", domain.ErrInvalid},
		{"javascript link", func(d *domain.RecommendationDraft) {
			d.Sources = []domain.Source{{Kind: "note"}, {Kind: "note", URL: "javascript:alert(1)"}}
		}, false, "sources[1].url", domain.ErrInvalid},
		{"relative link", func(d *domain.RecommendationDraft) {
			d.Sources = []domain.Source{{Kind: "note", URL: "/p/x"}}
		}, false, "sources[0].url", domain.ErrInvalid},
		{"link with credentials", func(d *domain.RecommendationDraft) {
			d.Sources = []domain.Source{{Kind: "note", URL: "https://user:pw@example.test/"}} // #nosec G101 -- made up
		}, false, "sources[0].url", domain.ErrInvalid},
		{"long excerpt", func(d *domain.RecommendationDraft) {
			d.Sources = []domain.Source{{Kind: "chat", Excerpt: strings.Repeat("e", 501)}}
		}, false, "sources[0].excerpt", domain.ErrInvalid},
		{"visit tomorrow everywhere", func(d *domain.RecommendationDraft) { d.VisitedOn = "2026-11-29" }, false,
			"visitedOn", domain.ErrInvalid},
		{"not a date", func(d *domain.RecommendationDraft) { d.VisitedOn = "2026-02-30" }, false, "visitedOn",
			domain.ErrInvalid},
		{"signature", func(d *domain.RecommendationDraft) { d.DisplayAs = "anonymous" }, false, "displayAs",
			domain.ErrInvalid},
		{"pseudonym without one", func(d *domain.RecommendationDraft) { d.DisplayAs = domain.SignAsPseudonym }, false,
			"displayAs", domain.ErrNoPseudonym},
		{"federated", func(d *domain.RecommendationDraft) { d.Visibility = domain.VisibilityFederated }, true,
			"visibility", domain.ErrInvalid},
		{"unknown visibility", func(d *domain.RecommendationDraft) { d.Visibility = "public" }, true, "visibility",
			domain.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := validRecommendation()
			tc.change(&d)
			_, err := domain.NormalizeRecommendation(d, tc.hasPseudonym, now)
			var fe *domain.FieldError
			if !errors.As(err, &fe) || fe.Field != tc.field || !errors.Is(err, tc.reason) {
				t.Errorf("error %v, want %s: %v", err, tc.field, tc.reason)
			}
		})
	}
}

func TestSimilarNames(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"Test Noodle Bar", "test noodle-bar", true},
		{"ＴＥＳＴ　Ｃａｆｅ", "Test Cafe", true}, // full-width forms
		{"测试面馆", "测试面馆（老店）", true},       // contained
		{"Test Noodle Bar", "Test Noodle Baa", true},
		{"Test Noodle Bar", "Test Nodle Br", true}, // two edits, long name
		{"Cafe", "Cafa", true},                     // one edit, short name
		{"Ab", "Ac", false},                        // too short for edits
		{"Test Noodle Bar", "Test Pizza Place", false},
		{"Test Bakery", "", false},
		{"!!", "Test Bakery", false},
		{"A", "Alpha", false}, // one character is not contained
	} {
		if got := domain.SimilarNames(tc.a, tc.b); got != tc.want {
			t.Errorf("SimilarNames(%q, %q) = %v", tc.a, tc.b, got)
		}
		if got := domain.SimilarNames(tc.b, tc.a); got != tc.want {
			t.Errorf("SimilarNames(%q, %q) = %v", tc.b, tc.a, got)
		}
	}
}

func TestDistance(t *testing.T) {
	// 0.001 degrees of latitude is about 111 m.
	if d := domain.DistanceMeters(0.34, 7.06, 0.341, 7.06); math.Abs(d-111.2) > 0.5 {
		t.Errorf("distance %f", d)
	}
	for m, want := range map[float64]int{0: 0, 4.9: 0, 5: 10, 44: 40, 99.9: 100} {
		if got := domain.RoundDistance(m); got != want {
			t.Errorf("RoundDistance(%v) = %d, want %d", m, got, want)
		}
	}
	minLat, minLon, maxLat, maxLon := domain.NearbyBox(48.0, 11.0, 100)
	for _, p := range [][2]float64{{minLat, 11}, {maxLat, 11}, {48, minLon}, {48, maxLon}} {
		if d := domain.DistanceMeters(48, 11, p[0], p[1]); math.Abs(d-100) > 0.5 {
			t.Errorf("box edge %v is %f m away", p, d)
		}
	}
}
