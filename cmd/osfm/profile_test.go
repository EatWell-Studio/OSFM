package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// strptr returns a pointer to s, for the optional fields of api.ProfileUpdate.
func strptr(s string) *string { return &s }

// profileError decodes an error response of the profile route: its code and data.field.
func profileError(t *testing.T, r *testapp.Response) (code, field string) {
	t.Helper()
	e := testapp.Decode[api.Error](r)
	f, _ := e.Data["field"].(string)
	return e.Code, f
}

// TestProfile checks GET and POST /api/osfm/me/profile as the four identities: each member reads and changes only
// their own profile; names are checked, unique across all main names and pseudonyms whatever the letter case or width,
// and never the member's login name; a conflict does not say whose name or which kind it is; a pseudonym that signs
// recommendations cannot be cleared; changing it writes an audit entry and renames the recommendations.
func TestProfile(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})

	// Anonymous: 401; without X-OSFM: 403.
	a.As(testapp.Anonymous).Get(api.ProfilePath).Expect(http.StatusUnauthorized)
	a.As(testapp.Anonymous).Post(api.ProfilePath, api.ProfileUpdate{}).Expect(http.StatusUnauthorized)
	req, err := http.NewRequest(http.MethodPost, api.ProfilePath, strings.NewReader(`{"displayName":"Test X"}`))
	if err != nil {
		t.Fatal(err)
	}
	a.As(testapp.Member).Send(req).Expect(http.StatusForbidden)

	// Each signed-in identity reads their own profile, not cached.
	for id, want := range map[testapp.Identity]api.Profile{
		testapp.Member:     {DisplayName: "Test Member", ExternalLinks: []api.ExternalLink{}},
		testapp.Maintainer: {DisplayName: "Test Maintainer", ExternalLinks: []api.ExternalLink{}},
		testapp.Author: {DisplayName: "Test Author", Pseudonym: "Test Pen Name", PseudonymInUse: true,
			ExternalLinks: []api.ExternalLink{{URL: testapp.AuthorLink, Label: "Test Blog"}}},
	} {
		r := a.As(id).Get(api.ProfilePath).Expect(http.StatusOK)
		if r.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control %q", id, r.Header().Get("Cache-Control"))
		}
		got := testapp.Decode[api.Profile](r)
		if got.DisplayName != want.DisplayName || got.Pseudonym != want.Pseudonym ||
			got.PseudonymInUse != want.PseudonymInUse || got.DisplayNameIsLoginName ||
			!slices.Equal(got.ExternalLinks, want.ExternalLinks) {
			t.Errorf("%s: %+v, want %+v", id, got, want)
		}
	}

	member := a.As(testapp.Member)
	post := func(c *testapp.Client, u api.ProfileUpdate, status int) *testapp.Response {
		t.Helper()
		return c.Post(api.ProfilePath, u).Expect(status)
	}

	// Names that are not names.
	for _, bad := range []string{"", "   ", strings.Repeat("名", 65), "Test\u202eName", "Test\nName", "\u200b\u200b"} {
		for _, u := range []api.ProfileUpdate{{DisplayName: strptr(bad)}, {Pseudonym: strptr(bad + "x\x00")}} {
			code, field := profileError(t, post(member, u, http.StatusBadRequest))
			if code != api.CodeNameInvalid || field == "" {
				t.Errorf("%q: %s %s", bad, code, field)
			}
		}
	}
	// The login name, in any case or width, is neither a main name nor a pseudonym.
	for _, u := range []api.ProfileUpdate{{DisplayName: strptr(" TEST_member ")}, {Pseudonym: strptr("ｔｅｓｔ＿ｍｅｍｂｅｒ")}} {
		code, _ := profileError(t, post(member, u, http.StatusBadRequest))
		if code != api.CodeNameIsLoginName {
			t.Errorf("login name: %s", code)
		}
	}

	// Another member's main name and another member's pseudonym, in other letter cases and widths, are taken alike:
	// the answers are the same byte for byte, so they do not tell a main name from a pseudonym.
	var bodies []string
	for _, name := range []string{"test author", "ＴＥＳＴ　ＡＵＴＨＯＲ", "TEST PEN NAME", "Ｔｅｓｔ  Pen   Name"} {
		r := post(member, api.ProfileUpdate{DisplayName: strptr(name)}, http.StatusConflict)
		if code, field := profileError(t, r); code != api.CodeNameTaken || field != api.ProfileFieldDisplayName {
			t.Errorf("%q: %s %s", name, code, field)
		}
		bodies = append(bodies, r.Body.String())
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Errorf("conflict answers differ: %s / %s", bodies[0], b)
		}
	}
	for _, name := range []string{"Test Maintainer", "test pen name"} {
		r := post(member, api.ProfileUpdate{Pseudonym: strptr(name)}, http.StatusConflict)
		if code, field := profileError(t, r); code != api.CodeNameTaken || field != api.ProfileFieldPseudonym {
			t.Errorf("pseudonym %q: %s %s", name, code, field)
		}
	}
	// Nothing was saved by the refused requests.
	if m, _ := a.FindRecordById(store.Members, a.Member.Id); m.GetString("display_name") != "Test Member" ||
		m.GetString("pseudonym") != "" {
		t.Errorf("saved after refusals: %q %q", m.GetString("display_name"), m.GetString("pseudonym"))
	}

	// The member sets a pseudonym and links, and changes the main name: the response is the saved profile, /me has
	// the new main name, and the pseudonym change is audited for the maintainers.
	links := []api.ExternalLink{{URL: " https://example.test/test-member ", Label: " Test Page "},
		{URL: "https://example.test/test-other"}}
	got := testapp.Decode[api.Profile](post(member, api.ProfileUpdate{DisplayName: strptr(" Test Member Renamed "),
		Pseudonym: strptr("Test Member Pen"), ExternalLinks: &links}, http.StatusOK))
	wantLinks := []api.ExternalLink{{URL: "https://example.test/test-member", Label: "Test Page"},
		{URL: "https://example.test/test-other"}}
	if got.DisplayName != "Test Member Renamed" || got.Pseudonym != "Test Member Pen" || got.PseudonymInUse ||
		!slices.Equal(got.ExternalLinks, wantLinks) {
		t.Errorf("saved: %+v", got)
	}
	if me := testapp.Decode[auth.Me](member.Get(auth.MePath)); me.DisplayName != "Test Member Renamed" {
		t.Errorf("/me: %+v", me)
	}
	audits, err := a.FindAllRecords(store.AuditLog, dbx.HashExp{"action": api.AuditPseudonymChange})
	if err != nil || len(audits) != 1 {
		t.Fatalf("audit entries: %d, %v", len(audits), err)
	}
	var details map[string]any
	_ = audits[0].UnmarshalJSONField("details", &details)
	if audits[0].GetString("actor") != a.Member.Id || audits[0].GetString("target_uuid") !=
		a.Member.GetString(store.FieldUUID) || details["from"] != "" || details["to"] != "Test Member Pen" {
		t.Errorf("audit entry: %v %v", audits[0], details)
	}
	// A request without a field keeps it; the old main name is free again, the own pseudonym is not a main name.
	got = testapp.Decode[api.Profile](post(member, api.ProfileUpdate{ExternalLinks: &[]api.ExternalLink{}},
		http.StatusOK))
	if got.DisplayName != "Test Member Renamed" || got.Pseudonym != "Test Member Pen" || len(got.ExternalLinks) != 0 {
		t.Errorf("partial update: %+v", got)
	}
	if code, field := profileError(t, post(member, api.ProfileUpdate{DisplayName: strptr("TEST MEMBER PEN")},
		http.StatusConflict)); code != api.CodeNameTaken || field != api.ProfileFieldDisplayName {
		t.Errorf("own pseudonym as main name: %s %s", code, field)
	}
	post(a.As(testapp.Maintainer), api.ProfileUpdate{Pseudonym: strptr("Test Member")}, http.StatusOK)
	// Clearing a pseudonym that signs nothing.
	got = testapp.Decode[api.Profile](post(member, api.ProfileUpdate{Pseudonym: strptr("")}, http.StatusOK))
	if got.Pseudonym != "" {
		t.Errorf("cleared: %+v", got)
	}

	// External links: at most five https URLs, labels at most 64 characters.
	six := slices.Repeat([]api.ExternalLink{{URL: "https://example.test/x"}}, 6)
	for _, l := range [][]api.ExternalLink{six, {{URL: "http://example.test/"}}, {{URL: "javascript:alert(1)"}},
		{{URL: "https://test-user@example.test/"}}, {{URL: "https:///path"}}, {{URL: "https://example.test/a b"}},
		{{URL: "https://example.test/", Label: strings.Repeat("x", 65)}},
		{{URL: "https://example.test/", Label: "a\u202eb"}}} {
		r := post(member, api.ProfileUpdate{ExternalLinks: &l}, http.StatusBadRequest)
		if code, _ := profileError(t, r); code != api.CodeExternalLinksInvalid {
			t.Errorf("links %v: %s", l, code)
		}
	}

	// The author's pseudonym signs a recommendation: it cannot be cleared, but it can be changed, and every reader
	// then sees the new pseudonym on it, without links and without the author's uuid.
	author := a.As(testapp.Author)
	if code, _ := profileError(t, post(author, api.ProfileUpdate{Pseudonym: strptr(" ")}, http.StatusConflict)); code !=
		api.CodePseudonymInUse {
		t.Errorf("clearing a pseudonym in use: %s", code)
	}
	post(author, api.ProfileUpdate{Pseudonym: strptr("Test Second Pen")}, http.StatusOK)
	for _, id := range []testapp.Identity{testapp.Member, testapp.Maintainer, testapp.Author} {
		v := viewerOf(a, id)
		views, err := api.RecommendationViews(a, v, []*core.Record{a.PseudonymRecommendation, a.Recommendation})
		if err != nil || len(views) != 2 {
			t.Fatalf("%s: %d views, %v", id, len(views), err)
		}
		if views[0].DisplayName != "Test Second Pen" || len(views[0].ExternalLinks) != 0 {
			t.Errorf("%s: pseudonym recommendation %+v", id, views[0])
		}
		if views[1].DisplayName != "Test Author" || len(views[1].ExternalLinks) != 1 {
			t.Errorf("%s: main-name recommendation %+v", id, views[1])
		}
		body, _ := json.Marshal(views[0])
		for _, leak := range []string{a.Author.GetString(store.FieldUUID), a.Author.Id, "Test Author",
			testapp.AuthorLink, "test_author"} {
			if strings.Contains(string(body), leak) {
				t.Errorf("%s: the pseudonym recommendation carries %q: %s", id, leak, body)
			}
		}
	}
	// The old pseudonym is free again; once the recommendation is deleted, the pseudonym can be cleared.
	post(member, api.ProfileUpdate{Pseudonym: strptr("Test Pen Name")}, http.StatusOK)
	if err := store.SoftDelete(a, a.PseudonymRecommendation); err != nil {
		t.Fatal(err)
	}
	post(author, api.ProfileUpdate{Pseudonym: strptr("")}, http.StatusOK)
	if n, _ := a.CountRecords(store.AuditLog, dbx.HashExp{"action": api.AuditPseudonymChange}); n != 6 {
		t.Errorf("%d pseudonym audit entries, want 6", n)
	}

	// The maintainers' member list still shows no pseudonym (M2-Q7).
	list := a.As(testapp.Maintainer).Get(auth.MembersPath).Expect(http.StatusOK).Body.String()
	if strings.Contains(list, "Test Pen Name") || strings.Contains(strings.ToLower(list), "pseudonym") {
		t.Errorf("member list: %s", list)
	}

	a.CheckNoInternalIDs()
}

