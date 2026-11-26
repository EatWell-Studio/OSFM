package auth_test

import (
	"errors"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/store"
	_ "github.com/EatWell-Studio/OSFM/migrations" // the members collection
)

func newApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	store.BindHooks(app)
	return app
}

func newMember(t *testing.T, app core.App, loginName string) *core.Record {
	t.Helper()
	r, err := auth.NewMemberRecord(app)
	if err != nil {
		t.Fatal(err)
	}
	r.Set("login_name", loginName)
	r.Set("display_name", "Test "+loginName)
	r.Set("role", "member")
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
	return r
}

func issue(t *testing.T, member *core.Record) string {
	t.Helper()
	token, err := auth.IssueToken(member)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// TestIssueTokenWithSignInMethodsOff checks that OSFM can issue a member's token although every built-in sign-in
// method of the members collection is off.
func TestIssueTokenWithSignInMethodsOff(t *testing.T) {
	app := newApp(t)
	c, err := app.FindCollectionByNameOrId(store.Members)
	if err != nil {
		t.Fatal(err)
	}
	if c.PasswordAuth.Enabled || c.OTP.Enabled || c.OAuth2.Enabled || c.MFA.Enabled || c.AuthRule != nil {
		t.Fatal("the members collection still has a built-in sign-in method")
	}

	member := newMember(t, app, "alpha")
	got, err := auth.MemberByToken(app, issue(t, member))
	if err != nil {
		t.Fatal(err)
	}
	if got.Id != member.Id {
		t.Errorf("token of %s resolves to %s", member.Id, got.Id)
	}
}

func TestRevokeTokens(t *testing.T) {
	app := newApp(t)
	alpha := newMember(t, app, "alpha")
	beta := newMember(t, app, "beta")
	old1, old2 := issue(t, alpha), issue(t, alpha)
	other := issue(t, beta)

	if err := auth.RevokeTokens(app, alpha); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{old1, old2} {
		if _, err := auth.MemberByToken(app, token); err == nil {
			t.Error("a token issued before RevokeTokens is still valid")
		}
	}
	// A token issued after the rotation works, from the saved record as well as from a fresh copy of it.
	if _, err := auth.MemberByToken(app, issue(t, alpha)); err != nil {
		t.Errorf("new token after RevokeTokens: %v", err)
	}
	reloaded, err := app.FindRecordById(store.Members, alpha.Id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.MemberByToken(app, issue(t, reloaded)); err != nil {
		t.Errorf("new token from the reloaded record: %v", err)
	}
	if _, err := auth.MemberByToken(app, other); err != nil {
		t.Errorf("another member's token stopped working: %v", err)
	}
}

func TestMemberByTokenRejectsOtherTokens(t *testing.T) {
	app := newApp(t)
	member := newMember(t, app, "alpha")

	superusers, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatal(err)
	}
	su := core.NewRecord(superusers)
	su.SetEmail("superuser@example.test")
	su.SetRandomPassword()
	if err := app.Save(su); err != nil {
		t.Fatal(err)
	}
	suToken, err := su.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.MemberByToken(app, suToken); !errors.Is(err, auth.ErrNotMember) {
		t.Errorf("superuser token: %v, want ErrNotMember", err)
	}
	if _, err := auth.IssueToken(su); !errors.Is(err, auth.ErrNotMember) {
		t.Errorf("IssueToken for a superuser: %v, want ErrNotMember", err)
	}

	fileToken, err := member.NewFileToken()
	if err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string]string{"file token": fileToken, "empty": "", "garbage": "not.a.jwt"} {
		if _, err := auth.MemberByToken(app, token); err == nil {
			t.Errorf("%s accepted", name)
		}
	}

	// A token signed for a member that no longer exists.
	token := issue(t, member)
	if err := app.Delete(member); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.MemberByToken(app, token); err == nil {
		t.Error("token of a deleted record accepted")
	}
}
