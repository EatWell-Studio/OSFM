package api_test

import (
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// saveRecord writes a made-up record of collection with values, through the store hooks.
func saveRecord(t *testing.T, a *testapp.App, collection string, values map[string]any) *core.Record {
	t.Helper()
	var r *core.Record
	if collection == store.Members {
		var err error
		if r, err = auth.NewMemberRecord(a); err != nil {
			t.Fatal(err)
		}
	} else {
		c, err := a.FindCachedCollectionByNameOrId(collection)
		if err != nil {
			t.Fatal(err)
		}
		r = core.NewRecord(c)
	}
	for k, v := range values {
		r.Set(k, v)
	}
	if err := a.Save(r); err != nil {
		t.Fatalf("save %s: %v", collection, err)
	}
	return r
}

// exportOf downloads the personal export with c and checks the response's headers.
func exportOf(t *testing.T, c *testapp.Client) (api.PersonalExport, string) {
	t.Helper()
	r := c.Get(api.PersonalExportPath).Expect(http.StatusOK)
	h := r.Header()
	if got := h.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control %q", got)
	}
	if got := h.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type %q", got)
	}
	want := `attachment; filename="` + api.PersonalExportFilePrefix + time.Now().UTC().Format(time.DateOnly) + `.json"`
	if got := h.Get("Content-Disposition"); got != want {
		t.Errorf("Content-Disposition %q, want %q", got, want)
	}
	ex := testapp.Decode[api.PersonalExport](r)
	if ex.Schema != "osfm-personal/1" {
		t.Errorf("osfm:schema %q", ex.Schema)
	}
	body := r.Body.String()
	if !strings.Contains(body, `"osfm:schema": "osfm-personal/1"`) {
		t.Error("the file does not name its schema under osfm:schema")
	}
	return ex, body
}

// absent fails the test for every value of values that occurs in body.
func absent(t *testing.T, who, body string, values ...string) {
	t.Helper()
	for _, v := range values {
		if v != "" && strings.Contains(body, v) {
			t.Errorf("%s's export contains %q", who, v)
		}
	}
}

// memberStrings returns what identifies the member m: uuid, login name, main name and pseudonym.
func memberStrings(m *core.Record) []string {
	return []string{m.GetString(store.FieldUUID), m.GetString("login_name"), m.GetString("display_name"),
		m.GetString("pseudonym")}
}

