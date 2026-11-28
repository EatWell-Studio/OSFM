package api

import (
	"errors"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// The queries of the map data (T027). Which recommendations count is decided in one place, countedOn: those the
// caller may see (store.RecommendationVisibleTo) that are not hidden (store.Shown), with a known attitude, and only the
// newest of each author at a place — domain.CountAttitudes as SQL. M2-Q1 allows one live recommendation per member
// and place already; the last condition makes sure a main-name and a pseudonym recommendation of one member could
// never count as two people, nor show up as two signatures of one place. The counts, the time slots and the filters on
// them come from one grouped query over the places of the area; whether a place exists at all is
// store.HasVisibleRecommendation (M2-Q3), so that the author and the maintainers see a place whose only recommendation
// is hidden, with counts of zero.

// mapBox is minLon, minLat, maxLon, maxLat in WGS-84 degrees.
type mapBox [4]float64

// intersect returns the part of b inside o, and whether they overlap at all.
func (b mapBox) intersect(o mapBox) (mapBox, bool) {
	out := mapBox{max(b[0], o[0]), max(b[1], o[1]), min(b[2], o[2]), min(b[3], o[3])}
	return out, out[0] <= out[2] && out[1] <= out[3]
}

var errMapParam = errors.New("api: invalid map parameter")

// parseMapBBox parses minLon,minLat,maxLon,maxLat.
func parseMapBBox(s string) (mapBox, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return mapBox{}, errMapParam
	}
	var b mapBox
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return mapBox{}, errMapParam
		}
		b[i] = v
	}
	if b[0] < -180 || b[2] > 180 || b[1] < -90 || b[3] > 90 || b[0] >= b[2] || b[1] >= b[3] {
		return mapBox{}, errMapParam
	}
	return b, nil
}

// mapTimeSlots are the time slots of recommendations, in the order the map data lists them.
var mapTimeSlots = []string{"breakfast", "lunch", "afternoon", "dinner", "late_night"}

// Limits of the filters, so that a request cannot build an arbitrarily large query.
const (
	maxMapFilterValues = 50
	maxMapByNames      = 20
	maxMapByNameLength = 200
)

// mapCuisineValue is the form of an OSM cuisine value; values outside the instance's list simply match nothing.
var mapCuisineValue = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)

// mapFilter is the filter of a map request (A3).
type mapFilter struct {
	slots, categories, cuisines, names []string
	mustTry, noAvoid                   bool
}

// parseMapFilter reads the filter parameters of q; errMapParam for a value it does not accept.
func parseMapFilter(q url.Values) (mapFilter, error) {
	var f mapFilter
	var err error
	if f.slots, err = mapEnumParam(q[MapParamSlots], func(v string) bool { return slices.Contains(mapTimeSlots, v) }); err != nil {
		return f, err
	}
	if f.categories, err = mapEnumParam(q[MapParamCategory], func(v string) bool {
		return slices.Contains(store.PlaceCategories, v)
	}); err != nil {
		return f, err
	}
	if f.cuisines, err = mapEnumParam(q[MapParamCuisine], mapCuisineValue.MatchString); err != nil {
		return f, err
	}
	for _, name := range q[MapParamBy] {
		if name == "" {
			continue
		}
		if len(name) > maxMapByNameLength || len(f.names) == maxMapByNames {
			return f, errMapParam
		}
		if !slices.Contains(f.names, name) {
			f.names = append(f.names, name)
		}
	}
	if f.mustTry, err = mapFlagParam(q.Get(MapParamMustTry)); err != nil {
		return f, err
	}
	f.noAvoid, err = mapFlagParam(q.Get(MapParamNoAvoid))
	return f, err
}

// mapEnumParam splits the values of a list parameter at commas, drops empty ones and duplicates, and checks each.
func mapEnumParam(values []string, valid func(string) bool) ([]string, error) {
	var out []string
	for _, v := range values {
		for _, item := range strings.Split(v, ",") {
			item = strings.TrimSpace(item)
			if item == "" || slices.Contains(out, item) {
				continue
			}
			if !valid(item) || len(out) == maxMapFilterValues {
				return nil, errMapParam
			}
			out = append(out, item)
		}
	}
	return out, nil
}

// mapFlagParam reads a flag: 1 or true is set; absent, empty, 0 or false is not.
func mapFlagParam(v string) (bool, error) {
	switch v {
	case "1", "true":
		return true, nil
	case "", "0", "false":
		return false, nil
	}
	return false, errMapParam
}

