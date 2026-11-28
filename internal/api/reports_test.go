package api_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// recPath fills the {uuid} of a route with the uuid of the record r.
func recPath(pattern string, r *core.Record) string {
	return strings.Replace(pattern, "{uuid}", uuidOf(r), 1)
}

func report(c *testapp.Client, rec *core.Record, reason, note string) *testapp.Response {
	return c.Post(recPath(api.ReportPath, rec), api.ReportRequest{Reason: reason, Note: note})
}

// cardOf returns the card of place as c sees it; ok is false when c gets 404.
func cardOf(t *testing.T, c *testapp.Client, place *core.Record) (api.PlaceCard, bool) {
	t.Helper()
	r := c.Get(recPath(api.PlaceCardPath, place))
	if r.Code == http.StatusNotFound {
		return api.PlaceCard{}, false
	}
	return testapp.Decode[api.PlaceCard](r.Expect(http.StatusOK)), true
}

// recOnCard returns the recommendation rec on the card of its place as c sees it, nil when it is not there.
func recOnCard(t *testing.T, a *testapp.App, c *testapp.Client, rec *core.Record) *api.RecommendationView {
	t.Helper()
	place, err := a.FindRecordById(store.Places, rec.GetString("place"))
	if err != nil {
		t.Fatal(err)
	}
	card, ok := cardOf(t, c, place)
	if !ok {
		return nil
	}
	for _, v := range card.Recommendations {
		if v.UUID == uuidOf(rec) {
			return &v
		}
	}
	return nil
}

