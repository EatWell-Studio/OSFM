package main

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// visibilityFixture is the seeded instance with, besides the author's three recommendations (main name, pseudonym,
// private), recommendations in every other state.
type visibilityFixture struct {
	*testapp.App
	hidden, deleted, maintainerPrivate, federated, onDeletedPlace     *core.Record
	hiddenPlace, deletedRecPlace, maintainerPlace, deletedPlace, both *core.Record
}

func newVisibilityFixture(t *testing.T) *visibilityFixture {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	f := &visibilityFixture{App: a}
	// The member's recommendation hidden by a maintainer, the only one of its place.
	f.hiddenPlace = a.SeedPlace(testapp.PlaceSeed{Name: "Test Hidden Grill"})
	f.hidden = a.SeedRecommendation(testapp.RecommendationSeed{Place: f.hiddenPlace, Author: a.Member,
		Attitude: domain.AttitudeAvoid, HiddenKind: store.HiddenByMaintainer, HiddenReason: "Test hidden reason",
		HiddenBy: a.Maintainer})
	// The member's deleted recommendation, the only one of its place.
	f.deletedRecPlace = a.SeedPlace(testapp.PlaceSeed{Name: "Test Gone Diner"})
	f.deleted = a.SeedRecommendation(testapp.RecommendationSeed{Place: f.deletedRecPlace, Author: a.Member,
		Deleted: true})
	// The maintainer's private recommendation, the only one of its place.
	f.maintainerPlace = a.SeedPlace(testapp.PlaceSeed{Name: "Test Quiet Cafe"})
	f.maintainerPrivate = a.SeedRecommendation(testapp.RecommendationSeed{Place: f.maintainerPlace,
		Author: a.Maintainer, Visibility: domain.VisibilityPrivate})
	// A circle recommendation of a deleted place.
	f.deletedPlace = a.SeedPlace(testapp.PlaceSeed{Name: "Test Closed Bar", Deleted: true})
	f.onDeletedPlace = a.SeedRecommendation(testapp.RecommendationSeed{Place: f.deletedPlace, Author: a.Member})
	// A place with the author's private recommendation and the maintainer's federated one: it exists for everybody,
	// but the member sees only the maintainer's recommendation.
	f.both = a.SeedPlace(testapp.PlaceSeed{Name: "Test Shared Kitchen"})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: f.both, Author: a.Author,
		Visibility: domain.VisibilityPrivate, Attitude: domain.AttitudeAvoid})
	f.federated = a.SeedRecommendation(testapp.RecommendationSeed{Place: f.both, Author: a.Maintainer,
		Visibility: domain.VisibilityFederated, Attitude: domain.AttitudeMustTry})
	return f
}

func (f *visibilityFixture) viewer(id testapp.Identity) domain.Viewer {
	switch id {
	case testapp.Member:
		return api.MemberViewer(f.Member)
	case testapp.Author:
		return api.MemberViewer(f.Author)
	case testapp.Maintainer:
		return api.MemberViewer(f.Maintainer)
	}
	return api.MemberViewer(nil)
}

func uuidsOf(records []*core.Record) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.GetString(store.FieldUUID))
	}
	slices.Sort(out)
	return out
}

func sortedUUIDs(records ...*core.Record) []string { return uuidsOf(records) }