// TestPersonalExport: a member downloads all of their own data — private, hidden and pseudonym recommendations with
// their places and revisions, invites, reports, notices — and nothing about anyone else but the inviter's main name
// (E6). As the four identities; no internal IDs.
func TestPersonalExport(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	a.As(testapp.Anonymous).Get(api.PersonalExportPath).Expect(http.StatusUnauthorized)

	// Another member, invited by the author with one of the author's invites, with a pseudonym recommendation the
	// author reported and a report of the author's recommendation.
	joined := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	other := saveRecord(t, a, store.Members, map[string]any{"login_name": "test_other", "display_name": "Test Other",
		"pseudonym": "Test Other Pen", "role": auth.RoleMember, "invited_by": a.Author.Id, "joined_at": joined})
	otherPlace := a.SeedPlace(testapp.PlaceSeed{Name: "Test Tea House", Lat: 0.33, Lon: 7.07})
	otherRec := a.SeedRecommendation(testapp.RecommendationSeed{Place: otherPlace, Author: other,
		DisplayAs: domain.SignAsPseudonym, Text: "Test other text"})

	// More of the author's recommendations: a hidden one with every content field, a deleted one and one of a place
	// a maintainer deleted (M2-Q20: exported, marked; the other member's recommendation of that place is not).
	hiddenPlace := a.SeedPlace(testapp.PlaceSeed{Name: "Test Hidden Grill", Lat: 0.332, Lon: 7.072})
	hiddenRec := a.SeedRecommendation(testapp.RecommendationSeed{Place: hiddenPlace, Author: a.Author,
		Attitude: domain.AttitudeAvoid, Text: "Test hidden text", Dishes: []string{"Test Dish"},
		TimeSlots: []string{"lunch", "dinner"}, Season: "Test season",
		Sources:   []map[string]any{{"url": "https://example.test/source", "kind": "visited", "excerpt": "Test excerpt"}},
		VisitedOn: "2026-03-04", HiddenKind: store.HiddenByMaintainer, HiddenReason: "Test hidden reason",
		HiddenBy: a.Maintainer, EditedByMaintainerAt: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)})
	deletedPlace := a.SeedPlace(testapp.PlaceSeed{Name: "Test Deleted Diner", Lat: 0.334, Lon: 7.074})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: deletedPlace, Author: a.Author,
		Text: "Test deleted text", Deleted: true})
	gonePlace := a.SeedPlace(testapp.PlaceSeed{Name: "Test Gone Canteen", Lat: 0.336, Lon: 7.076})
	time.Sleep(2 * time.Millisecond) // created has millisecond precision: goneRec is the newest
	goneRec := a.SeedRecommendation(testapp.RecommendationSeed{Place: gonePlace, Author: a.Author,
		DisplayAs: domain.SignAsPseudonym, Text: "Test gone text"})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: gonePlace, Author: other, Text: "Test other gone text"})
	if err := store.SoftDelete(a, gonePlace); err != nil {
		t.Fatal(err)
	}

	// Revisions: of the main-name recommendation, by a maintainer and then by the author; of the hidden one, its
	// full content.
	before := store.SnapshotOf(a.Recommendation)
	before.Text = "Test text before the maintainer"
	saveRecord(t, a, store.RecommendationRevisions, map[string]any{"recommendation": a.Recommendation.Id,
		"editor_role": "maintainer", "editor": a.Maintainer.Id, "snapshot": before})
	time.Sleep(2 * time.Millisecond) // created has millisecond precision
	before.Text = "Test text before the author"
	saveRecord(t, a, store.RecommendationRevisions, map[string]any{"recommendation": a.Recommendation.Id,
		"editor_role": "author", "editor": a.Author.Id, "snapshot": before})
	saveRecord(t, a, store.RecommendationRevisions, map[string]any{"recommendation": hiddenRec.Id,
		"editor_role": "author", "editor": a.Author.Id, "snapshot": store.SnapshotOf(hiddenRec)})
	// A revision of the other member's recommendation.
	saveRecord(t, a, store.RecommendationRevisions, map[string]any{"recommendation": otherRec.Id,
		"editor_role": "author", "editor": other.Id, "snapshot": map[string]any{"text": "Test other old text"}})

	saveRecord(t, a, store.Reports, map[string]any{"recommendation": otherRec.Id, "reporter": a.Author.Id,
		"reason": store.ReasonAdvertising, "note": "Test report note"})
	saveRecord(t, a, store.Reports, map[string]any{"recommendation": a.Recommendation.Id, "reporter": other.Id,
		"reason": store.ReasonAbuse, "note": "Test foreign note"})
	if err := domain.Notify(store.NoticesIn(a), a.Author.Id, domain.NoticeHiddenByMaintainer,
		hiddenRec.GetString(store.FieldUUID)); err != nil {
		t.Fatal(err)
	}
	if err := domain.Notify(store.NoticesIn(a), other.Id, domain.NoticeRestored,
		otherRec.GetString(store.FieldUUID)); err != nil {
		t.Fatal(err)
	}
	usedInvite := saveRecord(t, a, store.Invites, map[string]any{"code_hash": strings.Repeat("2a", 32),
		"created_by": a.Author.Id, "used_by": other.Id, "used_at": "2026-02-01 00:00:00Z",
		"expires_at": "2026-02-08 00:00:00Z"})
	time.Sleep(2 * time.Millisecond)
	activeInvite := saveRecord(t, a, store.Invites, map[string]any{"code_hash": strings.Repeat("2b", 32),
		"created_by": a.Author.Id, "expires_at": "2099-01-01 00:00:00Z"})
	saveRecord(t, a, store.Invites, map[string]any{"code_hash": strings.Repeat("2c", 32),
		"created_by": a.Maintainer.Id, "expires_at": "2099-01-01 00:00:00Z"})

	ex, body := exportOf(t, a.As(testapp.Author))
	if time.Since(ex.ExportedAt) > time.Minute || time.Until(ex.ExportedAt) > time.Second {
		t.Errorf("exportedAt %v", ex.ExportedAt)
	}
	wantAccount := api.PersonalAccount{UUID: a.Author.GetString(store.FieldUUID), LoginName: "test_author",
		DisplayName: "Test Author", Pseudonym: "Test Pen Name",
		ExternalLinks: []api.ExternalLink{{URL: testapp.AuthorLink, Label: "Test Blog"}}, Role: auth.RoleMember,
		JoinedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), InvitedBy: "Test Maintainer"}
	if !reflect.DeepEqual(ex.Account, wantAccount) {
		t.Errorf("account %+v, want %+v", ex.Account, wantAccount)
	}

	if len(ex.Invites) != 2 || ex.Invites[0].UUID != activeInvite.GetString(store.FieldUUID) ||
		ex.Invites[0].State != auth.InviteActive || ex.Invites[0].UsedAt != nil ||
		ex.Invites[1].UUID != usedInvite.GetString(store.FieldUUID) || ex.Invites[1].State != auth.InviteUsed ||
		ex.Invites[1].UsedAt == nil || !ex.Invites[1].UsedAt.Equal(joined) {
		t.Errorf("invites %+v", ex.Invites)
	}
	if len(ex.Credentials) != 0 {
		t.Errorf("credentials %+v (the seeded author has none)", ex.Credentials)
	}

	recs := map[string]api.PersonalRecommendation{}
	for _, r := range ex.Recommendations {
		recs[r.UUID] = r
	}
	if len(ex.Recommendations) != 5 || len(recs) != 5 {
		t.Fatalf("%d recommendations, want the five live ones: %+v", len(ex.Recommendations),
			ex.Recommendations)
	}
	main := recs[a.Recommendation.GetString(store.FieldUUID)]
	if main.DisplayAs != domain.SignAsMain || main.Visibility != domain.VisibilityCircle || main.Hidden != nil ||
		main.Text != "Test recommendation text" || main.Place != (api.PersonalPlace{UUID: a.Place.GetString(store.FieldUUID),
		Name: "Test Dumpling House", Lat: 0.34, Lon: 7.06}) {
		t.Errorf("main-name recommendation %+v", main)
	}
	if len(main.Revisions) != 2 || main.Revisions[0].EditorRole != "author" ||
		main.Revisions[0].Content.Text != "Test text before the author" || main.Revisions[1].EditorRole != "maintainer" ||
		main.Revisions[1].Content.Text != "Test text before the maintainer" || main.Revisions[0].UUID == "" {
		t.Errorf("revisions %+v", main.Revisions)
	}
	if p := recs[a.PseudonymRecommendation.GetString(store.FieldUUID)]; p.DisplayAs != domain.SignAsPseudonym ||
		p.Text != "Test pen name text" || p.Place.Name != "Test Noodle Stall" || len(p.Revisions) != 0 {
		t.Errorf("pseudonym recommendation %+v", p)
	}
	if p := recs[a.PrivateRecommendation.GetString(store.FieldUUID)]; p.Visibility != domain.VisibilityPrivate ||
		p.Text != "Test private text" || p.Place.Name != "Test Secret Bakery" {
		t.Errorf("private recommendation %+v", p)
	}
	h := recs[hiddenRec.GetString(store.FieldUUID)]
	wantSources := []api.Source{{URL: "https://example.test/source", Kind: "visited", Excerpt: "Test excerpt"}}
	if h.Hidden == nil || *h.Hidden != (api.HiddenInfo{Kind: store.HiddenByMaintainer, Reason: "Test hidden reason"}) ||
		h.Attitude != domain.AttitudeAvoid || !reflect.DeepEqual(h.Dishes, []string{"Test Dish"}) ||
		!reflect.DeepEqual(h.TimeSlots, []string{"lunch", "dinner"}) || h.Season != "Test season" ||
		!reflect.DeepEqual(h.Sources, wantSources) || h.VisitedOn != "2026-03-04" || h.EditedByMaintainerAt == nil {
		t.Errorf("hidden recommendation %+v", h)
	}
	wantContent := api.PersonalRevisionContent{Attitude: domain.AttitudeAvoid, Text: "Test hidden text",
		Dishes: []string{"Test Dish"}, TimeSlots: []string{"lunch", "dinner"}, Season: "Test season",
		Sources: wantSources, VisitedOn: "2026-03-04"}
	if len(h.Revisions) != 1 || !reflect.DeepEqual(h.Revisions[0].Content, wantContent) {
		t.Errorf("revision of the hidden recommendation %+v, want %+v", h.Revisions, wantContent)
	}

	for uuid, r := range recs {
		if r.PlaceDeleted != (uuid == goneRec.GetString(store.FieldUUID)) {
			t.Errorf("recommendation %s: placeDeleted %v", r.Text, r.PlaceDeleted)
		}
	}
	if g := recs[goneRec.GetString(store.FieldUUID)]; g.Text != "Test gone text" || g.DisplayAs != domain.SignAsPseudonym ||
		g.Place != (api.PersonalPlace{UUID: gonePlace.GetString(store.FieldUUID), Name: "Test Gone Canteen",
			Lat: 0.336, Lon: 7.076}) {
		t.Errorf("recommendation of the deleted place %+v", g)
	}
	// Newest first, across the visible ones and the exception.
	if ex.Recommendations[0].UUID != goneRec.GetString(store.FieldUUID) {
		t.Errorf("first recommendation %q, want the newest (of the deleted place)", ex.Recommendations[0].Text)
	}

	if len(ex.Reports) != 1 || ex.Reports[0].Recommendation != otherRec.GetString(store.FieldUUID) ||
		ex.Reports[0].Reason != store.ReasonAdvertising || ex.Reports[0].Note != "Test report note" ||
		ex.Reports[0].CreatedAt.IsZero() {
		t.Errorf("reports %+v", ex.Reports)
	}
	if len(ex.Notices) != 1 || ex.Notices[0].Kind != domain.NoticeHiddenByMaintainer ||
		ex.Notices[0].Ref != hiddenRec.GetString(store.FieldUUID) || ex.Notices[0].ReadAt != nil ||
		ex.Notices[0].UUID == "" {
		t.Errorf("notices %+v", ex.Notices)
	}

	// Nothing about other members (the inviter's main name aside), nothing of what is deleted, nothing of the
	// reported recommendation but its uuid.
	absent(t, "the author", body, memberStrings(other)...)
	absent(t, "the author", body, memberStrings(a.Member)...)
	absent(t, "the author", body, a.Maintainer.GetString(store.FieldUUID), a.Maintainer.GetString("login_name"))
	absent(t, "the author", body, "Test other text", "Test other old text", "Test foreign note", "Test Tea House",
		"Test deleted text", "Test Deleted Diner", "Test other gone text", "editor\"",
		"reporter", "usedBy", "used_by")
	if strings.Count(body, "Test Maintainer") != 1 {
		t.Error("the inviter's main name should appear once, as invitedBy")
	}

	// The member (no recommendations) and the maintainer (invited by nobody) get their own data only.
	mex, mbody := exportOf(t, a.As(testapp.Member))
	if mex.Account.UUID != a.Member.GetString(store.FieldUUID) || mex.Account.InvitedBy != "Test Maintainer" ||
		mex.Account.Pseudonym != "" || len(mex.Account.ExternalLinks) != 0 || len(mex.Recommendations) != 0 ||
		len(mex.Invites) != 0 || len(mex.Reports) != 0 || len(mex.Notices) != 0 {
		t.Errorf("member's export %+v", mex)
	}
	for _, field := range []string{`"invites": []`, `"credentials": []`, `"recommendations": []`, `"reports": []`,
		`"notices": []`, `"externalLinks": []`} {
		if !strings.Contains(mbody, field) {
			t.Errorf("member's export: no %s", field)
		}
	}
	absent(t, "the member", mbody, memberStrings(a.Author)...)
	absent(t, "the member", mbody, memberStrings(other)...)
	absent(t, "the member", mbody, "Test private text", "Test pen name text", a.Maintainer.GetString(store.FieldUUID))

	xex, xbody := exportOf(t, a.As(testapp.Maintainer))
	if xex.Account.Role != auth.RoleMaintainer || xex.Account.InvitedBy != "" || len(xex.Recommendations) != 0 ||
		len(xex.Invites) != 1 {
		t.Errorf("maintainer's export %+v", xex)
	}
	absent(t, "the maintainer", xbody, memberStrings(a.Author)...)
	absent(t, "the maintainer", xbody, memberStrings(other)...)
	absent(t, "the maintainer", xbody, "Test private text", "Test hidden text", "Test hidden reason")

	a.CheckNoInternalIDs()
}

// TestPersonalExportBudget: the export has a per-member budget of its own: 429 with Retry-After when it is spent,
// other members unaffected.
func TestPersonalExportBudget(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	for range 3 {
		a.As(testapp.Member).Get(api.PersonalExportPath).Expect(http.StatusOK)
	}
	r := a.As(testapp.Member).Get(api.PersonalExportPath).Expect(http.StatusTooManyRequests)
	if e := testapp.Decode[api.Error](r); e.Code != api.CodeRateLimited {
		t.Errorf("429 body %+v", e)
	}
	if s, err := strconv.Atoi(r.Header().Get("Retry-After")); err != nil || s < 1 || s > 60 {
		t.Errorf("Retry-After %q", r.Header().Get("Retry-After"))
	}
	a.As(testapp.Author).Get(api.PersonalExportPath).Expect(http.StatusOK)
}
