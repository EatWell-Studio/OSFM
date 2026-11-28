package api_test

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// BenchmarkMap measures the map data, the signatures and the place card on 1000 made-up places with 5000
// recommendations by 50 members in the seeded city (T027):
//
//	go test ./internal/api -run '^$' -bench BenchmarkMap -benchtime 50x
func BenchmarkMap(b *testing.B) {
	a := testapp.Start(b, testapp.Options{Bind: func(app core.App, cfg *config.Config) {
		store.BindHooks(app)
		auth.Bind(app, auth.Options{})
		api.Bind(app, api.Options{Config: cfg})
	}})
	rng := rand.New(rand.NewPCG(1000, 5000)) // #nosec G404 -- reproducible made-up benchmark data
	members := make([]*core.Record, 50)
	for i := range members {
		m, err := auth.NewMemberRecord(a)
		if err != nil {
			b.Fatal(err)
		}
		m.Set("login_name", fmt.Sprintf("bench_%d", i))
		m.Set("display_name", fmt.Sprintf("Bench Member %d", i))
		m.Set("pseudonym", fmt.Sprintf("Bench Pen %d", i))
		m.Set("role", auth.RoleMember)
		if err := a.Save(m); err != nil {
			b.Fatal(err)
		}
		members[i] = m
	}
	slots := []string{"breakfast", "lunch", "afternoon", "dinner", "late_night"}
	var some *core.Record
	for i := range 1000 {
		place := a.SeedPlace(testapp.PlaceSeed{Name: fmt.Sprintf("Bench Place %04d", i),
			Lat: 0.31 + rng.Float64()*0.06, Lon: 7.02 + rng.Float64()*0.08,
			Category: store.PlaceCategories[rng.IntN(len(store.PlaceCategories))], Cuisine: []string{"chinese"}})
		some = place
		for _, j := range rng.Perm(len(members))[:5] {
			seed := testapp.RecommendationSeed{Place: place, Author: members[j],
				Attitude: domain.Attitudes[rng.IntN(4)], TimeSlots: []string{slots[rng.IntN(5)]}}
			switch rng.IntN(10) {
			case 0:
				seed.Visibility = domain.VisibilityPrivate
			case 1:
				seed.HiddenKind = store.HiddenByReports
			case 2, 3:
				seed.DisplayAs = domain.SignAsPseudonym
			}
			a.SeedRecommendation(seed)
		}
	}
	c := a.SignedIn(members[0])
	city := "?city=" + testapp.CitySlug
	for name, path := range map[string]string{
		"places/city":     api.MapPlacesPath + city,
		"places/viewport": api.MapPlacesPath + city + "&bbox=7.04,0.32,7.08,0.35",
		"places/filtered": api.MapPlacesPath + city + "&slots=lunch,dinner&mustTry=1&noAvoid=1&by=Bench%20Member%203",
		"signatures":      api.SignaturesPath + city,
		"card":            strings.Replace(api.PlaceCardPath, "{uuid}", some.GetString(store.FieldUUID), 1),
	} {
		b.Run(name, func(b *testing.B) {
			var n int
			for b.Loop() {
				r := c.Get(path).Expect(http.StatusOK)
				n = r.Body.Len()
			}
			b.ReportMetric(float64(n), "bytes/op-body")
		})
	}
}
