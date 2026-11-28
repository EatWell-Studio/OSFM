package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

const day = 24 * time.Hour

// TestInviteConstants: the generated types carry the domain's values.
func TestInviteConstants(t *testing.T) {
	if api.InviteReasonCooldown != domain.InviteReasonCooldown || api.InviteReasonDisabled != domain.InviteReasonDisabled ||
		api.MaxActiveInvites != domain.MaxActiveInvites {
		t.Error("the invite constants of api and domain differ")
	}
}

// setMember changes a member's columns in SQL.
func setMember(t *testing.T, a *testapp.App, member *core.Record, params dbx.Params) {
	t.Helper()
	if _, err := a.DB().Update(store.Members, params, dbx.HashExp{"id": member.Id}).Execute(); err != nil {
		t.Fatal(err)
	}
}

func dbTime(t time.Time) string { return t.UTC().Format(types.DefaultDateLayout) }

func auditCount(t *testing.T, a *testapp.App, action string) int64 {
	t.Helper()
	n, err := a.CountRecords(store.AuditLog, dbx.HashExp{"action": action})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func memberPath(pattern string, member *core.Record) string {
	return strings.Replace(pattern, "{uuid}", member.GetString(store.FieldUUID), 1)
}

// TestInviteStatus: every member learns whether they may invite now and why not, with the end of the cooling-off
// period, which follows the setting at once (D12, M2-Q11).
func TestInviteStatus(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	a.As(testapp.Anonymous).Get(api.InviteStatusPath).Expect(http.StatusUnauthorized)

	r := a.As(testapp.Maintainer).Get(api.InviteStatusPath).Expect(http.StatusOK)
	if s := testapp.Decode[api.InviteStatus](r); !s.Allowed || s.Reason != "" || s.CooldownEndsAt != nil ||
		s.MaxActiveInvites != nil || s.ActiveInvites != 0 {
		t.Errorf("maintainer: %+v", s)
	}
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", r.Header().Get("Cache-Control"))
	}

	status := func() api.InviteStatus {
		t.Helper()
		return testapp.Decode[api.InviteStatus](a.As(testapp.Member).Get(api.InviteStatusPath).Expect(http.StatusOK))
	}
	// The seeded member's permission is off (as for members from before M2-Q11 who were not migrated).
	joined := a.Member.GetDateTime("joined_at").Time()
	if s := status(); s.Allowed || s.Reason != api.InviteReasonDisabled || s.CooldownEndsAt == nil ||
		!s.CooldownEndsAt.Equal(joined.Add(30*day)) || s.MaxActiveInvites == nil || *s.MaxActiveInvites != 3 {
		t.Errorf("permission off: %+v", s)
	}

	recent := time.Now().UTC().Add(-day).Truncate(time.Second)
	setMember(t, a, a.Member, dbx.Params{"can_invite": true, "joined_at": dbTime(recent)})
	if s := status(); s.Allowed || s.Reason != api.InviteReasonCooldown || !s.CooldownEndsAt.Equal(recent.Add(30*day)) {
		t.Errorf("cooling-off period: %+v", s)
	}
	a.As(testapp.Member).Post(auth.InvitesPath, nil).Expect(http.StatusForbidden)

	// The maintainer shortens the period: it applies at once.
	days := 1
	a.As(testapp.Maintainer).Post(api.InstancePath, api.InstanceUpdate{InviteCooldownDays: &days}).Expect(http.StatusOK)
	if s := status(); !s.Allowed || s.Reason != "" || !s.CooldownEndsAt.Equal(recent.Add(day)) {
		t.Errorf("period shortened: %+v", s)
	}
	a.As(testapp.Member).Post(auth.InvitesPath, nil).Expect(http.StatusCreated)
	if s := status(); s.ActiveInvites != 1 {
		t.Errorf("active invites: %d", s.ActiveInvites)
	}
	a.CheckNoInternalIDs()
}

