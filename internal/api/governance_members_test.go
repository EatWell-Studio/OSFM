package api_test

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// addMember saves a made-up member invited by inviter (nil for none), with the role.
func addMember(t *testing.T, a *testapp.App, name string, inviter *core.Record, role string) *core.Record {
	t.Helper()
	m, err := auth.NewMemberRecord(a)
	if err != nil {
		t.Fatal(err)
	}
	m.Set("login_name", "test_"+strings.ToLower(strings.ReplaceAll(name, " ", "_")))
	m.Set("display_name", "Test "+name)
	m.Set("role", role)
	m.Set("can_invite", true)
	m.Set("joined_at", time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if inviter != nil {
		m.Set("invited_by", inviter.Id)
	}
	if err := a.Save(m); err != nil {
		t.Fatal(err)
	}
	return m
}

// makeStale moves the last verification of every session of member out of the re-verification window.
func makeStale(t *testing.T, a *testapp.App, member *core.Record) {
	t.Helper()
	old := time.Now().Add(-auth.StepUpWindow - time.Minute)
	if _, err := a.DB().Update(store.Sessions, dbx.Params{"verified_at": dbTime(old)},
		dbx.HashExp{"member": member.Id}).Execute(); err != nil {
		t.Fatal(err)
	}
}

func reload(t *testing.T, a *testapp.App, r *core.Record) *core.Record {
	t.Helper()
	fresh, err := a.FindRecordById(r.Collection().Name, r.Id)
	if err != nil {
		t.Fatal(err)
	}
	return fresh
}

func uuidOf(r *core.Record) string { return r.GetString(store.FieldUUID) }

func expectCode(t *testing.T, r *testapp.Response, code string) {
	t.Helper()
	if e := testapp.Decode[api.Error](r); e.Code != code {
		t.Errorf("%s %s: code %q, want %q", r.Method, r.Path, e.Code, code)
	}
}

// refused checks that the identities other than the maintainer are refused: 401 anonymous, 403 for members.
func refused(t *testing.T, a *testapp.App, send func(c *testapp.Client) *testapp.Response) {
	t.Helper()
	send(a.As(testapp.Anonymous)).Expect(http.StatusUnauthorized)
	for _, id := range []testapp.Identity{testapp.Member, testapp.Author} {
		expectCode(t, send(a.As(id)).Expect(http.StatusForbidden), api.CodeForbidden)
	}
}

func names(nodes []api.TreeMember) []string {
	out := []string{}
	for _, n := range nodes {
		out = append(out, n.DisplayName)
	}
	return out
}

// TestMemberTree: maintainers see who invited whom, members who left included, deleted members left out (the members
// they invited hang from the next member above), with no login names and no pseudonyms (M2-Q7).
func TestMemberTree(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	refused(t, a, func(c *testapp.Client) *testapp.Response { return c.Get(api.MemberTreePath) })

	child := addMember(t, a, "Child", a.Author, auth.RoleMember)
	gone := addMember(t, a, "Gone", a.Author, auth.RoleMember)
	setMember(t, a, gone, dbx.Params{"left_at": dbTime(time.Now())})
	deleted := addMember(t, a, "Deleted", a.Member, auth.RoleMember)
	orphan := addMember(t, a, "Orphan", deleted, auth.RoleMember)
	if err := store.SoftDelete(a, reload(t, a, deleted)); err != nil {
		t.Fatal(err)
	}
	setMember(t, a, orphan, dbx.Params{"joined_at": dbTime(time.Now().Add(-day))}) // in the cooling-off period

	r := a.As(testapp.Maintainer).Get(api.MemberTreePath).Expect(http.StatusOK)
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", r.Header().Get("Cache-Control"))
	}
	body := r.Body.String()
	for _, secret := range []string{"test_maintainer", "test_member", "test_author", "Test Pen Name", "login",
		"pseudonym", "Test Deleted"} {
		if strings.Contains(body, secret) {
			t.Errorf("the tree contains %q: %s", secret, body)
		}
	}
	tree := testapp.Decode[api.MemberTree](r)
	if len(tree.Roots) != 1 || tree.Roots[0].UUID != uuidOf(a.Maintainer) || tree.Roots[0].Role != api.RoleMaintainer {
		t.Fatalf("roots: %+v", tree.Roots)
	}
	root := tree.Roots[0]
	if got := names(root.Invited); !slices.Equal(got, []string{"Test Member", "Test Author"}) {
		t.Fatalf("invited by the maintainer: %v", got)
	}
	member, author := root.Invited[0], root.Invited[1]
	if got := names(author.Invited); !slices.Equal(got, []string{"Test Child", "Test Gone"}) {
		t.Errorf("invited by the author: %v", got)
	}
	if author.Invited[1].LeftAt == nil || author.Invited[0].LeftAt != nil || !author.Invited[0].CanInvite {
		t.Errorf("author's invitees: %+v", author.Invited)
	}
	if len(member.Invited) != 1 || member.Invited[0].UUID != uuidOf(orphan) || !member.Invited[0].InviteCooldown {
		t.Errorf("the deleted member's invitee hangs from the member: %+v", member.Invited)
	}
	if root.JoinedAt.IsZero() || root.LeftAt != nil || root.InviteCooldown || child == nil {
		t.Errorf("root: %+v", root)
	}
	a.CheckNoInternalIDs()
}

