package store

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/domain"
)

// Visibility (C4, C6, M2-Q3; architecture §3.4: every read goes through one function). Every read of recommendations
// or places on behalf of a member — the card, the map, the counts, the filters, the duplicate hint, the revisions,
// the exports, the share text — starts from one of the two queries below, or, where a query has to be written by hand
// (a grouping query, a join), from the condition [RecommendationVisibleTo]. They are the query form of
// [domain.CanSeeRecommendation] and [domain.CanSeePlace]; TestVisibility checks that both agree.
//
//   - [VisibleRecommendations]: the recommendations the viewer may see, as a [RecordQuery]. Add conditions with Where
//     (they are ANDed, the visibility condition cannot be removed; IncludeDeleted does not bring deleted ones back):
//
//     store.VisibleRecommendations(app, v).Where(dbx.HashExp{"place": place.Id}).OrderBy("created DESC").All()
//
//   - [VisiblePlaces]: the live places with at least one recommendation the viewer may see. Narrow it with Where (a
//     bounding box on lat and lon, a uuid) and with conditions on the place's visible recommendations,
//     [HasVisibleRecommendation] (the map filters: "some visible must_try", "no visible avoid", time slots):
//
//     store.VisiblePlaces(app, v).
//     Where(dbx.Between("lat", minLat, maxLat)).Where(dbx.Between("lon", minLon, maxLon)).
//     Where(store.HasVisibleRecommendation(v, dbx.HashExp{store.Rec("attitude"): "must_try"})).
//     Where(dbx.Not(store.HasVisibleRecommendation(v, dbx.HashExp{store.Rec("attitude"): "avoid"}))).All()
//
//   - [RecommendationVisibleTo]: the condition itself, for a hand-written query over recommendations under any table
//     name or alias, such as the grouped query of the map's counts and time slots. Counts leave out hidden
//     recommendations as well, for every viewer ([domain.CountAttitudes]): add [Shown] for that.
//
// Nobody who is not signed in (the zero [domain.Viewer]) sees anything.

// RecAlias is the alias of the recommendations table in the conditions given to [HasVisibleRecommendation]: refer to
// its columns with [Rec].
const RecAlias = "r"

// Rec returns the column of the recommendations table under [RecAlias], for conditions given to
// [HasVisibleRecommendation]: Rec("attitude") is "r.attitude".
func Rec(column string) string { return RecAlias + "." + column }

// RecommendationVisibleTo returns the condition that selects the recommendations v may see, with the columns of the
// recommendations table qualified by table (its name, Recommendations, or an alias). It is [domain.CanSeeRecommendation]
// as SQL: not deleted, of a place that is not deleted, and authored by v, or shared (circle, federated) and, unless v
// is a maintainer, not hidden.
func RecommendationVisibleTo(v domain.Viewer, table string) dbx.Expression {
	if !v.SignedIn() {
		return dbx.NewExp("1 = 0")
	}
	col := func(name string) string { return table + "." + name }
	shared := []dbx.Expression{dbx.In(col("visibility"), anyStrings(domain.SharedVisibilities)...)}
	if !v.Maintainer {
		shared = append(shared, Shown(table))
	}
	return dbx.And(
		dbx.HashExp{col(FieldDeletedAt): ""},
		dbx.Exists(dbx.NewExp("SELECT 1 FROM {{"+Places+"}} AS [[rp]] WHERE [[rp.id]] = [["+col("place")+
			"]] AND [[rp."+FieldDeletedAt+"]] = ''")),
		dbx.Or(dbx.HashExp{col("author"): v.MemberID}, dbx.And(shared...)),
	)
}

// Shown returns the condition that selects the recommendations that are not hidden, with the columns qualified by
// table. A recommendation counts as hidden when either hidden or hidden_kind says so, so that a row written
// inconsistently errs on the side of hiding.
func Shown(table string) dbx.Expression {
	return dbx.HashExp{table + ".hidden": false, table + ".hidden_kind": ""}
}

// VisibleRecommendations returns the query of the recommendations v may see.
func VisibleRecommendations(app core.App, v domain.Viewer) *RecordQuery {
	return Query(app, Recommendations).Where(RecommendationVisibleTo(v, Recommendations))
}

// VisiblePlaces returns the query of the live places that have at least one recommendation v may see (M2-Q3): a
// place whose only recommendations v may not see does not exist for v.
func VisiblePlaces(app core.App, v domain.Viewer) *RecordQuery {
	return Query(app, Places).Where(NotDeleted(Places)).Where(HasVisibleRecommendation(v, nil))
}

// HasVisibleRecommendation returns the condition, for a query of places, that the place has a recommendation v may see
// that also meets cond (nil for none). cond refers to the recommendation's columns with [Rec]. Negate it with dbx.Not
// for "no visible recommendation meets cond".
func HasVisibleRecommendation(v domain.Viewer, cond dbx.Expression) dbx.Expression {
	return &visibleRecommendationExists{viewer: v, cond: cond}
}

type visibleRecommendationExists struct {
	viewer domain.Viewer
	cond   dbx.Expression
}

// Build implements dbx.Expression.
func (e *visibleRecommendationExists) Build(db *dbx.DB, params dbx.Params) string {
	where := dbx.And(
		dbx.NewExp("[["+Rec("place")+"]] = [["+Places+".id]]"),
		RecommendationVisibleTo(e.viewer, RecAlias),
		e.cond,
	)
	return "EXISTS (SELECT 1 FROM {{" + Recommendations + "}} AS [[" + RecAlias + "]] WHERE " +
		where.Build(db, params) + ")"
}

// RecommendationStates returns what the visibility rules need to know about each recommendation record of recs, in
// the same order. Whether their places are deleted is looked up in one query.
func RecommendationStates(app core.App, recs []*core.Record) ([]domain.Recommendation, error) {
	placeIDs := make([]any, 0, len(recs))
	for _, r := range recs {
		placeIDs = append(placeIDs, r.GetString("place"))
	}
	livePlaces := map[string]bool{}
	if len(placeIDs) > 0 {
		var ids []string
		if err := app.DB().Select("id").From(Places).
			Where(dbx.And(dbx.In("id", placeIDs...), NotDeleted(Places))).Column(&ids); err != nil {
			return nil, err
		}
		for _, id := range ids {
			livePlaces[id] = true
		}
	}
	states := make([]domain.Recommendation, len(recs))
	for i, r := range recs {
		states[i] = domain.Recommendation{
			AuthorID:     r.GetString("author"),
			Visibility:   r.GetString("visibility"),
			Hidden:       IsHidden(r),
			Deleted:      IsDeleted(r),
			PlaceDeleted: !livePlaces[r.GetString("place")],
		}
	}
	return states, nil
}

// FilterVisible returns the recommendation records of recs that v may see ([domain.CanSeeRecommendation]), in order.
// Code that got them through [VisibleRecommendations] need not filter again; projections do anyway, as a second line
// of defence.
func FilterVisible(app core.App, v domain.Viewer, recs []*core.Record) ([]*core.Record, error) {
	states, err := RecommendationStates(app, recs)
	if err != nil {
		return nil, err
	}
	out := make([]*core.Record, 0, len(recs))
	for i, r := range recs {
		if r.Collection().Name == Recommendations && domain.CanSeeRecommendation(v, states[i]) {
			out = append(out, r)
		}
	}
	return out, nil
}

// IsHidden reports whether the recommendation record r is hidden (hidden or hidden_kind set, as [Shown]).
func IsHidden(r *core.Record) bool {
	return r.GetBool("hidden") || r.GetString("hidden_kind") != ""
}

func anyStrings(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
