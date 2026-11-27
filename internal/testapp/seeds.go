package testapp

import (
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// PlaceSeed is a made-up place for [App.SeedPlace]. Zero fields get defaults.
type PlaceSeed struct {
	Name string // default "Test Place"
	// Lat and Lon default to the center of the seeded city (both zero means unset).
	Lat, Lon  float64
	Precision string // default "exact"
	Category  string // default none
	Cuisine   []string
	Deleted   bool // soft-deleted after it is written
}

// SeedPlace writes a made-up place, through the store hooks (uuid, rounded coordinates), and returns it. Places carry
// no member fields: who created a place is only in the audit log, which seeds do not write.
func (a *App) SeedPlace(p PlaceSeed) *core.Record {
	a.t.Helper()
	if p.Name == "" {
		p.Name = "Test Place"
	}
	if p.Lat == 0 && p.Lon == 0 {
		p.Lat, p.Lon = 0.34, 7.06 // the center of CitySlug
	}
	if p.Precision == "" {
		p.Precision = "exact"
	}
	values := map[string]any{"name": p.Name, "lat": p.Lat, "lon": p.Lon, "precision": p.Precision,
		"category": p.Category}
	if p.Cuisine != nil {
		values["cuisine"] = p.Cuisine
	}
	r := a.saveRecord(store.Places, values)
	if p.Deleted {
		a.softDelete(r)
	}
	return r
}

// RecommendationSeed is a made-up recommendation for [App.SeedRecommendation]. Place and Author are required; zero
// fields get defaults.
type RecommendationSeed struct {
	Place, Author *core.Record
	DisplayAs     string // default domain.SignAsMain
	Attitude      string // default domain.AttitudeGood
	Visibility    string // default domain.VisibilityCircle
	Text          string // default "Test recommendation text"
	Dishes        []string
	TimeSlots     []string
	Season        string
	// Sources are written as given ([{url, kind, excerpt}]).
	Sources   []map[string]any
	VisitedOn string // YYYY-MM-DD
	// HiddenKind hides the recommendation (store.HiddenBy*, with HiddenReason); HiddenBy is the maintainer, if any.
	HiddenKind, HiddenReason string
	HiddenBy                 *core.Record
	// EditedByMaintainerAt sets edited_by_maintainer_at.
	EditedByMaintainerAt time.Time
	Deleted              bool // soft-deleted after it is written
}

// SeedRecommendation writes a made-up recommendation, through the store hooks, and returns it. A member has at most
// one live recommendation per place (M2-Q1): seed a second one of the same author and place only after deleting the
// first.
func (a *App) SeedRecommendation(r RecommendationSeed) *core.Record {
	a.t.Helper()
	if r.Place == nil || r.Author == nil {
		a.t.Fatal("testapp: RecommendationSeed needs Place and Author")
	}
	def := func(v *string, d string) {
		if *v == "" {
			*v = d
		}
	}
	def(&r.DisplayAs, domain.SignAsMain)
	def(&r.Attitude, domain.AttitudeGood)
	def(&r.Visibility, domain.VisibilityCircle)
	def(&r.Text, "Test recommendation text")
	values := map[string]any{"place": r.Place.Id, "author": r.Author.Id, "display_as": r.DisplayAs,
		"attitude": r.Attitude, "visibility": r.Visibility, "text": r.Text, "season": r.Season}
	if r.Dishes != nil {
		values["dishes"] = r.Dishes
	}
	if r.TimeSlots != nil {
		values["time_slots"] = r.TimeSlots
	}
	if r.Sources != nil {
		values["sources"] = r.Sources
	}
	if r.VisitedOn != "" {
		values["visited_on"] = r.VisitedOn + " 00:00:00.000Z"
	}
	if r.HiddenKind != "" {
		values["hidden"] = true
		values["hidden_kind"] = r.HiddenKind
		values["hidden_reason"] = r.HiddenReason
		values["hidden_at"] = types.NowDateTime()
		if r.HiddenBy != nil {
			values["hidden_by"] = r.HiddenBy.Id
		}
	}
	if !r.EditedByMaintainerAt.IsZero() {
		values["edited_by_maintainer_at"] = r.EditedByMaintainerAt
	}
	rec := a.saveRecord(store.Recommendations, values)
	if r.Deleted {
		a.softDelete(rec)
	}
	return rec
}

func (a *App) saveRecord(collection string, values map[string]any) *core.Record {
	a.t.Helper()
	c, err := a.FindCachedCollectionByNameOrId(collection)
	if err != nil {
		a.t.Fatal(err)
	}
	r := core.NewRecord(c)
	for k, v := range values {
		r.Set(k, v)
	}
	if err := a.Save(r); err != nil {
		a.t.Fatalf("testapp: seed %s: %v", collection, err)
	}
	return r
}

func (a *App) softDelete(r *core.Record) {
	a.t.Helper()
	if err := store.SoftDelete(a, r); err != nil {
		a.t.Fatal(err)
	}
}