// TestRemoveMember: a maintainer removes a member with a fresh proof: the member's sessions end at once, they cannot
// sign in, their unused invites are revoked; with "hide" all their recommendations (main name and pseudonym) are hidden
// from members and shown to maintainers with the kind; with "keep" they stay, signed as by a member who left. Nobody
// removes themselves or a maintainer.
func TestRemoveMember(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	hide := api.RemoveMemberRequest{Recommendations: api.RecommendationsHide, Reason: "Test reason, made up"}
	removePath := memberPath(api.MemberRemovePath, a.Author)
	refused(t, a, func(c *testapp.Client) *testapp.Response { return c.Post(removePath, hide) })

	maintainer := a.As(testapp.Maintainer)
	for _, body := range []any{nil, map[string]any{}, api.RemoveMemberRequest{Recommendations: "delete", Reason: "x"},
		api.RemoveMemberRequest{Recommendations: api.RecommendationsKeep, Reason: "  "},
		api.RemoveMemberRequest{Recommendations: api.RecommendationsKeep, Reason: strings.Repeat("x", 501)}} {
		maintainer.Post(removePath, body).Expect(http.StatusBadRequest)
	}
	maintainer.Post(strings.Replace(api.MemberRemovePath, "{uuid}", "0190a1b2-0000-7000-8000-00000000ffff", 1), hide).
		Expect(http.StatusNotFound)
	expectCode(t, maintainer.Post(memberPath(api.MemberRemovePath, a.Maintainer), hide).Expect(http.StatusConflict),
		api.CodeCannotRemoveSelf)
	other := addMember(t, a, "Second Maintainer", a.Maintainer, auth.RoleMaintainer)
	expectCode(t, maintainer.Post(memberPath(api.MemberRemovePath, other), hide).Expect(http.StatusConflict),
		api.CodeCannotRemoveMaintainer)

	// A stale session is asked to re-verify; nothing changes.
	stale := a.As(testapp.Maintainer)
	makeStale(t, a, a.Maintainer)
	e := testapp.Decode[auth.AuthError](stale.Post(removePath, hide).Expect(http.StatusForbidden))
	if e.Code != auth.CodeReauthRequired {
		t.Errorf("stale: %+v", e)
	}

	// The author has a session and an unused invite.
	setMember(t, a, a.Author, dbx.Params{"can_invite": true})
	authorClient := a.As(testapp.Author)
	authorClient.Post(auth.InvitesPath, auth.CreateInviteRequest{Days: 7}).Expect(http.StatusCreated)
	authorClient.Get(auth.MePath).Expect(http.StatusOK)

	removed := testapp.Decode[api.RemovedMember](a.As(testapp.Maintainer).Post(removePath, hide).Expect(http.StatusOK))
	if removed.UUID != uuidOf(a.Author) || removed.HiddenRecommendations != 3 {
		t.Errorf("removed: %+v", removed)
	}
	authorClient.Get(auth.MePath).Expect(http.StatusUnauthorized)
	author := reload(t, a, a.Author)
	if auth.CanSignIn(author) {
		t.Error("the removed member can still sign in")
	}
	if _, _, err := auth.CreateSession(a, author); !errors.Is(err, auth.ErrCannotSignIn) {
		t.Errorf("a session for the removed member: %v", err)
	}
	invites, err := a.FindAllRecords(store.Invites, dbx.HashExp{"created_by": a.Author.Id})
	if err != nil || len(invites) != 1 || invites[0].GetDateTime("revoked_at").IsZero() {
		t.Errorf("the removed member's invite is not revoked (%v)", err)
	}

	// Hidden from members, shown to maintainers with the kind and the reason.
	for _, place := range []*core.Record{a.Place, a.PseudonymPlace} {
		a.As(testapp.Member).Get(memberPath(api.PlaceCardPath, place)).Expect(http.StatusNotFound)
		card := testapp.Decode[api.PlaceCard](a.As(testapp.Maintainer).Get(memberPath(api.PlaceCardPath, place)).
			Expect(http.StatusOK))
		if len(card.Recommendations) != 1 || card.Recommendations[0].Hidden == nil ||
			card.Recommendations[0].Hidden.Kind != store.HiddenMemberRemoved ||
			card.Recommendations[0].Hidden.Reason != hide.Reason {
			t.Errorf("maintainer's card of %s: %+v", place.GetString("name"), card.Recommendations)
		}
	}
	rec := reload(t, a, a.PseudonymRecommendation)
	if rec.GetString("hidden_by") != a.Maintainer.Id || rec.GetDateTime("hidden_at").IsZero() || !rec.GetBool("hidden") {
		t.Errorf("hidden fields: by %q at %v", rec.GetString("hidden_by"), rec.GetDateTime("hidden_at"))
	}
	if n := auditCount(t, a, api.AuditMemberRemove); n != 1 {
		t.Errorf("%d removal audit entries", n)
	}
	entry, err := a.FindFirstRecordByData(store.AuditLog, "action", api.AuditMemberRemove)
	if err != nil {
		t.Fatal(err)
	}
	if entry.GetString("actor") != a.Maintainer.Id || entry.GetString("target_uuid") != uuidOf(a.Author) {
		t.Errorf("audit entry: actor %s, target %s", entry.GetString("actor"), entry.GetString("target_uuid"))
	}
	// Removed already.
	a.As(testapp.Maintainer).Post(removePath, hide).Expect(http.StatusNotFound)

	// Keep: a member's recommendation stays, signed as by a member who left.
	writer := addMember(t, a, "Writer", a.Member, auth.RoleMember)
	place := a.SeedPlace(testapp.PlaceSeed{Name: "Test Tea House", Lat: 0.341, Lon: 7.061})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: place, Author: writer})
	kept := testapp.Decode[api.RemovedMember](a.As(testapp.Maintainer).Post(memberPath(api.MemberRemovePath, writer),
		api.RemoveMemberRequest{Recommendations: api.RecommendationsKeep, Reason: "Test reason"}).Expect(http.StatusOK))
	if kept.HiddenRecommendations != 0 {
		t.Errorf("kept: %+v", kept)
	}
	card := testapp.Decode[api.PlaceCard](a.As(testapp.Member).Get(memberPath(api.PlaceCardPath, place)).
		Expect(http.StatusOK))
	if len(card.Recommendations) != 1 || !card.Recommendations[0].AuthorLeft || card.Recommendations[0].DisplayName != "" ||
		card.Recommendations[0].Hidden != nil {
		t.Errorf("kept recommendation: %+v", card.Recommendations)
	}
	a.CheckNoInternalIDs()
}