// audits returns the audit entries of action about the object with uuid target.
func audits(t *testing.T, a *testapp.App, action, target string) []*core.Record {
	t.Helper()
	entries, err := a.FindAllRecords(store.AuditLog, dbx.HashExp{"action": action, "target_uuid": target})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// noticeKinds returns the kinds of the notices of member about the recommendation rec, oldest first.
func noticeKinds(t *testing.T, a *testapp.App, member, rec *core.Record) []string {
	t.Helper()
	notices, err := a.FindAllRecords(store.Notices, dbx.HashExp{"member": member.Id, "ref": uuidOf(rec)})
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(notices, func(x, y *core.Record) int {
		return x.GetDateTime(store.FieldCreated).Compare(y.GetDateTime(store.FieldCreated))
	})
	out := []string{}
	for _, n := range notices {
		out = append(out, n.GetString("kind"))
	}
	return out
}

func reportsOf(t *testing.T, a *testapp.App, rec *core.Record) []*core.Record {
	t.Helper()
	reports, err := a.FindAllRecords(store.Reports, dbx.HashExp{"recommendation": rec.Id})
	if err != nil {
		t.Fatal(err)
	}
	return reports
}

func setThreshold(t *testing.T, a *testapp.App, n int) {
	t.Helper()
	a.As(testapp.Maintainer).Post(api.InstancePath, api.InstanceUpdate{ReportThreshold: &n}).Expect(http.StatusOK)
}

// TestReportConstants keeps the codes of the API in step with the values the collections accept.
func TestReportConstants(t *testing.T) {
	t.Parallel()
	reasons := []string{api.ReportReasonAdvertising, api.ReportReasonAbuse, api.ReportReasonPoliticalIllegal,
		api.ReportReasonUnverifiedClaim, api.ReportReasonFalseOrDuplicate, api.ReportReasonOther}
	if !slices.Equal(reasons, store.ReportReasons) {
		t.Errorf("reasons %v, store %v", reasons, store.ReportReasons)
	}
	resolutions := []string{api.ResolutionRestored, api.ResolutionKeptHidden, api.ResolutionDeleted}
	if !slices.Equal(resolutions, store.Resolutions) {
		t.Errorf("resolutions %v, store %v", resolutions, store.Resolutions)
	}
	if api.MaxReportNoteLength != store.MaxReportNoteLength {
		t.Error("MaxReportNoteLength differs from the store's")
	}
}

// TestReportAutoHide: members who see a recommendation report it once each; when the open reports reach the
// threshold it is hidden, its author gets a notice and the audit log an entry; the author cannot report it.
func TestReportAutoHide(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	rec := a.Recommendation
	reporters := []*core.Record{a.Member, addMember(t, a, "Second", a.Maintainer, auth.RoleMember),
		addMember(t, a, "Third", a.Maintainer, auth.RoleMember)}

	report(a.As(testapp.Anonymous), rec, api.ReportReasonAbuse, "").Expect(http.StatusUnauthorized)
	expectCode(t, report(a.As(testapp.Author), rec, api.ReportReasonAbuse, "").Expect(http.StatusBadRequest),
		api.CodeOwnRecommendation)
	// Invalid requests.
	for _, req := range []api.ReportRequest{{Reason: "spam"}, {Reason: api.ReportReasonOther},
		{Reason: api.ReportReasonOther, Note: "   "}, {Reason: api.ReportReasonAbuse, Note: strings.Repeat("x", 501)}, {}} {
		expectCode(t, a.As(testapp.Member).Post(recPath(api.ReportPath, rec), req).Expect(http.StatusBadRequest),
			api.CodeInvalidRequest)
	}
	// Another member's private recommendation does not exist for them.
	expectCode(t, report(a.As(testapp.Member), a.PrivateRecommendation, api.ReportReasonAbuse, "").
		Expect(http.StatusNotFound), api.CodeNotFound)
	reportDeleted(t, a)

	for i, reporter := range reporters {
		c := a.SignedIn(reporter)
		r := report(c, rec, api.ReportReasonOther, fmt.Sprintf("Test note %d", i)).Expect(http.StatusCreated)
		if got := testapp.Decode[api.ReportReceipt](r); got.Recommendation != uuidOf(rec) {
			t.Errorf("receipt %+v", got)
		}
		shown := recOnCard(t, a, a.As(testapp.Member), rec) != nil
		if want := i < 2; shown != want {
			t.Fatalf("after %d reports: shown to members %v, want %v", i+1, shown, want)
		}
		if shown {
			expectCode(t, report(c, rec, api.ReportReasonAbuse, "").Expect(http.StatusConflict), api.CodeAlreadyReported)
		} else {
			// Hidden now: the last reporter no longer sees it.
			report(c, rec, api.ReportReasonAbuse, "").Expect(http.StatusNotFound)
		}
	}

	rec = reload(t, a, rec)
	if !rec.GetBool("hidden") || rec.GetString("hidden_kind") != store.HiddenByReports || rec.GetString("hidden_by") != "" ||
		rec.GetDateTime("hidden_at").IsZero() {
		t.Errorf("hidden %v, kind %q, by %q", rec.GetBool("hidden"), rec.GetString("hidden_kind"), rec.GetString("hidden_by"))
	}
	// Its only recommendation hidden, the place is gone for members; its author and maintainers see it, not counted.
	if _, ok := cardOf(t, a.As(testapp.Member), a.Place); ok {
		t.Error("members still see the place")
	}
	for _, id := range []testapp.Identity{testapp.Author, testapp.Maintainer} {
		card, ok := cardOf(t, a.As(id), a.Place)
		if !ok || len(card.Recommendations) != 1 || card.Recommendations[0].Hidden == nil ||
			card.Recommendations[0].Hidden.Kind != store.HiddenByReports || card.Recommendations[0].Hidden.Reason != "" {
			t.Fatalf("%s: card %+v", id, card)
		}
		if card.Counts != (api.PlaceCounts{}) {
			t.Errorf("%s: counts %+v", id, card.Counts)
		}
	}
	if got := noticeKinds(t, a, a.Author, rec); !slices.Equal(got, []string{domain.NoticeHiddenByReports}) {
		t.Errorf("notices %v", got)
	}
	entries := audits(t, a, api.AuditAutoHide, uuidOf(rec))
	if len(entries) != 1 || entries[0].GetString("actor") != "" {
		t.Fatalf("audit %v", entries)
	}

	// The maintainer sees the hidden recommendation and may report it too; it is hidden once.
	report(a.As(testapp.Maintainer), rec, api.ReportReasonAbuse, "").Expect(http.StatusCreated)
	if n := len(audits(t, a, api.AuditAutoHide, uuidOf(rec))); n != 1 {
		t.Errorf("%d auto_hide entries", n)
	}
	if n := len(reportsOf(t, a, rec)); n != 4 {
		t.Errorf("%d reports", n)
	}
	// Members no longer see it: a fourth member gets 404.
	report(a.SignedIn(addMember(t, a, "Fourth", a.Maintainer, auth.RoleMember)), rec, api.ReportReasonAbuse, "").
		Expect(http.StatusNotFound)
	a.CheckNoInternalIDs()
}

// reportDeleted checks that a deleted recommendation cannot be reported.
func reportDeleted(t *testing.T, a *testapp.App) {
	t.Helper()
	place := a.SeedPlace(testapp.PlaceSeed{Name: "Test Gone Grill", Lat: 0.33, Lon: 7.03})
	rec := a.SeedRecommendation(testapp.RecommendationSeed{Place: place, Author: a.Author, Deleted: true})
	report(a.As(testapp.Member), rec, api.ReportReasonAbuse, "").Expect(http.StatusNotFound)
}

// TestReportThreshold: the instance's threshold applies (2 works), and resolved reports no longer count.
func TestReportThreshold(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	setThreshold(t, a, 2)
	second := addMember(t, a, "Second", a.Maintainer, auth.RoleMember)
	third := addMember(t, a, "Third", a.Maintainer, auth.RoleMember)

	rec := a.PseudonymRecommendation
	report(a.As(testapp.Member), rec, api.ReportReasonAdvertising, "").Expect(http.StatusCreated)
	if store.IsHidden(reload(t, a, rec)) {
		t.Fatal("hidden after one report")
	}
	report(a.SignedIn(second), rec, api.ReportReasonAdvertising, "").Expect(http.StatusCreated)
	if got := reload(t, a, rec).GetString("hidden_kind"); got != store.HiddenByReports {
		t.Fatalf("after two reports: kind %q", got)
	}

	// One report, dismissed by a maintainer (restored), then another: only one is open, below the threshold.
	other := a.Recommendation
	report(a.As(testapp.Member), other, api.ReportReasonAbuse, "").Expect(http.StatusCreated)
	res := testapp.Decode[api.ResolveResult](a.As(testapp.Maintainer).Post(recPath(api.ResolvePath, other),
		api.ResolveRequest{Resolution: api.ResolutionRestored}).Expect(http.StatusOK))
	if res.Resolved != 1 || res.Recommendation == nil || res.Recommendation.Hidden != nil {
		t.Fatalf("resolve %+v", res)
	}
	report(a.SignedIn(third), other, api.ReportReasonAbuse, "").Expect(http.StatusCreated)
	if store.IsHidden(reload(t, a, other)) {
		t.Error("a resolved report counted towards the threshold")
	}
	// The member whose report was resolved cannot report it again.
	expectCode(t, report(a.As(testapp.Member), other, api.ReportReasonAbuse, "").Expect(http.StatusConflict),
		api.CodeAlreadyReported)
	// Nothing to notify about the dismissed reports: the author was never told it was hidden.
	if got := noticeKinds(t, a, a.Author, other); len(got) != 0 {
		t.Errorf("notices %v", got)
	}
}

// TestHideUnhide: maintainers hide with a reason and unhide; the author is told, the audit log records both, and
// unhiding resolves the open reports as restored.
func TestHideUnhide(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	rec := a.Recommendation
	refused(t, a, func(c *testapp.Client) *testapp.Response {
		return c.Post(recPath(api.HidePath, rec), api.HideRequest{Reason: "Test reason"})
	})
	refused(t, a, func(c *testapp.Client) *testapp.Response { return c.Post(recPath(api.UnhidePath, rec), nil) })
	m := a.As(testapp.Maintainer)
	for _, reason := range []string{"", "  ", strings.Repeat("x", 501)} {
		m.Post(recPath(api.HidePath, rec), api.HideRequest{Reason: reason}).Expect(http.StatusBadRequest)
	}
	expectCode(t, m.Post(recPath(api.UnhidePath, rec), nil).Expect(http.StatusConflict), api.CodeNotHidden)
	// The author's private recommendation does not exist for maintainers.
	m.Post(recPath(api.HidePath, a.PrivateRecommendation), api.HideRequest{Reason: "Test reason"}).
		Expect(http.StatusNotFound)

	report(a.As(testapp.Member), rec, api.ReportReasonAbuse, "Test note").Expect(http.StatusCreated)
	view := testapp.Decode[api.RecommendationView](m.Post(recPath(api.HidePath, rec),
		api.HideRequest{Reason: " Test hide reason "}).Expect(http.StatusOK))
	if view.Hidden == nil || view.Hidden.Kind != store.HiddenByMaintainer || view.Hidden.Reason != "Test hide reason" {
		t.Fatalf("view %+v", view)
	}
	if r := reload(t, a, rec); r.GetString("hidden_by") != a.Maintainer.Id || r.GetString("hidden_reason") != "Test hide reason" {
		t.Errorf("by %q, reason %q", r.GetString("hidden_by"), r.GetString("hidden_reason"))
	}
	expectCode(t, m.Post(recPath(api.HidePath, rec), api.HideRequest{Reason: "Test reason"}).Expect(http.StatusConflict),
		api.CodeAlreadyHidden)
	if recOnCard(t, a, a.As(testapp.Member), rec) != nil {
		t.Error("members still see the hidden recommendation")
	}
	if v := recOnCard(t, a, a.As(testapp.Author), rec); v == nil || v.Hidden == nil || v.Hidden.Reason != "Test hide reason" {
		t.Errorf("the author sees %+v", v)
	}
	if e := audits(t, a, api.AuditHide, uuidOf(rec)); len(e) != 1 || e[0].GetString("actor") != a.Maintainer.Id {
		t.Errorf("hide audit %v", e)
	}
	// The open report is still open while hidden by the maintainer.
	if r := reportsOf(t, a, rec); len(r) != 1 || r[0].GetString("resolution") != "" {
		t.Fatalf("reports %v", r)
	}

	view = testapp.Decode[api.RecommendationView](m.Post(recPath(api.UnhidePath, rec), nil).Expect(http.StatusOK))
	if view.Hidden != nil {
		t.Errorf("still hidden: %+v", view)
	}
	r := reload(t, a, rec)
	if store.IsHidden(r) || r.GetString("hidden_reason") != "" || r.GetString("hidden_by") != "" {
		t.Errorf("hidden %v, reason %q", store.IsHidden(r), r.GetString("hidden_reason"))
	}
	if rep := reportsOf(t, a, rec)[0]; rep.GetString("resolution") != store.ResolutionRestored ||
		rep.GetString("resolved_by") != a.Maintainer.Id || rep.GetDateTime("resolved_at").IsZero() {
		t.Errorf("report %v", rep)
	}
	if recOnCard(t, a, a.As(testapp.Member), rec) == nil {
		t.Error("members do not see the recommendation again")
	}
	if got := noticeKinds(t, a, a.Author, rec); !slices.Equal(got,
		[]string{domain.NoticeHiddenByMaintainer, domain.NoticeRestored}) {
		t.Errorf("notices %v", got)
	}
	if e := audits(t, a, api.AuditUnhide, uuidOf(rec)); len(e) != 1 {
		t.Errorf("unhide audit %v", e)
	}
	a.CheckNoInternalIDs()
}

// TestReviewQueue: maintainers see the recommendations with open reports and the hidden ones, with their place, the
// number of open reports per reason and the notes; never who reported.
func TestReviewQueue(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	refused(t, a, func(c *testapp.Client) *testapp.Response { return c.Get(api.ReviewQueuePath) })
	m := a.As(testapp.Maintainer)
	if q := testapp.Decode[api.ReviewQueue](m.Get(api.ReviewQueuePath).Expect(http.StatusOK)); len(q.Items) != 0 {
		t.Fatalf("queue %+v", q)
	}

	reporter := addMember(t, a, "Reporter Uno", a.Maintainer, auth.RoleMember)
	report(a.As(testapp.Member), a.Recommendation, api.ReportReasonAbuse, "Test first note").Expect(http.StatusCreated)
	report(a.SignedIn(reporter), a.Recommendation, api.ReportReasonAbuse, "").Expect(http.StatusCreated)
	report(a.As(testapp.Maintainer), a.Recommendation, api.ReportReasonUnverifiedClaim, "Test third note").
		Expect(http.StatusCreated)
	m.Post(recPath(api.HidePath, a.PseudonymRecommendation), api.HideRequest{Reason: "Test reason"}).Expect(http.StatusOK)

	r := m.Get(api.ReviewQueuePath).Expect(http.StatusOK)
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", r.Header().Get("Cache-Control"))
	}
	for _, secret := range []string{uuidOf(a.Member), uuidOf(reporter), "test_member", "Test Member", "test_reporter_uno",
		"Reporter Uno", uuidOf(a.Maintainer), "Test Maintainer", "reporter", uuidOf(a.Author), "test_author"} {
		if strings.Contains(r.Body.String(), secret) {
			t.Errorf("the queue contains %q", secret)
		}
	}
	q := testapp.Decode[api.ReviewQueue](r)
	if len(q.Items) != 2 {
		t.Fatalf("queue %+v", q)
	}
	first, second := q.Items[0], q.Items[1]
	if first.Recommendation.UUID != uuidOf(a.Recommendation) || first.Place.UUID != uuidOf(a.Place) ||
		first.Place.Name != "Test Dumpling House" || first.OpenReports != 3 || first.Recommendation.Hidden == nil ||
		first.Recommendation.Hidden.Kind != store.HiddenByReports || first.LastReportedAt == nil {
		t.Errorf("first %+v", first)
	}
	if want := []api.ReasonCount{{Reason: api.ReportReasonAbuse, Count: 2}, {Reason: api.ReportReasonUnverifiedClaim,
		Count: 1}}; !slices.Equal(first.Reasons, want) {
		t.Errorf("reasons %+v", first.Reasons)
	}
	if len(first.Notes) != 2 || first.Notes[0].Note != "Test third note" || first.Notes[1].Note != "Test first note" ||
		first.Notes[1].Reason != api.ReportReasonAbuse {
		t.Errorf("notes %+v", first.Notes)
	}
	if second.Recommendation.UUID != uuidOf(a.PseudonymRecommendation) || second.OpenReports != 0 ||
		second.LastReportedAt != nil || second.Recommendation.Hidden == nil ||
		second.Recommendation.Hidden.Kind != store.HiddenByMaintainer || second.Recommendation.DisplayName != "Test Pen Name" ||
		len(second.Recommendation.ExternalLinks) != 0 || len(second.Reasons) != 0 || len(second.Notes) != 0 {
		t.Errorf("second %+v", second)
	}
	a.CheckNoInternalIDs()
}