// countedOn returns the condition that the recommendation under alias r counts for v (see the top of this file).
func countedOn(v domain.Viewer, r string) dbx.Expression {
	known := make([]any, len(domain.Attitudes))
	for i, a := range domain.Attitudes {
		known[i] = a
	}
	counts := func(alias string) dbx.Expression {
		return dbx.And(store.RecommendationVisibleTo(v, alias), store.Shown(alias), dbx.In(alias+".attitude", known...))
	}
	const n = "rn" // a newer counted recommendation of the same author at the same place
	newer := sqlExp{"NOT EXISTS (SELECT 1 FROM {{" + store.Recommendations + "}} AS [[" + n + "]] WHERE ",
		dbx.And(
			dbx.NewExp("[["+n+".place]] = [["+r+".place]] AND [["+n+".author]] = [["+r+".author]]"),
			dbx.NewExp("([["+n+".created]] > [["+r+".created]] OR ([["+n+".created]] = [["+r+".created]] AND [["+
				n+".uuid]] > [["+r+".uuid]]))"),
			counts(n),
		), ")"}
	return dbx.And(counts(r), newer)
}

// sqlExp is the expression inner between prefix and suffix, to put a built condition into SQL around it.
type sqlExp struct {
	prefix string
	inner  dbx.Expression
	suffix string
}

// Build implements dbx.Expression.
func (e sqlExp) Build(db *dbx.DB, params dbx.Params) string {
	return e.prefix + e.inner.Build(db, params) + e.suffix
}

// mapRow is a row of the grouped query.
type mapRow struct {
	UUID      string  `db:"uuid"`
	Name      string  `db:"name"`
	Lat       float64 `db:"lat"`
	Lon       float64 `db:"lon"`
	Precision string  `db:"precision"`
	Category  string  `db:"category"`
	MustTry   int     `db:"must_try"`
	Good      int     `db:"good"`
	OK        int     `db:"ok"`
	Avoid     int     `db:"avoid"`
	Slots     string  `db:"slots"` // one character per time slot of mapTimeSlots: 1 if a counted recommendation has it
}