// TestInvitePermissionAndEarly: maintainers switch a member's invite permission and open inviting early, with audit
// entries; members cannot; a member opened early invites someone who then has them as inviter.
func TestInvitePermissionAndEarly(t *testing.T) {
	t.Parallel()
	a := start(t, api.DefaultLimits())
	permission := memberPath(api.InvitePermissionPath, a.Member)
	early := memberPath(api.InviteEarlyPath, a.Member)
	on, off := true, false

	for _, id := range []testapp.Identity{testapp.Anonymous, testapp.Member, testapp.Author} {
		want := http.StatusForbidden
		if id == testapp.Anonymous {
			want = http.StatusUnauthorized
		}
		a.As(id).Post(permission, api.InvitePermissionRequest{CanInvite: &on}).Expect(want)
		a.As(id).Post(early, nil).Expect(want)
	}
	if m, _ := a.FindRecordById(store.Members, a.Member.Id); m.GetBool("can_invite") {
		t.Error("a member's request switched the permission on")
	}

	maintainer := a.As(testapp.Maintainer)
	maintainer.Post(permission, nil).Expect(http.StatusBadRequest)
	maintainer.Post(permission, map[string]any{}).Expect(http.StatusBadRequest)
	for _, id := range []string{"x", "0190a1b2-0000-7000-8000-00000000ffff"} {
		maintainer.Post(strings.Replace(api.InvitePermissionPath, "{uuid}", id, 1),
			api.InvitePermissionRequest{CanInvite: &on}).Expect(http.StatusNotFound)
	}

	// On, in the cooling-off period (joined yesterday).
	recent := time.Now().UTC().Add(-day).Truncate(time.Second)
	setMember(t, a, a.Member, dbx.Params{"joined_at": dbTime(recent)})
	r := testapp.Decode[api.MemberInviteRights](maintainer.Post(permission,
		api.InvitePermissionRequest{CanInvite: &on}).Expect(http.StatusOK))
	if r.UUID != a.Member.GetString(store.FieldUUID) || !r.CanInvite || r.Allowed ||
		r.Reason != api.InviteReasonCooldown || !r.CooldownEndsAt.Equal(recent.Add(30*day)) {
		t.Errorf("switched on: %+v", r)
	}
	// Again: nothing changes, no second audit entry.
	maintainer.Post(permission, api.InvitePermissionRequest{CanInvite: &on}).Expect(http.StatusOK)
	if n := auditCount(t, a, auth.AuditInvitePermission); n != 1 {
		t.Errorf("%d permission audit entries, want 1", n)
	}

	// Opened early: the member may invite now.
	r = testapp.Decode[api.MemberInviteRights](maintainer.Post(early, nil).Expect(http.StatusOK))
	if !r.Allowed || r.Reason != "" || r.CooldownEndsAt == nil || r.CooldownEndsAt.After(time.Now()) {
		t.Errorf("opened early: %+v", r)
	}
	maintainer.Post(early, nil).Expect(http.StatusOK) // over already: nothing to do
	if n := auditCount(t, a, auth.AuditInviteEarly); n != 1 {
		t.Errorf("%d early audit entries, want 1", n)
	}
	entry, err := a.FindFirstRecordByData(store.AuditLog, "action", auth.AuditInviteEarly)
	if err != nil {
		t.Fatal(err)
	}
	if entry.GetString("actor") != a.Maintainer.Id || entry.GetString("target_uuid") != a.Member.GetString(store.FieldUUID) {
		t.Errorf("audit entry: actor %s, target %s", entry.GetString("actor"), entry.GetString("target_uuid"))
	}

	// The member invites; the new member has them as inviter.
	created := testapp.Decode[auth.CreatedInvite](a.As(testapp.Member).Post(auth.InvitesPath,
		auth.CreateInviteRequest{Days: 1}).Expect(http.StatusCreated))
	invited, err := auth.RegisterWithInvite(a, created.Code, func(tx core.App, m *core.Record) error {
		m.Set("login_name", "test_invited")
		m.Set("display_name", "Test Invited")
		return tx.Save(m)
	})
	if err != nil {
		t.Fatal(err)
	}
	if invited.GetString("invited_by") != a.Member.Id {
		t.Errorf("invited_by %q, want the member", invited.GetString("invited_by"))
	}

	// Off: 403 for the member's next invite, with the reason.
	r = testapp.Decode[api.MemberInviteRights](maintainer.Post(permission,
		api.InvitePermissionRequest{CanInvite: &off}).Expect(http.StatusOK))
	if r.CanInvite || r.Allowed || r.Reason != api.InviteReasonDisabled {
		t.Errorf("switched off: %+v", r)
	}
	e := testapp.Decode[auth.AuthError](a.As(testapp.Member).Post(auth.InvitesPath, nil).Expect(http.StatusForbidden))
	if e.Code != auth.CodeInviteNotAllowed || e.Data["reason"] != api.InviteReasonDisabled {
		t.Errorf("403 body: %+v", e)
	}
	if n := auditCount(t, a, auth.AuditInvitePermission); n != 2 {
		t.Errorf("%d permission audit entries, want 2", n)
	}

	// Members who left cannot be managed any more.
	setMember(t, a, a.Author, dbx.Params{"left_at": dbTime(time.Now())})
	maintainer.Post(memberPath(api.InvitePermissionPath, a.Author), api.InvitePermissionRequest{CanInvite: &on}).
		Expect(http.StatusNotFound)
	maintainer.Post(memberPath(api.InviteEarlyPath, a.Author), nil).Expect(http.StatusNotFound)
	a.CheckNoInternalIDs()
}