// TestResolve: restoring, keeping hidden and the violation delete, which erases the recommendation.
func TestResolve(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	m := a.As(testapp.Maintainer)
	resolve := func(rec *core.Record, resolution, reason string) *testapp.Response {
		return m.Post(recPath(api.ResolvePath, rec), api.ResolveRequest{Resolution: resolution, Reason: reason})
	}
	refused(t, a, func(c *testapp.Client) *testapp.Response {
		return c.Post(recPath(api.ResolvePath, a.Recommendation), api.ResolveRequest{Resolution: api.ResolutionRestored})
	})
	expectCode(t, resolve(a.Recommendation, api.ResolutionRestored, "").Expect(http.StatusConflict), api.CodeNotUnderReview)
	resolve(a.Recommendation, "forgotten", "").Expect(http.StatusBadRequest)

	// Restored while shown: the reports are dismissed, nothing else changes.
	report(a.As(testapp.Member), a.Recommendation, api.ReportReasonAbuse, "").Expect(http.StatusCreated)
	expectCode(t, resolve(a.Recommendation, api.ResolutionKeptHidden, "").Expect(http.StatusConflict), api.CodeNotHidden)
	res := testapp.Decode[api.ResolveResult](resolve(a.Recommendation, api.ResolutionRestored, "Test fine").
		Expect(http.StatusOK))
	if res.Resolved != 1 || res.Resolution != api.ResolutionRestored || res.Recommendation == nil {
		t.Errorf("result %+v", res)
	}
	if e := audits(t, a, api.AuditResolve, uuidOf(a.Recommendation)); len(e) != 1 {
		t.Errorf("audit %v", e)
	}
	expectCode(t, resolve(a.Recommendation, api.ResolutionRestored, "").Expect(http.StatusConflict), api.CodeNotUnderReview)

	// Kept hidden: the reports are resolved, it stays hidden.
	hidden := a.PseudonymRecommendation
	m.Post(recPath(api.HidePath, hidden), api.HideRequest{Reason: "Test reason"}).Expect(http.StatusOK)
	report(m, hidden, api.ReportReasonAbuse, "").Expect(http.StatusCreated)
	res = testapp.Decode[api.ResolveResult](resolve(hidden, api.ResolutionKeptHidden, "").Expect(http.StatusOK))
	if res.Resolved != 1 || res.Recommendation == nil || res.Recommendation.Hidden == nil {
		t.Errorf("result %+v", res)
	}
	if rep := reportsOf(t, a, hidden)[0]; rep.GetString("resolution") != store.ResolutionKeptHidden {
		t.Errorf("resolution %q", rep.GetString("resolution"))
	}
	if !store.IsHidden(reload(t, a, hidden)) {
		t.Error("no longer hidden")
	}
	// Restored when hidden: shown again, the author is told.
	res = testapp.Decode[api.ResolveResult](resolve(hidden, api.ResolutionRestored, "").Expect(http.StatusOK))
	if res.Resolved != 0 || res.Recommendation == nil || res.Recommendation.Hidden != nil {
		t.Errorf("result %+v", res)
	}
	if got := noticeKinds(t, a, a.Author, hidden); !slices.Equal(got,
		[]string{domain.NoticeHiddenByMaintainer, domain.NoticeRestored}) {
		t.Errorf("notices %v", got)
	}

	// The violation delete: a reason is required; the recommendation is erased, its revisions removed.
	place := a.SeedPlace(testapp.PlaceSeed{Name: "Test Violation Diner", Lat: 0.336, Lon: 7.07})
	bad := a.SeedRecommendation(testapp.RecommendationSeed{Place: place, Author: a.Author, Text: "Test violating text",
		Dishes: []string{"Test dish"}, Season: "Test season", VisitedOn: "2026-03-01",
		Sources: []map[string]any{{"url": "https://example.test/source", "kind": "chat", "excerpt": "Test excerpt"}}})
	c, err := a.FindCachedCollectionByNameOrId(store.RecommendationRevisions)
	if err != nil {
		t.Fatal(err)
	}
	rev := core.NewRecord(c)
	rev.Set("recommendation", bad.Id)
	rev.Set("editor_role", "author")
	rev.Set("editor", a.Author.Id)
	rev.Set("snapshot", store.SnapshotOf(bad))
	if err := a.Save(rev); err != nil {
		t.Fatal(err)
	}
	report(a.As(testapp.Member), bad, api.ReportReasonAbuse, "").Expect(http.StatusCreated)
	for _, reason := range []string{"", " "} {
		resolve(bad, api.ResolutionDeleted, reason).Expect(http.StatusBadRequest)
	}
	res = testapp.Decode[api.ResolveResult](resolve(bad, api.ResolutionDeleted, "Test violation reason").
		Expect(http.StatusOK))
	if res.Recommendation != nil || res.Resolved != 1 || res.Resolution != api.ResolutionDeleted {
		t.Errorf("result %+v", res)
	}
	tomb, err := store.Query(a, store.Recommendations).IncludeDeleted().
		Where(dbx.HashExp{store.Recommendations + ".id": bad.Id}).One()
	if err != nil {
		t.Fatal(err)
	}
	if !store.IsDeleted(tomb) || tomb.GetString("text") != "" || tomb.GetString("season") != "" ||
		!tomb.GetDateTime("visited_on").IsZero() || len(store.SnapshotOf(tomb).Dishes) != 0 ||
		len(store.SnapshotOf(tomb).Sources) != 0 {
		t.Errorf("tombstone %v", tomb)
	}
	if n, _ := store.Query(a, store.RecommendationRevisions).Where(dbx.HashExp{"recommendation": bad.Id}).Count(); n != 0 {
		t.Errorf("%d revisions left", n)
	}
	if rep := reportsOf(t, a, bad)[0]; rep.GetString("resolution") != store.ResolutionDeleted {
		t.Errorf("resolution %q", rep.GetString("resolution"))
	}
	e := audits(t, a, api.AuditViolationDelete, uuidOf(bad))
	if len(e) != 1 || e[0].GetString("actor") != a.Maintainer.Id || !strings.Contains(e[0].GetString("details"),
		"Test violation reason") {
		t.Fatalf("audit %v", e)
	}
	if got := noticeKinds(t, a, a.Author, bad); !slices.Equal(got, []string{domain.NoticeViolationDeleted}) {
		t.Errorf("notices %v", got)
	}
	for _, id := range []testapp.Identity{testapp.Member, testapp.Author, testapp.Maintainer} {
		if _, ok := cardOf(t, a.As(id), place); ok {
			t.Errorf("%s still sees the place", id)
		}
	}
	resolve(bad, api.ResolutionDeleted, "Test reason").Expect(http.StatusNotFound)
	// Nobody else deletes it: the card has no other route, and the author's own deleting is T031's.
	a.CheckNoInternalIDs()
}