// queryMapPlaces returns the places of area that v may see and that pass f, with their counts and time slots, sorted
// by name, then uuid (never by attitude, A3).
func queryMapPlaces(app core.App, v domain.Viewer, area mapBox, f mapFilter) ([]MapPlace, error) {
	out := []MapPlace{}
	var bySigner dbx.Expression
	if len(f.names) > 0 {
		signers, err := signersNamed(app, f.names)
		if err != nil {
			return nil, err
		}
		if signers == nil {
			return out, nil
		}
		bySigner = signers
	}
	var rows []mapRow
	if err := mapPlacesQuery(app, v, area, f, bySigner).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		p := MapPlace{UUID: row.UUID, Name: row.Name, Lat: row.Lat, Lon: row.Lon, Precision: row.Precision,
			Category: row.Category, TimeSlots: []string{},
			Counts: PlaceCounts{MustTry: row.MustTry, Good: row.Good, OK: row.OK, Avoid: row.Avoid}}
		for i, s := range mapTimeSlots {
			if i < len(row.Slots) && row.Slots[i] == '1' {
				p.TimeSlots = append(p.TimeSlots, s)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// mapPlacesQuery returns the grouped query of queryMapPlaces, with bySigner (from signersNamed; nil for no filter by
// signature) in place of f.names.
func mapPlacesQuery(app core.App, v domain.Viewer, area mapBox, f mapFilter, bySigner dbx.Expression) *dbx.SelectQuery {
	const r = "r"
	attitude := func(a string) string {
		return "COUNT(CASE WHEN [[" + r + ".attitude]] = '" + a + "' THEN 1 END)"
	}
	slotFlags := make([]string, len(mapTimeSlots))
	for i, s := range mapTimeSlots {
		// time_slots is a JSON array of the fixed values above, none of which contains a quote.
		slotFlags[i] = "MAX(CASE WHEN instr([[" + r + ".time_slots]], '\"" + s + "\"') > 0 THEN 1 ELSE 0 END)"
	}
	q := app.DB().Select(
		store.Places+".uuid AS uuid",
		store.Places+".name AS name",
		store.Places+".lat AS lat",
		store.Places+".lon AS lon",
		store.Places+".precision AS precision",
		store.Places+".category AS category",
		attitude(domain.AttitudeMustTry)+" AS must_try",
		attitude(domain.AttitudeGood)+" AS good",
		attitude(domain.AttitudeOK)+" AS ok",
		attitude(domain.AttitudeAvoid)+" AS avoid",
		"("+strings.Join(slotFlags, " || ")+") AS slots",
	).
		From(store.Places).
		LeftJoin(store.Recommendations+" "+r, dbx.And(
			dbx.NewExp("[["+r+".place]] = [["+store.Places+".id]]"),
			countedOn(v, r),
		)).
		Where(store.NotDeleted(store.Places)).
		AndWhere(dbx.Between(store.Places+".lat", area[1], area[3])).
		AndWhere(dbx.Between(store.Places+".lon", area[0], area[2])).
		AndWhere(store.HasVisibleRecommendation(v, nil)).
		GroupBy(store.Places+".id").
		OrderBy(store.Places+".name", store.Places+".uuid")

	if len(f.categories) > 0 {
		q.AndWhere(dbx.In(store.Places+".category", anyOf(f.categories)...))
	}
	if len(f.cuisines) > 0 {
		q.AndWhere(sqlExp{"EXISTS (SELECT 1 FROM json_each(CASE WHEN json_valid([[" + store.Places +
			".cuisine]]) THEN [[" + store.Places + ".cuisine]] ELSE '[]' END) AS [[pc]] WHERE ",
			dbx.In("pc.value", anyOf(f.cuisines)...), ")"})
	}
	if f.mustTry {
		q.AndHaving(dbx.NewExp(attitude(domain.AttitudeMustTry) + " > 0"))
	}
	if f.noAvoid {
		q.AndHaving(dbx.NewExp(attitude(domain.AttitudeAvoid) + " = 0"))
	}
	if len(f.slots) > 0 {
		var alternatives []string
		for i, s := range mapTimeSlots {
			if slices.Contains(f.slots, s) {
				alternatives = append(alternatives, slotFlags[i]+" = 1")
			}
		}
		q.AndHaving(dbx.NewExp("(" + strings.Join(alternatives, " OR ") + ")"))
	}
	if bySigner != nil {
		q.AndHaving(sqlExp{"MAX(CASE WHEN ", bySigner, " THEN 1 ELSE 0 END) = 1"})
	}

	return q
}

// signersNamed returns the condition, on the recommendation under alias r, that it is signed with one of names:
// which (author, display_as) pairs resolve to which name is decided by domain.ResolveSignature alone, so a main name
// never matches its member's pseudonym recommendations, and the empty name of a member who left matches nothing. It
// returns nil when no pair resolves to any of names.
func signersNamed(app core.App, names []string) (dbx.Expression, error) {
	members, err := store.Query(app, store.Members).IncludeDeleted().All()
	if err != nil {
		return nil, err
	}
	var pairs []dbx.Expression
	for _, m := range members {
		author := authorOf(m)
		for _, as := range []string{domain.SignAsMain, domain.SignAsPseudonym} {
			sig := domain.ResolveSignature(as, author)
			if sig.DisplayName != "" && slices.Contains(names, sig.DisplayName) {
				pairs = append(pairs, dbx.HashExp{"r.author": m.Id, "r.display_as": as})
			}
		}
	}
	if len(pairs) == 0 {
		return nil, nil
	}
	return dbx.Or(pairs...), nil
}

// querySignatures returns the signatures of the counted recommendations of the places in area, for v: each name once,
// sorted, without the empty name of a member who left (domain.ResolveSignature). Only strings: not whether a name is a
// main name or a pseudonym, and no uuid.
func querySignatures(app core.App, v domain.Viewer, area mapBox) ([]string, error) {
	var pairs []struct {
		Author    string `db:"author"`
		DisplayAs string `db:"display_as"`
	}
	err := app.DB().Select("r.author AS author", "r.display_as AS display_as").Distinct(true).
		From(store.Recommendations+" r").
		InnerJoin(store.Places, dbx.NewExp("[["+store.Places+".id]] = [[r.place]]")).
		Where(store.NotDeleted(store.Places)).
		AndWhere(dbx.Between(store.Places+".lat", area[1], area[3])).
		AndWhere(dbx.Between(store.Places+".lon", area[0], area[2])).
		AndWhere(countedOn(v, "r")).
		All(&pairs)
	if err != nil {
		return nil, err
	}
	ids := []any{}
	seen := map[string]bool{}
	for _, p := range pairs {
		if !seen[p.Author] {
			seen[p.Author] = true
			ids = append(ids, p.Author)
		}
	}
	authors := map[string]domain.Author{}
	if len(ids) > 0 {
		members, err := store.Query(app, store.Members).IncludeDeleted().Where(dbx.In(store.Members+".id", ids...)).All()
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			authors[m.Id] = authorOf(m)
		}
	}
	names := []string{}
	for _, p := range pairs {
		author, ok := authors[p.Author]
		if !ok {
			continue
		}
		name := domain.ResolveSignature(p.DisplayAs, author).DisplayName
		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return names, nil
}

func anyOf(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
