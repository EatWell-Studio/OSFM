package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// TestMemberList calls the maintainers' member list on the app as `osfm serve` boots it: 401 for an anonymous
// caller, 403 for members and authors, 200 for the maintainer; members registered through the real routes appear with
// their sign-in methods and a pending reset link, by uuid and main name only: no login name, no pseudonym, no internal
// ID.
func TestMemberList(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	pw := a.RegisterPasswordTOTP("test_pw")
	pk := a.RegisterPasskey("test_pk")

	for id, want := range map[testapp.Identity]int{testapp.Anonymous: http.StatusUnauthorized,
		testapp.Member: http.StatusForbidden, testapp.Author: http.StatusForbidden, testapp.Maintainer: http.StatusOK} {
		if r := a.As(id).Get(auth.MembersPath); r.Code != want {
			t.Errorf("%s: %d, want %d: %s", id, r.Code, want, r.Body.String())
		}
	}
	pw.Client.Get(auth.MembersPath).Expect(http.StatusForbidden)

	maintainer := a.As(testapp.Maintainer)
	pkUUID := testapp.Decode[auth.Me](pk.Client.Get(auth.MePath).Expect(http.StatusOK)).UUID
	created := testapp.Decode[auth.CreatedResetLink](maintainer.Post(auth.ResetLinksPath,
		auth.CreateResetLinkRequest{Member: pkUUID}).Expect(http.StatusCreated))

	r := maintainer.Get(auth.MembersPath).Expect(http.StatusOK)
	list := testapp.Decode[auth.MemberList](r).Members
	byUUID := map[string]auth.MemberInfo{}
	for _, m := range list {
		byUUID[m.UUID] = m
	}
	if len(list) != 5 {
		t.Errorf("%d members, want 5: %+v", len(list), list)
	}
	if m := byUUID[a.Maintainer.GetString(store.FieldUUID)]; m.Role != auth.RoleMaintainer || !m.CanInvite {
		t.Errorf("maintainer %+v", m)
	}
	pwUUID := testapp.Decode[auth.Me](pw.Client.Get(auth.MePath).Expect(http.StatusOK)).UUID
	if m := byUUID[pwUUID]; !m.PasswordTOTP || m.Passkeys != 0 || m.PendingResetLink != nil ||
		m.InvitedBy == nil || m.InvitedBy.UUID != a.Maintainer.GetString(store.FieldUUID) {
		t.Errorf("password member %+v", m)
	}
	if m := byUUID[pkUUID]; m.PasswordTOTP || m.Passkeys != 1 || m.PendingResetLink == nil ||
		m.PendingResetLink.UUID != created.ResetLink.UUID {
		t.Errorf("passkey member %+v", m)
	}
	// A login name would be a JSON string of its own (the registered members' display names are "Test <login name>").
	body := r.Body.String()
	for _, text := range []string{`"test_maintainer"`, `"test_member"`, `"test_author"`, `"test_pw"`, `"test_pk"`,
		"Test Pen Name", "loginName", "pseudonym"} {
		if strings.Contains(body, text) {
			t.Errorf("the member list contains %q", text)
		}
	}
	a.CheckNoInternalIDs()
}