// TestRestoreRemovedAuthor: a member removed with their recommendations hidden had some hidden before by a maintainer
// and by reports. Restoring those does not show them against the removal's choice: they stay hidden as
// member_removed. Unhiding a member_removed recommendation is a maintainer's explicit choice and shows it. Removed with
// their recommendations kept, restoring shows them.
func TestRestoreRemovedAuthor(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	m := a.As(testapp.Maintainer)
	removed := addMember(t, a, "Removed", a.Maintainer, auth.RoleMember)
	places := make([]*core.Record, 4)
	for i := range places {
		places[i] = a.SeedPlace(testapp.PlaceSeed{Name: fmt.Sprintf("Test Removed Place %d", i), Lat: 0.32 + float64(i)/1000,
			Lon: 7.04})
	}
	byReports := a.SeedRecommendation(testapp.RecommendationSeed{Place: places[0], Author: removed,
		HiddenKind: store.HiddenByReports})
	byMaintainer := a.SeedRecommendation(testapp.RecommendationSeed{Place: places[1], Author: removed,
		HiddenKind: store.HiddenByMaintainer, HiddenReason: "Test earlier reason", HiddenBy: a.Maintainer})
	shown := a.SeedRecommendation(testapp.RecommendationSeed{Place: places[2], Author: removed})
	report(a.As(testapp.Member), shown, api.ReportReasonAbuse, "").Expect(http.StatusCreated)

	m.Post(recPath(api.MemberRemovePath, removed), api.RemoveMemberRequest{Recommendations: api.RecommendationsHide,
		Reason: "Test removal reason"}).Expect(http.StatusOK)
	if got := reload(t, a, byReports).GetString("hidden_kind"); got != store.HiddenByReports {
		t.Fatalf("kind after the removal %q", got)
	}

	// Resolving the reports of the removed author's recommendation as restored: still hidden, as member_removed.
	res := testapp.Decode[api.ResolveResult](m.Post(recPath(api.ResolvePath, byReports),
		api.ResolveRequest{Resolution: api.ResolutionRestored}).Expect(http.StatusOK))
	if res.Recommendation == nil || res.Recommendation.Hidden == nil ||
		res.Recommendation.Hidden.Kind != store.HiddenMemberRemoved || res.Recommendation.Hidden.Reason != "Test removal reason" {
		t.Fatalf("result %+v", res.Recommendation)
	}
	// Unhiding the one a maintainer hid: also still hidden, as member_removed; nobody is told it is shown.
	view := testapp.Decode[api.RecommendationView](m.Post(recPath(api.UnhidePath, byMaintainer), nil).Expect(http.StatusOK))
	if view.Hidden == nil || view.Hidden.Kind != store.HiddenMemberRemoved {
		t.Fatalf("view %+v", view)
	}
	if e := audits(t, a, api.AuditUnhide, uuidOf(byMaintainer)); len(e) != 1 || !strings.Contains(e[0].GetString("details"),
		`"shown":false`) {
		t.Errorf("audit %v", e)
	}
	// Resolving the reports of a member_removed recommendation as restored resolves them and leaves it hidden.
	res = testapp.Decode[api.ResolveResult](m.Post(recPath(api.ResolvePath, shown),
		api.ResolveRequest{Resolution: api.ResolutionRestored}).Expect(http.StatusOK))
	if res.Resolved != 1 || res.Recommendation == nil || res.Recommendation.Hidden == nil ||
		res.Recommendation.Hidden.Kind != store.HiddenMemberRemoved {
		t.Fatalf("result %+v", res)
	}
	for _, rec := range []*core.Record{byReports, byMaintainer, shown} {
		if recOnCard(t, a, a.As(testapp.Member), rec) != nil {
			t.Errorf("members see %s", uuidOf(rec))
		}
		if got := noticeKinds(t, a, removed, rec); slices.Contains(got, domain.NoticeRestored) {
			t.Errorf("notices %v", got)
		}
	}

	// An explicit unhide of a member_removed recommendation shows it.
	for _, rec := range []*core.Record{shown, byReports} {
		view = testapp.Decode[api.RecommendationView](m.Post(recPath(api.UnhidePath, rec), nil).Expect(http.StatusOK))
		if view.Hidden != nil || recOnCard(t, a, a.As(testapp.Member), rec) == nil {
			t.Errorf("%s: %+v", uuidOf(rec), view)
		}
	}

	// Removed with the recommendations kept: restoring shows them.
	kept := addMember(t, a, "Kept", a.Maintainer, auth.RoleMember)
	keptRec := a.SeedRecommendation(testapp.RecommendationSeed{Place: places[3], Author: kept,
		HiddenKind: store.HiddenByMaintainer, HiddenReason: "Test reason", HiddenBy: a.Maintainer})
	m.Post(recPath(api.MemberRemovePath, kept), api.RemoveMemberRequest{Recommendations: api.RecommendationsKeep,
		Reason: "Test removal reason"}).Expect(http.StatusOK)
	view = testapp.Decode[api.RecommendationView](m.Post(recPath(api.UnhidePath, keptRec), nil).Expect(http.StatusOK))
	if view.Hidden != nil || recOnCard(t, a, a.As(testapp.Member), keptRec) == nil {
		t.Errorf("kept: %+v", view)
	}
}