// TestVisibility is the acceptance test of the single read path (C4, C6, M2-Q3; architecture §3.4): the four
// identities read the seeded recommendations and places through store.VisibleRecommendations and
// store.VisiblePlaces. Private recommendations are seen by their author only, hidden ones by their author and the
// maintainers, deleted ones by nobody; a place whose only recommendation is someone else's private one does not exist
// for others. The queries agree with the rules of internal/domain on every record.
func TestVisibility(t *testing.T) {
	f := newVisibilityFixture(t)
	a := f.App
	for id, want := range map[testapp.Identity]struct{ recs, places []string }{
		testapp.Anonymous: {},
		testapp.Member: {
			recs:   sortedUUIDs(a.Recommendation, a.PseudonymRecommendation, f.hidden, f.federated),
			places: sortedUUIDs(a.Place, a.PseudonymPlace, f.hiddenPlace, f.both),
		},
		testapp.Author: {
			recs: append(sortedUUIDs(a.Recommendation, a.PseudonymRecommendation, a.PrivateRecommendation,
				f.federated), uuidsOf(authorPrivateAt(t, a, f.both))...),
			places: sortedUUIDs(a.Place, a.PseudonymPlace, a.PrivatePlace, f.both),
		},
		testapp.Maintainer: {
			recs: sortedUUIDs(a.Recommendation, a.PseudonymRecommendation, f.hidden, f.maintainerPrivate,
				f.federated),
			places: sortedUUIDs(a.Place, a.PseudonymPlace, f.hiddenPlace, f.maintainerPlace, f.both),
		},
	} {
		t.Run(string(id), func(t *testing.T) {
			v := f.viewer(id)
			recs, err := store.VisibleRecommendations(a, v).All()
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(want.recs)
			if got := uuidsOf(recs); !slices.Equal(got, want.recs) {
				t.Errorf("recommendations %v, want %v", got, want.recs)
			}
			places, err := store.VisiblePlaces(a, v).All()
			if err != nil {
				t.Fatal(err)
			}
			if got := uuidsOf(places); !slices.Equal(got, want.places) {
				t.Errorf("places %v, want %v", got, want.places)
			}
			// IncludeDeleted does not bring deleted recommendations back into the visible ones.
			if n, err := store.VisibleRecommendations(a, v).IncludeDeleted().Count(); err != nil ||
				int(n) != len(want.recs) {
				t.Errorf("with IncludeDeleted: %d, %v", n, err)
			}
			checkAgreesWithDomain(t, a, v, recs, places)
		})
	}
}

