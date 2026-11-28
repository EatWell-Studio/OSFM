package export

import (
	"fmt"
	"io"
	"math/rand/v2"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// BenchmarkLayers measures writing both layers of 1000 made-up places with 5000 recommendations by 50 members (the
// data of internal/api's BenchmarkMap):
//
//	go test ./internal/export -run '^$' -bench BenchmarkLayers -benchtime 20x
func BenchmarkLayers(b *testing.B) {
	a := testapp.Start(b, testapp.Options{Bind: func(app core.App, _ *config.Config) { store.BindHooks(app) }})
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
	for i := range 1000 {
		place := a.SeedPlace(testapp.PlaceSeed{Name: fmt.Sprintf("Bench Place %04d", i),
			Lat: 0.31 + rng.Float64()*0.06, Lon: 7.02 + rng.Float64()*0.08, Cuisine: []string{"chinese"}})
		for _, j := range rng.Perm(len(members))[:5] {
			seed := testapp.RecommendationSeed{Place: place, Author: members[j], Attitude: domain.Attitudes[rng.IntN(4)],
				Text: "Bench recommendation text, made up for the benchmark."}
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
	for name, write := range map[string]func(io.Writer, core.App, Options) error{
		"places": WritePlaces, "recommendations": WriteRecommendations,
	} {
		b.Run(name, func(b *testing.B) {
			var n countingWriter
			for b.Loop() {
				n = 0
				if err := write(&n, a, Options{}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(n)/1024, "KB")
		})
	}
}

type countingWriter int

func (c *countingWriter) Write(p []byte) (int, error) {
	*c += countingWriter(len(p))
	return len(p), nil
}