// TestRevealAuthor: maintainers look up the member behind a recommendation under review, with a fresh proof; every
// reveal is audited (M2-Q7).
func TestRevealAuthor(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	rec := a.PseudonymRecommendation
	reveal := func(c *testapp.Client, r *core.Record) *testapp.Response {
		return c.Post(recPath(api.RevealAuthorPath, r), nil)
	}
	refused(t, a, func(c *testapp.Client) *testapp.Response { return reveal(c, rec) })
	m := a.As(testapp.Maintainer)
	expectCode(t, reveal(m, rec).Expect(http.StatusConflict), api.CodeNotUnderReview)
	report(a.As(testapp.Member), rec, api.ReportReasonAbuse, "").Expect(http.StatusCreated)

	makeStale(t, a, a.Maintainer)
	expectCode(t, reveal(m, rec).Expect(http.StatusForbidden), auth.CodeReauthRequired)
	if n := len(audits(t, a, api.AuditRevealPseudonym, uuidOf(a.Author))); n != 0 {
		t.Errorf("%d audit entries without a reveal", n)
	}
	fresh := a.As(testapp.Maintainer)
	for i := 1; i <= 2; i++ {
		got := testapp.Decode[api.RevealedAuthor](reveal(fresh, rec).Expect(http.StatusOK))
		if got != (api.RevealedAuthor{UUID: uuidOf(a.Author), DisplayName: "Test Author", InCircle: true}) {
			t.Errorf("revealed %+v", got)
		}
		e := audits(t, a, api.AuditRevealPseudonym, uuidOf(a.Author))
		if len(e) != i || e[0].GetString("actor") != a.Maintainer.Id || !strings.Contains(e[0].GetString("details"), uuidOf(rec)) {
			t.Errorf("audit after %d reveals: %v", i, e)
		}
	}
	reveal(fresh, a.PrivateRecommendation).Expect(http.StatusNotFound)
	a.CheckNoInternalIDs()
}