// authorPrivateAt returns the author's recommendations of place.
func authorPrivateAt(t *testing.T, a *testapp.App, place *core.Record) []*core.Record {
	t.Helper()
	recs, err := store.Query(a, store.Recommendations).
		Where(dbx.HashExp{"place": place.Id, "author": a.Author.Id}).All()
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

// checkAgreesWithDomain compares the visible records with domain.CanSeeRecommendation and domain.CanSeePlace over
// every record, deleted ones included.
func checkAgreesWithDomain(t *testing.T, a *testapp.App, v domain.Viewer, visibleRecs, visiblePlaces []*core.Record) {
	t.Helper()
	all, err := store.Query(a, store.Recommendations).IncludeDeleted().All()
	if err != nil {
		t.Fatal(err)
	}
	visible := uuidsOf(visibleRecs)
	states, err := store.RecommendationStates(a, all)
	if err != nil {
		t.Fatal(err)
	}
	byPlace := map[string][]domain.Recommendation{}
	for i, r := range all {
		state := states[i]
		byPlace[r.GetString("place")] = append(byPlace[r.GetString("place")], state)
		if got := slices.Contains(visible, r.GetString(store.FieldUUID)); got != domain.CanSeeRecommendation(v, state) {
			t.Errorf("recommendation %q: query %v, domain %v", r.GetString("text"), got, !got)
		}
	}
	places, err := store.Query(a, store.Places).IncludeDeleted().All()
	if err != nil {
		t.Fatal(err)
	}
	shown := uuidsOf(visiblePlaces)
	for _, p := range places {
		want := domain.CanSeePlace(v, store.IsDeleted(p), byPlace[p.Id])
		if got := slices.Contains(shown, p.GetString(store.FieldUUID)); got != want {
			t.Errorf("place %q: query %v, domain %v", p.GetString("name"), got, want)
		}
	}
}

// TestVisiblePlaceFilters: conditions on a place's visible recommendations only see those (the map filters, T027):
// the author's private "avoid" excludes the shared kitchen for the author, not for the member.
func TestVisiblePlaceFilters(t *testing.T) {
	f := newVisibilityFixture(t)
	for id, want := range map[testapp.Identity]struct{ mustTry, noAvoid []string }{
		testapp.Member: {
			mustTry: sortedUUIDs(f.PseudonymPlace, f.both),
			// The hidden "avoid" is the member's own, so they see it.
			noAvoid: sortedUUIDs(f.Place, f.PseudonymPlace, f.both),
		},
		testapp.Author: {
			mustTry: sortedUUIDs(f.PseudonymPlace, f.both),
			noAvoid: sortedUUIDs(f.Place, f.PseudonymPlace, f.PrivatePlace),
		},
		testapp.Maintainer: {
			mustTry: sortedUUIDs(f.PseudonymPlace, f.both),
			// Maintainers see the hidden "avoid".
			noAvoid: sortedUUIDs(f.Place, f.PseudonymPlace, f.maintainerPlace, f.both),
		},
	} {
		v := f.viewer(id)
		got, err := store.VisiblePlaces(f, v).
			Where(store.HasVisibleRecommendation(v, dbx.HashExp{store.Rec("attitude"): domain.AttitudeMustTry})).All()
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(uuidsOf(got), want.mustTry) {
			t.Errorf("%s: must try %v, want %v", id, uuidsOf(got), want.mustTry)
		}
		got, err = store.VisiblePlaces(f, v).
			Where(dbx.Not(store.HasVisibleRecommendation(v, dbx.HashExp{store.Rec("attitude"): domain.AttitudeAvoid}))).
			All()
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(uuidsOf(got), want.noAvoid) {
			t.Errorf("%s: no avoid %v, want %v", id, uuidsOf(got), want.noAvoid)
		}
	}
}

// TestRecommendationViews: the projection signs as domain.ResolveSignature decides, carries external links only with
// a main name, tells only the author it is theirs, shows why a recommendation is hidden only to its author and the
// maintainers, and carries no member uuid or internal ID (E2, E3).
func TestRecommendationViews(t *testing.T) {
	f := newVisibilityFixture(t)
	a := f.App
	all, err := store.Query(a, store.Recommendations).IncludeDeleted().OrderBy("created").All()
	if err != nil {
		t.Fatal(err)
	}
	views := func(id testapp.Identity) map[string]api.RecommendationView {
		t.Helper()
		// Every record, the invisible ones included: the projection leaves out what the viewer may not see.
		vs, err := api.RecommendationViews(a, f.viewer(id), all)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]api.RecommendationView{}
		for _, v := range vs {
			out[v.UUID] = v
		}
		return out
	}
	uuid := func(r *core.Record) string { return r.GetString(store.FieldUUID) }

	member := views(testapp.Member)
	if len(member) != 4 {
		t.Errorf("the member gets %d views, want 4", len(member))
	}
	if _, ok := member[uuid(a.PrivateRecommendation)]; ok {
		t.Error("the member gets the author's private recommendation")
	}
	main, pen := member[uuid(a.Recommendation)], member[uuid(a.PseudonymRecommendation)]
	if main.DisplayName != "Test Author" || len(main.ExternalLinks) != 1 ||
		main.ExternalLinks[0].URL != testapp.AuthorLink || main.IsMine || main.Visibility != "" || main.Hidden != nil {
		t.Errorf("main-name recommendation for the member: %+v", main)
	}
	if pen.DisplayName != "Test Pen Name" || len(pen.ExternalLinks) != 0 || pen.IsMine || pen.AuthorLeft {
		t.Errorf("pseudonym recommendation for the member: %+v", pen)
	}
	if h := member[uuid(f.hidden)]; !h.IsMine || h.Hidden == nil || h.Hidden.Kind != store.HiddenByMaintainer ||
		h.Hidden.Reason != "Test hidden reason" || h.Visibility != domain.VisibilityCircle {
		t.Errorf("the member's own hidden recommendation: %+v", h)
	}

	author := views(testapp.Author)
	if v := author[uuid(a.PrivateRecommendation)]; !v.IsMine || v.Visibility != domain.VisibilityPrivate ||
		v.Text != "Test private text" {
		t.Errorf("the author's private recommendation: %+v", v)
	}
	if _, ok := author[uuid(f.hidden)]; ok {
		t.Error("the author sees another member's hidden recommendation")
	}
	if v := views(testapp.Maintainer)[uuid(f.hidden)]; v.IsMine || v.Hidden == nil || v.Hidden.Reason == "" {
		t.Errorf("the hidden recommendation for a maintainer: %+v", v)
	}
	if len(views(testapp.Anonymous)) != 0 {
		t.Error("views for nobody")
	}

	// No member uuid, internal ID, login name or signature kind in what a member gets; the pseudonym's view has
	// nothing of the main name.
	body, err := json.Marshal(member)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []*core.Record{a.Author, a.Member, a.Maintainer} {
		for _, s := range []string{m.Id, uuid(m), m.GetString("login_name")} {
			if strings.Contains(string(body), s) {
				t.Errorf("the member's views contain %q", s)
			}
		}
	}
	for _, s := range []string{"display_as", "displayAs", `"pseudonym"`, `"main"`, `"author"`, `"authorId"`} {
		if strings.Contains(string(body), s) {
			t.Errorf("the member's views contain %s", s)
		}
	}
	penJSON, _ := json.Marshal(pen)
	for _, s := range []string{"Test Author", testapp.AuthorLink, "externalLinks"} {
		if strings.Contains(string(penJSON), s) {
			t.Errorf("the pseudonym recommendation contains %q", s)
		}
	}

	// Once the author has left (keeping their recommendations), the main name gives way to "a member who left",
	// the pseudonym stays (M2-Q10).
	a.Author.Set("left_at", "2026-02-01 00:00:00Z")
	if err := a.Save(a.Author); err != nil {
		t.Fatal(err)
	}
	member = views(testapp.Member)
	if v := member[uuid(a.Recommendation)]; v.DisplayName != "" || !v.AuthorLeft || len(v.ExternalLinks) != 0 {
		t.Errorf("main-name recommendation of a member who left: %+v", v)
	}
	if v := member[uuid(a.PseudonymRecommendation)]; v.DisplayName != "Test Pen Name" || v.AuthorLeft {
		t.Errorf("pseudonym recommendation of a member who left: %+v", v)
	}
	a.CheckNoInternalIDs()
}