// TestBranch: the preview lists the branch's members in the circle (through members who left), maintainers as
// skipped; executing with a list that is no longer the branch is 409; stopping invites and removing act on the
// affected members only, with a fresh proof and audit entries.
func TestBranch(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	// Maintainer → Author → X → Gone (left) → Y; Author → Deputy (maintainer); Member stays outside.
	x := addMember(t, a, "X", a.Author, auth.RoleMember)
	gone := addMember(t, a, "Gone", x, auth.RoleMember)
	y := addMember(t, a, "Y", gone, auth.RoleMember)
	setMember(t, a, gone, dbx.Params{"left_at": dbTime(time.Now())})
	deputy := addMember(t, a, "Deputy", a.Author, auth.RoleMaintainer)
	setMember(t, a, a.Author, dbx.Params{"can_invite": true})

	previewPath := memberPath(api.BranchPreviewPath, a.Author)
	branchPath := memberPath(api.BranchPath, a.Author)
	stop := api.BranchPreviewRequest{Action: api.BranchStopInvites}
	refused(t, a, func(c *testapp.Client) *testapp.Response { return c.Post(previewPath, stop) })
	refused(t, a, func(c *testapp.Client) *testapp.Response {
		return c.Post(branchPath, api.BranchRequest{Action: api.BranchStopInvites, Affected: []string{}})
	})

	maintainer := a.As(testapp.Maintainer)
	maintainer.Post(previewPath, api.BranchPreviewRequest{Action: "delete"}).Expect(http.StatusBadRequest)
	maintainer.Post(strings.Replace(api.BranchPreviewPath, "{uuid}", "x", 1), stop).Expect(http.StatusNotFound)
	p := testapp.Decode[api.BranchPreview](maintainer.Post(previewPath, stop).Expect(http.StatusOK))
	uuids := func(ms []api.BranchMember) []string {
		out := []string{}
		for _, m := range ms {
			out = append(out, m.UUID)
		}
		return out
	}
	if got, want := uuids(p.Affected), []string{uuidOf(a.Author), uuidOf(x), uuidOf(y)}; !slices.Equal(got, want) {
		t.Fatalf("affected %v, want %v", got, want)
	}
	if p.Affected[2].Depth != 3 || p.Affected[0].Depth != 0 || p.Affected[1].DisplayName != "Test X" {
		t.Errorf("affected: %+v", p.Affected)
	}
	if len(p.Skipped) != 1 || p.Skipped[0].UUID != uuidOf(deputy) || p.Skipped[0].Role != api.RoleMaintainer {
		t.Errorf("skipped: %+v", p.Skipped)
	}

	// Bad requests, a stale session, a list that is not the branch.
	maintainer.Post(branchPath, api.BranchRequest{Action: api.BranchStopInvites}).Expect(http.StatusBadRequest)
	maintainer.Post(branchPath, api.BranchRequest{Action: api.BranchRemove, Affected: uuids(p.Affected)}).
		Expect(http.StatusBadRequest) // no recommendations choice and reason
	expectCode(t, maintainer.Post(branchPath, api.BranchRequest{Action: api.BranchStopInvites,
		Affected: uuids(p.Affected)[:2]}).Expect(http.StatusConflict), api.CodeBranchChanged)
	stale := a.As(testapp.Maintainer)
	makeStale(t, a, a.Maintainer)
	stale.Post(branchPath, api.BranchRequest{Action: api.BranchStopInvites, Affected: uuids(p.Affected)}).
		Expect(http.StatusForbidden)

	// Stop invites, with the list in another order.
	affected := uuids(p.Affected)
	slices.Reverse(affected)
	done := testapp.Decode[api.BranchPreview](a.As(testapp.Maintainer).Post(branchPath,
		api.BranchRequest{Action: api.BranchStopInvites, Affected: affected}).Expect(http.StatusOK))
	if len(done.Affected) != 3 {
		t.Errorf("done: %+v", done)
	}
	for _, m := range []*core.Record{a.Author, x, y} {
		if reload(t, a, m).GetBool("can_invite") {
			t.Errorf("%s may still invite", m.GetString("display_name"))
		}
	}
	if !reload(t, a, deputy).GetBool("can_invite") {
		t.Error("the maintainer of the branch was changed")
	}
	if n := auditCount(t, a, auth.AuditInvitePermission); n != 3 {
		t.Errorf("%d permission audit entries, want 3", n)
	}
	if n := auditCount(t, a, api.AuditBranch); n != 1 {
		t.Errorf("%d branch audit entries, want 1", n)
	}

	// The branch grows after the preview: 409, nothing removed.
	late := addMember(t, a, "Late", y, auth.RoleMember)
	remove := api.BranchRequest{Action: api.BranchRemove, Affected: uuids(p.Affected),
		Recommendations: api.RecommendationsHide, Reason: "Test reason"}
	expectCode(t, a.As(testapp.Maintainer).Post(branchPath, remove).Expect(http.StatusConflict), api.CodeBranchChanged)
	if !auth.CanSignIn(reload(t, a, a.Author)) {
		t.Error("removed despite the conflict")
	}

	// Preview again and remove.
	p = testapp.Decode[api.BranchPreview](a.As(testapp.Maintainer).Post(previewPath,
		api.BranchPreviewRequest{Action: api.BranchRemove}).Expect(http.StatusOK))
	remove.Affected = uuids(p.Affected)
	if len(remove.Affected) != 4 {
		t.Fatalf("affected after the change: %+v", p.Affected)
	}
	a.As(testapp.Maintainer).Post(branchPath, remove).Expect(http.StatusOK)
	for _, m := range []*core.Record{a.Author, x, y, late} {
		if auth.CanSignIn(reload(t, a, m)) {
			t.Errorf("%s is still in the circle", m.GetString("display_name"))
		}
	}
	if !auth.CanSignIn(reload(t, a, deputy)) || !auth.CanSignIn(reload(t, a, a.Member)) {
		t.Error("the maintainer of the branch or a member outside it was removed")
	}
	a.As(testapp.Member).Get(memberPath(api.PlaceCardPath, a.Place)).Expect(http.StatusNotFound)
	if n := auditCount(t, a, api.AuditMemberRemove); n != 4 {
		t.Errorf("%d removal audit entries, want 4", n)
	}
	a.CheckNoInternalIDs()
}

