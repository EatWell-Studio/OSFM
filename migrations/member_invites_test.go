package migrations_test

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

const memberInvitesMigration = "1795603400_member_invites.go"

func memberInvitesItem(t *testing.T) *core.Migration {
	t.Helper()
	for _, item := range core.AppMigrations.Items() {
		if item.File == memberInvitesMigration {
			return item
		}
	}
	t.Fatalf("no migration %s", memberInvitesMigration)
	return nil
}

// TestMemberInvitesUpgradeFromM1: members who joined under M1 (can_invite unset) may invite after the upgrade.
func TestMemberInvitesUpgradeFromM1(t *testing.T) {
	dir, ids := m1DataDir(t)
	app := start(t, dir)
	for _, id := range ids[store.Members] {
		m, err := app.FindRecordById(store.Members, id)
		if err != nil {
			t.Fatal(err)
		}
		if !m.GetBool("can_invite") {
			t.Errorf("member %s: can_invite still off after the upgrade", m.GetString("login_name"))
		}
	}
}

// TestMemberInvitesBackfill: the backfill switches the permission on for members nobody decided for, keeps the
// decisions maintainers made (audit entries), changes nothing when run again, and the down migration undoes exactly
// what it did.
func TestMemberInvitesBackfill(t *testing.T) {
	if auditAction := "members.invite_permission"; auth.AuditInvitePermission != auditAction {
		t.Fatalf("auth.AuditInvitePermission is %q; the migration looks for %q", auth.AuditInvitePermission, auditAction)
	}
	app := newApp(t)
	before := snapshot(t, app)
	item := memberInvitesItem(t)
	member := func(login, role string, canInvite bool) *core.Record {
		return newRecord(t, app, store.Members, map[string]any{"login_name": login, "display_name": "Test " + login,
			"role": role, "can_invite": canInvite})
	}
	undecided := member("test_undecided", "member", false)
	switchedOff := member("test_switched_off", "member", false)
	switchedOn := member("test_switched_on", "member", true)
	maintainer := member("test_maintainer", "maintainer", true)
	for _, m := range []*core.Record{switchedOff, switchedOn} {
		if err := store.WriteAudit(app, maintainer, auth.AuditInvitePermission, store.Members,
			m.GetString(store.FieldUUID), map[string]any{"canInvite": m.GetBool("can_invite")}); err != nil {
			t.Fatal(err)
		}
	}
	// An entry of another kind about the undecided member does not count as a decision.
	if err := store.WriteAudit(app, maintainer, auth.AuditInviteEarly, store.Members,
		undecided.GetString(store.FieldUUID), nil); err != nil {
		t.Fatal(err)
	}

	check := func(step string, want map[*core.Record]bool) {
		t.Helper()
		for m, w := range want {
			got, err := app.FindRecordById(store.Members, m.Id)
			if err != nil {
				t.Fatal(err)
			}
			if got.GetBool("can_invite") != w {
				t.Errorf("%s: %s can_invite %v, want %v", step, got.GetString("login_name"), !w, w)
			}
		}
	}
	for _, step := range []string{"up", "up again"} {
		if err := item.Up(app); err != nil {
			t.Fatal(err)
		}
		check(step, map[*core.Record]bool{undecided: true, switchedOff: false, switchedOn: true, maintainer: true})
	}
	if err := item.Down(app); err != nil {
		t.Fatal(err)
	}
	check("down", map[*core.Record]bool{undecided: false, switchedOff: false, switchedOn: true, maintainer: true})
	if err := item.Up(app); err != nil {
		t.Fatal(err)
	}
	check("up after down", map[*core.Record]bool{undecided: true, switchedOff: false, switchedOn: true,
		maintainer: true})
	if after := snapshot(t, app); after != before {
		t.Errorf("the migration changed the schema\nbefore: %s\nafter:  %s", before, after)
	}
}