// TestNotify: domain.Notify writes the notice in the caller's transaction, with a uuid; a rolled back transaction
// leaves none.
func TestNotify(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	ref := a.Recommendation.GetString(store.FieldUUID)
	if err := a.RunInTransaction(func(tx core.App) error {
		return domain.Notify(store.NoticesIn(tx), a.Author.Id, store.NoticeMaintainerEdited, ref)
	}); err != nil {
		t.Fatal(err)
	}
	errRollback := errors.New("test rollback")
	if err := a.RunInTransaction(func(tx core.App) error {
		if err := domain.Notify(store.NoticesIn(tx), a.Author.Id, store.NoticeRestored, ref); err != nil {
			return err
		}
		return errRollback
	}); !errors.Is(err, errRollback) {
		t.Fatal(err)
	}
	rows, err := a.FindAllRecords(store.Notices)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].GetString("member") != a.Author.Id || rows[0].GetString("ref") != ref ||
		rows[0].GetString("kind") != store.NoticeMaintainerEdited || !store.IsUUID(rows[0].GetString(store.FieldUUID)) ||
		!rows[0].GetDateTime("read_at").IsZero() {
		t.Errorf("notices: %v", rows)
	}
	if err := domain.Notify(store.NoticesIn(a), a.Author.Id, "test", ref); !errors.Is(err, domain.ErrInvalidNotice) {
		t.Errorf("unknown kind: %v", err)
	}
}
