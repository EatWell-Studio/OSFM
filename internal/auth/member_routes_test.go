package auth_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/auth/passkeytest"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// TestMemberList: the maintainers' member list shows every member who is not deleted, in the order they joined, with
// role, inviter, whether they can invite, whether they left, their sign-in methods as counts and a pending reset link;
// never login names, pseudonyms, credentials or internal IDs. Members get 403, anonymous callers 401.
func TestMemberList(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	maintainer.Set("display_name", "Head Keeper")
	maintainer.Set("can_invite", true)
	if err := pe.app.Save(maintainer); err != nil {
		t.Fatal(err)
	}
	mt := pe.session(maintainer)

	pe.advance(time.Second)
	pwToken, _, _ := pe.register(maintainer, newLogin) // password and TOTP
	pe.advance(time.Second)
	pkToken, _, _ := pe.passkeyMember(maintainer, "test_passkey")
	if rec := pe.addPasskey(pkToken, &passkeytest.Authenticator{}, "Test Tablet"); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	pe.advance(time.Second)
	pe.passkeyMember(maintainer, "test_left")
	left := pe.memberByLogin("test_left")
	left.Set("left_at", types.NowDateTime())
	if err := pe.app.Save(left); err != nil {
		t.Fatal(err)
	}
	pe.advance(time.Second)
	pe.passkeyMember(maintainer, "test_gone")
	if err := store.SoftDelete(pe.app, pe.memberByLogin("test_gone")); err != nil {
		t.Fatal(err)
	}
	// Display names that do not contain the login names, so that the check below finds any login name.
	for login, name := range map[string]string{newLogin: "Pat Password", "test_passkey": "Kim Passkey",
		"test_left": "Lee Left", "test_gone": "Gil Gone"} {
		m := pe.memberByLogin(login)
		m.Set("display_name", name)
		if login == newLogin {
			m.Set("pseudonym", "Secret Alias")
		}
		if err := pe.app.Save(m); err != nil {
			t.Fatal(err)
		}
	}
	pw := pe.memberByLogin(newLogin)
	_, link := pe.resetLink(mt, "test_passkey")

	for name, token := range map[string]string{"anonymous": "", "a member": pwToken} {
		want := http.StatusForbidden
		if token == "" {
			want = http.StatusUnauthorized
		}
		if got := pe.status(http.MethodGet, auth.MembersPath, token); got != want {
			t.Errorf("%s: %d, want %d", name, got, want)
		}
	}

	rec := pe.reqJSON(http.MethodGet, auth.MembersPath, mt, "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %q %s", rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
	}
	list := decode[auth.MemberList](t, rec).Members
	byUUID := map[string]auth.MemberInfo{}
	var order []string
	for _, m := range list {
		byUUID[m.UUID] = m
		order = append(order, m.DisplayName)
	}
	if strings.Join(order, ",") != "Head Keeper,Pat Password,Kim Passkey,Lee Left" {
		t.Errorf("order %v: want the members in the order they joined, without the deleted one", order)
	}
	uuid := func(r *core.Record) string { return r.GetString(store.FieldUUID) }
	pk, l := pe.memberByLogin("test_passkey"), pe.memberByLogin("test_left")
	ref := &auth.MemberRef{UUID: uuid(maintainer), DisplayName: "Head Keeper"}

	if m := byUUID[uuid(maintainer)]; m.Role != auth.RoleMaintainer || !m.CanInvite || m.InvitedBy != nil ||
		m.Passkeys != 0 || m.PasswordTOTP || m.LeftAt != nil || m.PendingResetLink != nil ||
		m.InviteCooldownEndsAt != nil {
		t.Errorf("maintainer %+v", m)
	}
	// A new member may invite after the cooling-off period (30 days by default) from joining (M2-Q11).
	if m := byUUID[uuid(pw)]; m.Role != auth.RoleMember || !m.CanInvite || *m.InvitedBy != *ref || m.Passkeys != 0 ||
		!m.PasswordTOTP || m.LeftAt != nil || m.JoinedAt.IsZero() || m.InviteCooldownEndsAt == nil ||
		!m.InviteCooldownEndsAt.Equal(m.JoinedAt.Add(30*24*time.Hour)) || !m.InviteCooldown {
		t.Errorf("password member %+v", m)
	}
	if m := byUUID[uuid(pk)]; m.Passkeys != 2 || m.PasswordTOTP || m.PendingResetLink == nil ||
		m.PendingResetLink.UUID != link.UUID || !m.PendingResetLink.ExpiresAt.Equal(link.ExpiresAt) {
		t.Errorf("passkey member %+v", m)
	}
	if m := byUUID[uuid(l)]; m.LeftAt == nil || m.Passkeys != 1 {
		t.Errorf("member who left %+v", m)
	}

	body := rec.Body.String()
	for _, secret := range []string{"test_maintainer", newLogin, "test_passkey", "test_left", "test_gone", "Gil Gone",
		"Secret Alias", "Test Tablet", "login", "pseudonym"} {
		if strings.Contains(body, secret) {
			t.Errorf("the list contains %q", secret)
		}
	}
	// No internal ID (AD4): of the members, their credentials or the reset link.
	ids := []string{maintainer.Id, pw.Id, pk.Id, l.Id, pe.memberByLogin("test_gone").Id}
	creds, err := pe.app.FindAllRecords(store.Credentials)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range creds {
		ids = append(ids, c.Id)
	}
	if r, err := pe.app.FindFirstRecordByData(store.ResetLinks, store.FieldUUID, link.UUID); err == nil {
		ids = append(ids, r.Id)
	}
	for _, id := range ids {
		if strings.Contains(body, id) {
			t.Errorf("the list contains the internal ID %s", id)
		}
	}

	// A used or revoked link is not pending.
	if rec := pe.post(passkeyRoute(auth.ResetLinkRevokePath, link.UUID), mt, nil); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	list = decode[auth.MemberList](t, pe.reqJSON(http.MethodGet, auth.MembersPath, mt, "")).Members
	for _, m := range list {
		if m.PendingResetLink != nil {
			t.Errorf("%s still has a pending link", m.DisplayName)
		}
	}
}
