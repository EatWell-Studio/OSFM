package main

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// TestNoticeKinds: the kinds the API declares for the client are the kinds domain.Notify writes.
func TestNoticeKinds(t *testing.T) {
	declared := []string{api.NoticeKindMaintainerEdited, api.NoticeKindHiddenByMaintainer,
		api.NoticeKindHiddenByReports, api.NoticeKindRestored, api.NoticeKindViolationDeleted}
	if !slices.Equal(declared, domain.NoticeKinds) {
		t.Errorf("api %v, domain %v", declared, domain.NoticeKinds)
	}
}

// TestNotices checks GET /api/osfm/me/notices and POST /api/osfm/me/notices/read as the four identities, with notices
// seeded as T031 and T036 write them: each member reads and marks only their own notices (another member's uuid is
// 404 and changes nothing); newest first; the unread count goes down as notices are read; a notice names its
// recommendation, the place and the place's name while the member can see it, and nothing once it or its place is
// deleted; no notice says who did it.
func TestNotices(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	uuid := func(r *core.Record) string { return r.GetString(store.FieldUUID) }

	// The author's recommendations in every state a notice can find them in.
	hiddenPlace := a.SeedPlace(testapp.PlaceSeed{Name: "Test Tea Corner", Lat: 0.341, Lon: 7.061})
	hidden := a.SeedRecommendation(testapp.RecommendationSeed{Place: hiddenPlace, Author: a.Author,
		HiddenKind: store.HiddenByMaintainer, HiddenReason: "Test reason", HiddenBy: a.Maintainer})
	deleted := a.SeedRecommendation(testapp.RecommendationSeed{Place: hiddenPlace, Author: a.Maintainer,
		Deleted: true})
	gonePlace := a.SeedPlace(testapp.PlaceSeed{Name: "Test Gone Grill", Lat: 0.342, Lon: 7.062})
	ofGonePlace := a.SeedRecommendation(testapp.RecommendationSeed{Place: gonePlace, Author: a.Author,
		HiddenKind: store.HiddenByReports})
	if err := store.SoftDelete(a, gonePlace); err != nil {
		t.Fatal(err)
	}
	others := a.SeedRecommendation(testapp.RecommendationSeed{Place: hiddenPlace, Author: a.Member})

	// Oldest first.
	edited := a.SeedNotice(a.Author, store.NoticeMaintainerEdited, uuid(a.Recommendation))
	hid := a.SeedNotice(a.Author, store.NoticeHiddenByMaintainer, uuid(hidden))
	violation := a.SeedNotice(a.Author, store.NoticeViolationDeleted, uuid(deleted))
	reported := a.SeedNotice(a.Author, store.NoticeHiddenByReports, uuid(ofGonePlace))
	// A reference to a recommendation that is not the member's resolves to nothing either.
	stray := a.SeedNotice(a.Author, store.NoticeRestored, uuid(others))
	restored := a.SeedNotice(a.Author, store.NoticeRestored, uuid(a.PseudonymRecommendation))
	maintainers := a.SeedNotice(a.Maintainer, store.NoticeMaintainerEdited, uuid(a.PrivateRecommendation))

	type want struct {
		notice *core.Record
		kind   string
		target *api.NoticeTarget
	}
	authorWants := []want{
		{restored, api.NoticeKindRestored, &api.NoticeTarget{Recommendation: uuid(a.PseudonymRecommendation),
			Place: uuid(a.PseudonymPlace), PlaceName: "Test Noodle Stall"}},
		{stray, api.NoticeKindRestored, nil},
		{reported, api.NoticeKindHiddenByReports, nil},
		{violation, api.NoticeKindViolationDeleted, nil},
		{hid, api.NoticeKindHiddenByMaintainer, &api.NoticeTarget{Recommendation: uuid(hidden),
			Place: uuid(hiddenPlace), PlaceName: "Test Tea Corner"}},
		{edited, api.NoticeKindMaintainerEdited, &api.NoticeTarget{Recommendation: uuid(a.Recommendation),
			Place: uuid(a.Place), PlaceName: "Test Dumpling House"}},
	}

	list := func(id testapp.Identity) api.NoticeList {
		t.Helper()
		r := a.As(id).Get(api.NoticesPath).Expect(http.StatusOK)
		if cc := r.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: Cache-Control %q", id, cc)
		}
		// Nothing about who did it: not the maintainer, not another member.
		body := r.Body.String()
		for _, leak := range []string{uuid(a.Maintainer), "Test Maintainer", uuid(a.Member), "Test Member",
			"Test reason", "hidden_by\"", "reporter"} {
			if id != testapp.Maintainer && strings.Contains(body, leak) {
				t.Errorf("%s: notices contain %q: %s", id, leak, body)
			}
		}
		return testapp.Decode[api.NoticeList](r)
	}
	check := func(got api.NoticeList, wants []want, read map[string]bool) {
		t.Helper()
		unread := 0
		for _, w := range wants {
			if !read[uuid(w.notice)] {
				unread++
			}
		}
		if got.Unread != unread || len(got.Notices) != len(wants) {
			t.Fatalf("unread %d of %d notices, want %d of %d: %+v", got.Unread, len(got.Notices), unread,
				len(wants), got)
		}
		for i, w := range wants {
			n := got.Notices[i]
			if n.UUID != uuid(w.notice) || n.Kind != w.kind || n.Read != read[n.UUID] || n.CreatedAt.IsZero() {
				t.Errorf("notice %d: %+v, want %s %s read %v", i, n, uuid(w.notice), w.kind, read[uuid(w.notice)])
			}
			switch {
			case w.target == nil && n.Target != nil:
				t.Errorf("notice %d (%s): target %+v, want none", i, w.kind, *n.Target)
			case w.target != nil && (n.Target == nil || *n.Target != *w.target):
				t.Errorf("notice %d (%s): target %+v, want %+v", i, w.kind, n.Target, *w.target)
			}
		}
	}
	read := func(c *testapp.Client, body any, status int) *testapp.Response {
		t.Helper()
		return c.Post(api.NoticesReadPath, body).Expect(status)
	}

	// Anonymous: 401; without X-OSFM: 403.
	a.As(testapp.Anonymous).Get(api.NoticesPath).Expect(http.StatusUnauthorized)
	read(a.As(testapp.Anonymous), api.MarkNoticesRead{All: true}, http.StatusUnauthorized)
	req, err := http.NewRequest(http.MethodPost, api.NoticesReadPath, strings.NewReader(`{"all":true}`))
	if err != nil {
		t.Fatal(err)
	}
	a.As(testapp.Author).Send(req).Expect(http.StatusForbidden)

	// Each identity reads its own notices only, newest first.
	check(list(testapp.Author), authorWants, nil)
	check(list(testapp.Member), nil, nil)
	if got := list(testapp.Member); got.Notices == nil {
		t.Error("member: notices is null, want []")
	}
	// The maintainer's notice names the author's private recommendation: not the maintainer's, so no target.
	maintainerWants := []want{{maintainers, api.NoticeKindMaintainerEdited, nil}}
	check(list(testapp.Maintainer), maintainerWants, nil)

	// Another member's notice is 404, alone or with one's own, and nothing changes.
	for _, id := range []testapp.Identity{testapp.Member, testapp.Maintainer} {
		read(a.As(id), api.MarkNoticesRead{UUIDs: []string{uuid(edited)}}, http.StatusNotFound)
	}
	read(a.As(testapp.Maintainer), api.MarkNoticesRead{UUIDs: []string{uuid(maintainers), uuid(edited)}},
		http.StatusNotFound)
	read(a.As(testapp.Author), api.MarkNoticesRead{UUIDs: []string{uuid(edited), uuid(maintainers)}},
		http.StatusNotFound)
	read(a.As(testapp.Author), api.MarkNoticesRead{UUIDs: []string{"0190a1b2-0000-7000-8000-00000000ffff"}},
		http.StatusNotFound)
	check(list(testapp.Author), authorWants, nil)
	check(list(testapp.Maintainer), maintainerWants, nil)

	// Requests that are not valid.
	author := a.As(testapp.Author)
	many := make([]string, api.MaxNoticesPerRead+1)
	for i := range many {
		many[i] = fmt.Sprintf("0190a1b2-0000-7000-8000-%012x", i)
	}
	for _, body := range []any{map[string]any{}, api.MarkNoticesRead{}, map[string]any{"all": false},
		api.MarkNoticesRead{All: true, UUIDs: []string{uuid(edited)}}, map[string]any{"uuids": []string{"x"}},
		map[string]any{"uuids": []string{strings.ToUpper(uuid(edited))}}, map[string]any{"uuids": "x"},
		api.MarkNoticesRead{UUIDs: many}} {
		read(author, body, http.StatusBadRequest)
	}
	bad, err := http.NewRequest(http.MethodPost, api.NoticesReadPath, strings.NewReader(`{`))
	if err != nil {
		t.Fatal(err)
	}
	bad.Header.Set("X-OSFM", "1")
	author.Send(bad).Expect(http.StatusBadRequest)
	check(list(testapp.Author), authorWants, nil)

	// Marking read: the count goes down; marking again changes nothing; the answer is the list afterwards.
	got := testapp.Decode[api.NoticeList](read(author, api.MarkNoticesRead{UUIDs: []string{uuid(edited),
		uuid(violation), uuid(edited)}}, http.StatusOK))
	isRead := map[string]bool{uuid(edited): true, uuid(violation): true}
	check(got, authorWants, isRead)
	readAt := func(n *core.Record) string {
		t.Helper()
		r, err := a.FindRecordById(store.Notices, n.Id)
		if err != nil {
			t.Fatal(err)
		}
		return r.GetString("read_at")
	}
	first := readAt(edited)
	check(testapp.Decode[api.NoticeList](read(author, api.MarkNoticesRead{UUIDs: []string{uuid(edited)}},
		http.StatusOK)), authorWants, isRead)
	if first == "" || readAt(edited) != first {
		t.Errorf("read_at %q, then %q", first, readAt(edited))
	}
	check(list(testapp.Maintainer), maintainerWants, nil)

	// All: every notice of the caller, no one else's.
	for _, w := range authorWants {
		isRead[uuid(w.notice)] = true
	}
	check(testapp.Decode[api.NoticeList](read(author, api.MarkNoticesRead{All: true}, http.StatusOK)),
		authorWants, isRead)
	if readAt(edited) != first {
		t.Errorf("read_at changed by all: %q, was %q", readAt(edited), first)
	}
	check(list(testapp.Maintainer), maintainerWants, nil)
	check(testapp.Decode[api.NoticeList](read(a.As(testapp.Member), api.MarkNoticesRead{All: true},
		http.StatusOK)), nil, nil)

	// The list holds the newest MaxNoticesListed; the count covers all.
	for range api.MaxNoticesListed + 1 {
		a.SeedNotice(a.Member, store.NoticeRestored, uuid(others))
	}
	m := list(testapp.Member)
	if len(m.Notices) != api.MaxNoticesListed || m.Unread != api.MaxNoticesListed+1 {
		t.Errorf("member: %d notices, %d unread", len(m.Notices), m.Unread)
	}
	if m.Notices[0].Target == nil || m.Notices[0].Target.Place != uuid(hiddenPlace) {
		t.Errorf("member's own recommendation: %+v", m.Notices[0].Target)
	}
	a.CheckNoInternalIDs()
}