// TestReportBudget: every member has a budget of reports of their own.
func TestReportBudget(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	c := a.As(testapp.Member)
	for i := range 11 {
		place := a.SeedPlace(testapp.PlaceSeed{Name: fmt.Sprintf("Test Budget Place %d", i), Lat: 0.35, Lon: 7.03 + float64(i)/1000})
		rec := a.SeedRecommendation(testapp.RecommendationSeed{Place: place, Author: a.Author})
		r := report(c, rec, api.ReportReasonAbuse, "")
		if i < 10 {
			r.Expect(http.StatusCreated)
			continue
		}
		expectCode(t, r.Expect(http.StatusTooManyRequests), api.CodeRateLimited)
		if r.Header().Get("Retry-After") == "" {
			t.Error("no Retry-After")
		}
		// Another member's budget is their own.
		report(a.As(testapp.Maintainer), rec, api.ReportReasonAbuse, "").Expect(http.StatusCreated)
	}
}

// TestNoReporterInResponses: after reports that hide a recommendation and a review, no response to any identity —
// cards, map, signatures, notices, both exports, the personal export, the review queue and the maintainer's actions —
// contains a reporter's uuid, login name or name. Hidden recommendations are in no member's card, counts, map or
// layer export.
func TestNoReporterInResponses(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	reporters := []*core.Record{addMember(t, a, "Reporter Alpha", a.Maintainer, auth.RoleMember),
		addMember(t, a, "Reporter Beta", a.Maintainer, auth.RoleMember),
		addMember(t, a, "Reporter Gamma", a.Maintainer, auth.RoleMember)}
	// Each response is checked for the identifiers of the reporters who are not its caller.
	type check struct {
		r      *testapp.Response
		except *core.Record
	}
	var checks []check
	keep := func(r *testapp.Response, caller *core.Record) { checks = append(checks, check{r, caller}) }
	for _, rec := range []*core.Record{a.Recommendation, a.PseudonymRecommendation} {
		for _, reporter := range reporters {
			keep(report(a.SignedIn(reporter), rec, api.ReportReasonUnverifiedClaim, "Test note").
				Expect(http.StatusCreated), reporter)
		}
	}
	m := a.As(testapp.Maintainer)
	keep(m.Get(api.ReviewQueuePath).Expect(http.StatusOK), nil)
	keep(m.Post(recPath(api.RevealAuthorPath, a.PseudonymRecommendation), nil).Expect(http.StatusOK), nil)
	keep(m.Post(recPath(api.ResolvePath, a.PseudonymRecommendation),
		api.ResolveRequest{Resolution: api.ResolutionKeptHidden}).Expect(http.StatusOK), nil)

	city := "?city=" + testapp.CitySlug
	paths := []string{api.MapPlacesPath + city, api.SignaturesPath + city, api.NoticesPath, api.ExportPlacesPath,
		api.ExportRecommendationsPath, api.PersonalExportPath, api.ReviewQueuePath, recPath(api.PlaceCardPath, a.Place),
		recPath(api.PlaceCardPath, a.PseudonymPlace)}
	for _, id := range testapp.Identities {
		c := a.As(id)
		for _, path := range paths {
			keep(c.Get(path), nil)
		}
	}
	// A reporter's view: the other reporters are not in it.
	for _, path := range paths {
		keep(a.SignedIn(reporters[0]).Get(path), reporters[0])
	}

	for _, c := range checks {
		body := c.r.Body.String()
		for _, reporter := range reporters {
			if reporter == c.except {
				continue
			}
			for _, s := range []string{uuidOf(reporter), reporter.GetString("display_name"), reporter.GetString("login_name")} {
				if strings.Contains(body, s) {
					t.Errorf("%s %s (%d) contains %q", c.r.Method, c.r.Path, c.r.Code, s)
				}
			}
		}
		// (The reporters' own login names have the word: only the others' responses are checked for it.)
		if c.except == nil && strings.Contains(strings.ToLower(body), "reporter") {
			t.Errorf("%s %s mentions reporters", c.r.Method, c.r.Path)
		}
	}

	// Both recommendations are hidden: no member sees them anywhere.
	for _, rec := range []*core.Record{a.Recommendation, a.PseudonymRecommendation} {
		if !store.IsHidden(reload(t, a, rec)) {
			t.Fatalf("%s is not hidden", uuidOf(rec))
		}
	}
	member := a.As(testapp.Member)
	for _, path := range []string{api.MapPlacesPath + city, api.ExportPlacesPath, api.ExportRecommendationsPath} {
		body := member.Get(path).Expect(http.StatusOK).Body.String()
		for _, s := range []string{uuidOf(a.Recommendation), uuidOf(a.PseudonymRecommendation), uuidOf(a.Place),
			uuidOf(a.PseudonymPlace), "Test recommendation text", "Test pen name text"} {
			if strings.Contains(body, s) {
				t.Errorf("%s contains %q", path, s)
			}
		}
	}
	for _, id := range []testapp.Identity{testapp.Member, testapp.Maintainer} {
		for _, path := range []string{api.ExportPlacesPath, api.ExportRecommendationsPath} {
			if body := a.As(id).Get(path).Body.String(); strings.Contains(body, "Test pen name text") {
				t.Errorf("%s: %s contains the hidden recommendation", id, path)
			}
		}
	}
	a.CheckNoInternalIDs()
}