// viewerOf returns the domain.Viewer of a signed-in identity of the seeded instance.
func viewerOf(a *testapp.App, id testapp.Identity) domain.Viewer {
	switch id {
	case testapp.Member:
		return api.MemberViewer(a.Member)
	case testapp.Author:
		return api.MemberViewer(a.Author)
	case testapp.Maintainer:
		return api.MemberViewer(a.Maintainer)
	}
	return domain.Viewer{}
}

// TestProfileLegacyNames: names from before the uniqueness rule (M2-Q6) do not lock a member out of their profile. A
// main name that is the login name is flagged and cannot be sent again; a main name another member shares can be kept
// while the rest changes, but not taken by a third member.
func TestProfileLegacyNames(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	for _, m := range []struct {
		id   string
		name string
	}{{a.Member.Id, "test_member"}, {a.Maintainer.Id, "Test Shared"}, {a.Author.Id, "Test Shared"}} {
		r, err := a.FindRecordById(store.Members, m.id)
		if err != nil {
			t.Fatal(err)
		}
		r.Set("display_name", m.name)
		if err := a.Save(r); err != nil {
			t.Fatal(err)
		}
	}

	member := a.As(testapp.Member)
	if p := testapp.Decode[api.Profile](member.Get(api.ProfilePath)); !p.DisplayNameIsLoginName {
		t.Errorf("not flagged: %+v", p)
	}
	member.Post(api.ProfilePath, api.ProfileUpdate{DisplayName: strptr("test_member")}).Expect(http.StatusBadRequest)
	member.Post(api.ProfilePath, api.ProfileUpdate{Pseudonym: strptr("Test Legacy Pen")}).Expect(http.StatusOK)
	member.Post(api.ProfilePath, api.ProfileUpdate{DisplayName: strptr("test shared")}).Expect(http.StatusConflict)
	p := testapp.Decode[api.Profile](member.Post(api.ProfilePath,
		api.ProfileUpdate{DisplayName: strptr("Test Legacy Member")}).Expect(http.StatusOK))
	if p.DisplayNameIsLoginName {
		t.Errorf("still flagged: %+v", p)
	}

	// The two members sharing a main name keep it when they save it unchanged.
	a.As(testapp.Author).Post(api.ProfilePath, api.ProfileUpdate{DisplayName: strptr("Test Shared"),
		ExternalLinks: &[]api.ExternalLink{}}).Expect(http.StatusOK)
}

// TestProfileNameRace: two members asking for the same new name at once: one gets it, the other 409.
func TestProfileNameRace(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	for round := range 5 {
		name := "Test Race " + strings.Repeat("x", round+1)
		clients := []*testapp.Client{a.As(testapp.Member), a.As(testapp.Maintainer)}
		codes := make([]int, len(clients))
		var wg sync.WaitGroup
		for i, c := range clients {
			wg.Add(1)
			go func() {
				defer wg.Done()
				field := api.ProfileUpdate{DisplayName: strptr(name)}
				if i == 1 {
					field = api.ProfileUpdate{Pseudonym: strptr(strings.ToUpper(name))}
				}
				codes[i] = c.Post(api.ProfilePath, field).Code
			}()
		}
		wg.Wait()
		slices.Sort(codes)
		if !slices.Equal(codes, []int{http.StatusOK, http.StatusConflict}) {
			t.Errorf("round %d: %v", round, codes)
		}
	}
}