// TestMemberRole: maintainers make members maintainers and back, with a fresh proof and audit entries; nobody demotes
// themselves; two maintainers demoting each other at once leave one maintainer.
func TestMemberRole(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	promote := api.RoleRequest{Role: api.RoleMaintainer}
	demote := api.RoleRequest{Role: api.RoleMember}
	rolePath := memberPath(api.MemberRolePath, a.Member)
	refused(t, a, func(c *testapp.Client) *testapp.Response { return c.Post(rolePath, promote) })

	maintainer := a.As(testapp.Maintainer)
	maintainer.Post(rolePath, api.RoleRequest{Role: "owner"}).Expect(http.StatusBadRequest)
	maintainer.Post(rolePath, nil).Expect(http.StatusBadRequest)
	expectCode(t, maintainer.Post(memberPath(api.MemberRolePath, a.Maintainer), demote).Expect(http.StatusConflict),
		api.CodeCannotDemoteSelf)
	maintainer.Post(memberPath(api.MemberRolePath, a.Maintainer), promote).Expect(http.StatusOK) // no change
	gone := addMember(t, a, "Gone", a.Maintainer, auth.RoleMember)
	setMember(t, a, gone, dbx.Params{"left_at": dbTime(time.Now())})
	maintainer.Post(memberPath(api.MemberRolePath, gone), promote).Expect(http.StatusNotFound)
	stale := a.As(testapp.Maintainer)
	makeStale(t, a, a.Maintainer)
	stale.Post(rolePath, promote).Expect(http.StatusForbidden)
	if n := auditCount(t, a, api.AuditMemberRole); n != 0 {
		t.Errorf("%d role audit entries before any change", n)
	}

	r := testapp.Decode[api.MemberRole](a.As(testapp.Maintainer).Post(rolePath, promote).Expect(http.StatusOK))
	if r.UUID != uuidOf(a.Member) || r.Role != api.RoleMaintainer {
		t.Errorf("promoted: %+v", r)
	}
	a.As(testapp.Member).Get(api.MemberTreePath).Expect(http.StatusOK)
	if n := auditCount(t, a, api.AuditMemberRole); n != 1 {
		t.Errorf("%d role audit entries", n)
	}

	// Both maintainers demote each other at the same moment: one of them stays a maintainer.
	clients := []*testapp.Client{a.As(testapp.Maintainer), a.As(testapp.Member)}
	targets := []*core.Record{a.Member, a.Maintainer}
	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i := range clients {
		wg.Go(func() {
			statuses[i] = clients[i].Post(memberPath(api.MemberRolePath, targets[i]), demote).Code
		})
	}
	wg.Wait()
	n := 0
	for _, m := range targets {
		if reload(t, a, m).GetString("role") == auth.RoleMaintainer {
			n++
		}
	}
	slices.Sort(statuses)
	if n != 1 || statuses[0] != http.StatusOK || statuses[1] != http.StatusForbidden {
		t.Errorf("after demoting each other: %d maintainers, statuses %v", n, statuses)
	}
	a.CheckNoInternalIDs()
}
