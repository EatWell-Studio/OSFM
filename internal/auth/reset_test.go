package auth_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/auth/passkeytest"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// resetFor is the request that creates a reset link for the member with the login name: by the member's uuid.
func (pe *pwEnv) resetFor(login string) auth.CreateResetLinkRequest {
	pe.t.Helper()
	return auth.CreateResetLinkRequest{Member: pe.memberByLogin(login).GetString(store.FieldUUID)}
}

// resetLink creates a reset link for login on behalf of the maintainer's session and returns the token.
func (pe *pwEnv) resetLink(maintainerToken, login string) (string, auth.ResetLinkInfo) {
	pe.t.Helper()
	rec := pe.post(auth.ResetLinksPath, maintainerToken, pe.resetFor(login))
	if rec.Code != http.StatusCreated {
		pe.t.Fatalf("create reset link: %d %s", rec.Code, rec.Body.String())
	}
	created := decode[auth.CreatedResetLink](pe.t, rec)
	return created.Token, created.ResetLink
}

// resetPasskey resets with a new passkey of a.
func (pe *pwEnv) resetPasskey(token string, a *passkeytest.Authenticator) *httptest.ResponseRecorder {
	pe.t.Helper()
	rec := pe.post(auth.ResetPasskeyOptionsPath, "", auth.ResetTokenRequest{Token: token})
	if rec.Code != http.StatusOK {
		return rec
	}
	o := decode[optionsResponse](pe.t, rec)
	cred, err := a.Create(o.Options)
	if err != nil {
		pe.t.Fatal(err)
	}
	return pe.post(auth.ResetPasskeyPath, "", map[string]any{"token": token, "state": o.State,
		"credential": json.RawMessage(cred)})
}

// TestResetWithPasskey: a maintainer creates a reset link for a member with a passkey and password and TOTP; the
// member resets to a new passkey. Afterwards the old passkey, password, TOTP and recovery codes cannot sign in, every
// old session has ended, and the link cannot be used again.
func TestResetWithPasskey(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	maintainerToken := pe.session(maintainer)
	oldSession, secret, codes := pe.register(maintainer, newLogin)
	if rec := pe.addPasskey(oldSession, &passkeytest.Authenticator{}, "Test Phone"); rec.Code != http.StatusCreated {
		t.Fatalf("add passkey: %d %s", rec.Code, rec.Body.String())
	}
	member := pe.memberByLogin(newLogin)
	oldPasskey := &passkeytest.Authenticator{}
	if rec := pe.addPasskey(oldSession, oldPasskey, ""); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	oldHandle := pe.reload(member).GetString("passkey_user_handle")
	ticket := pe.ticket(newLogin, testPassword)

	token, info := pe.resetLink(maintainerToken, newLogin)
	if info.State != auth.ResetLinkActive || info.Member.UUID != member.GetString(store.FieldUUID) ||
		!info.ExpiresAt.Equal(pe.clock.Add(auth.ResetLinkLifetime)) {
		t.Errorf("link %+v", info)
	}
	rec := pe.post(auth.ResetCheckPath, "", auth.ResetTokenRequest{Token: token})
	if c := decode[auth.ResetCheckResponse](t, rec); !c.Valid || c.LoginName != newLogin {
		t.Errorf("check %+v", c)
	}

	fresh := &passkeytest.Authenticator{}
	rec = pe.resetPasskey(token, fresh)
	if rec.Code != http.StatusCreated {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}
	done := decode[auth.EnrolledResponse](t, rec)
	newSession := sessionToken(t, rec)
	if done.OtherPasskeys != 0 || len(done.RecoveryCodes) != auth.RecoveryCodeCount {
		t.Errorf("response %+v", done)
	}
	if creds := memberCredentials(t, pe.app, member); len(creds) != 1 || creds[0].GetString("kind") != "passkey" {
		t.Errorf("%d credentials after the reset", len(creds))
	}
	if h := pe.reload(member).GetString("passkey_user_handle"); h == oldHandle || h == "" {
		t.Error("the user handle was not replaced")
	}

	// Old sessions are gone; the new one works.
	if pe.status(http.MethodGet, auth.MePath, oldSession) != http.StatusUnauthorized {
		t.Error("an old session survived the reset")
	}
	if pe.status(http.MethodGet, auth.MePath, newSession) != http.StatusOK {
		t.Error("the new session does not work")
	}
	// The old passkey, password, TOTP, ticket and recovery codes no longer sign in.
	expectError(t, "the old passkey", pe.passkeySignIn(oldPasskey), http.StatusUnauthorized, auth.CodePasskeyFailed)
	expectError(t, "the old password", pe.post(auth.SignInPasswordPath, "",
		auth.SignInPasswordRequest{LoginName: newLogin, Password: testPassword}),
		http.StatusUnauthorized, auth.CodeCredentialsInvalid)
	pe.advance(30 * time.Second)
	expectError(t, "an old ticket with TOTP", pe.post(auth.SignInTOTPPath, "",
		auth.SignInCodeRequest{Ticket: ticket, Code: pe.code(secret, 0)}), http.StatusUnauthorized, auth.CodeTicketExpired)
	expectError(t, "an old ticket with a recovery code", pe.post(auth.SignInRecoveryPath, "",
		auth.SignInCodeRequest{Ticket: ticket, Code: codes[0]}), http.StatusUnauthorized, auth.CodeTicketExpired)
	expectError(t, "an old recovery code", pe.post(auth.RecoverPath, "",
		auth.RecoverRequest{LoginName: newLogin, Code: codes[1]}), http.StatusUnauthorized, auth.CodeRecoveryFailed)
	if rec := pe.passkeySignIn(fresh); rec.Code != http.StatusNoContent {
		t.Errorf("the new passkey: %d %s", rec.Code, rec.Body.String())
	}
	// Used once.
	expectError(t, "the link again", pe.resetPasskey(token, &passkeytest.Authenticator{}), http.StatusForbidden,
		auth.CodeResetInvalid)
	list := decode[auth.ResetLinkList](t, pe.reqJSON(http.MethodGet, auth.ResetLinksPath, maintainerToken, ""))
	if len(list.ResetLinks) != 1 || list.ResetLinks[0].State != auth.ResetLinkUsed {
		t.Errorf("list %+v", list)
	}

	// Audit: created by the maintainer, used by the member, every session revoked for the reset.
	if a := pe.audits(auth.AuditCreateResetLink); len(a) != 1 || a[0].GetString("actor") != maintainer.Id {
		t.Errorf("%d create audit entries", len(a))
	}
	if a := pe.audits(auth.AuditUseResetLink); len(a) != 1 || a[0].GetString("actor") != member.Id {
		t.Errorf("%d use audit entries", len(a))
	}
	revoked := pe.audits(auth.AuditRevokeSessions)
	if len(revoked) != 1 || details(t, revoked[0])["reason"] != auth.RevokeCredentialsReset {
		t.Errorf("%d revoke audit entries", len(revoked))
	}
	checkNoSecretsInAudit(t, pe.app, token, testPassword)
}

// TestResetWithPassword: a passkey-only member resets to password and TOTP; the passkey and user handle are gone.
func TestResetWithPassword(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	_, old, _ := pe.passkeyMember(maintainer, newLogin)
	member := pe.memberByLogin(newLogin)
	token, _ := pe.resetLink(pe.session(maintainer), newLogin)

	rec := pe.post(auth.ResetPasswordStartPath, "", auth.ResetPasswordStartRequest{Token: token, Password: testPassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	start := decode[auth.RegisterPasswordStartResponse](t, rec)
	expectError(t, "a wrong code", pe.post(auth.ResetPasswordConfirmPath, "", auth.ResetPasswordConfirmRequest{
		Token: token, Enrolment: start.Enrolment, Code: wrongCode(pe.code(start.Secret, 0))}),
		http.StatusBadRequest, auth.CodeCodeInvalid)
	// A wrong code leaves everything as it was.
	if n := len(memberCredentials(t, pe.app, member)); n != 1 {
		t.Fatalf("%d credentials after a wrong code", n)
	}
	rec = pe.post(auth.ResetPasswordConfirmPath, "", auth.ResetPasswordConfirmRequest{
		Token: token, Enrolment: start.Enrolment, Code: pe.code(start.Secret, 0)})
	if rec.Code != http.StatusCreated || responseCookie(rec) == nil {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
	}
	if pe.reload(member).GetString("passkey_user_handle") != "" {
		t.Error("the user handle remained")
	}
	expectError(t, "the old passkey", pe.passkeySignIn(old), http.StatusUnauthorized, auth.CodePasskeyFailed)
	pe.advance(30 * time.Second)
	if rec := pe.post(auth.SignInTOTPPath, "", auth.SignInCodeRequest{Ticket: pe.ticket(newLogin, testPassword),
		Code: pe.code(start.Secret, 0)}); rec.Code != http.StatusOK {
		t.Errorf("sign in: %d %s", rec.Code, rec.Body.String())
	}
}

// TestResetTokensNoLeak: an unknown, malformed, used, expired or revoked token, and the token of a member who left,
// get the same answer on every public reset route.
func TestResetTokensNoLeak(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	mt := pe.session(maintainer)
	for _, login := range []string{"test_used", "test_expired", "test_revoked", "test_left"} {
		pe.passkeyMember(maintainer, login)
	}
	used, _ := pe.resetLink(mt, "test_used")
	if rec := pe.resetPasskey(used, &passkeytest.Authenticator{}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	expired, _ := pe.resetLink(mt, "test_expired")
	revoked, revokedInfo := pe.resetLink(mt, "test_revoked")
	if rec := pe.post(passkeyRoute(auth.ResetLinkRevokePath, revokedInfo.UUID), mt, nil); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	leftToken, _ := pe.resetLink(mt, "test_left")
	left := pe.memberByLogin("test_left")
	left.Set("left_at", types.NowDateTime())
	if err := pe.app.Save(left); err != nil {
		t.Fatal(err)
	}
	replaced, _ := pe.resetLink(mt, "test_used")
	pe.resetLink(mt, "test_used") // replaces the previous one
	if _, err := pe.app.DB().Update(store.ResetLinks, dbx.Params{"expires_at": "2020-01-01 00:00:00.000Z"},
		dbx.HashExp{"code_hash": auth.HashToken(expired)}).Execute(); err != nil {
		t.Fatal(err)
	}

	tokens := map[string]string{"unknown": "dGVzdC10b2tlbi10aGF0LWRvZXMtbm90LWV4aXN0LWF0LWFsbA", "malformed": "x",
		"used": used, "expired": expired, "revoked": revoked, "left": leftToken, "replaced": replaced}
	type route struct {
		path string
		body func(token string) any
	}
	n := 0
	for _, r := range []route{
		{auth.ResetCheckPath, func(tok string) any { return auth.ResetTokenRequest{Token: tok} }},
		{auth.ResetPasswordStartPath, func(tok string) any {
			return auth.ResetPasswordStartRequest{Token: tok, Password: testPassword}
		}},
		{auth.ResetPasskeyOptionsPath, func(tok string) any { return auth.ResetTokenRequest{Token: tok} }},
	} {
		var first *httptest.ResponseRecorder
		for _, what := range slices.Sorted(maps.Keys(tokens)) {
			n++ // every invalid token is a failed attempt: a client of its own each, below the client lockout
			rec := pe.postFrom(client(100+n), r.path, r.body(tokens[what]))
			if first == nil {
				first = rec
				if r.path == auth.ResetCheckPath && rec.Body.String() != `{"valid":false}` {
					t.Errorf("check: %s", rec.Body.String())
				}
				continue
			}
			if rec.Code != first.Code || rec.Body.String() != first.Body.String() ||
				!maps.EqualFunc(rec.Header(), first.Header(), slices.Equal) {
				t.Errorf("%s, %s: the response differs:\n%d %s\n%d %s", r.path, what, rec.Code, rec.Body, first.Code,
					first.Body)
			}
		}
	}
}

// TestResetLinkRules: only maintainers create links, with a fresh session, not for themselves nor for members who
// left; a new link replaces the member's pending one; a used link cannot be revoked; other maintainers can be reset.
// Invalid tokens count as failures for the rate limit.
func TestResetLinkRules(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	other := pe.member("test_other_maintainer", auth.RoleMaintainer)
	mt := pe.session(maintainer)
	memberToken, _, _ := pe.passkeyMember(maintainer, newLogin)

	if rec := pe.post(auth.ResetLinksPath, memberToken, pe.resetFor(newLogin)); rec.Code != http.StatusForbidden {
		t.Errorf("a member creates a link: %d", rec.Code)
	}
	if rec := pe.reqJSON(http.MethodGet, auth.ResetLinksPath, memberToken, ""); rec.Code != http.StatusForbidden {
		t.Errorf("a member lists links: %d", rec.Code)
	}
	expectError(t, "for oneself", pe.post(auth.ResetLinksPath, mt, pe.resetFor("test_maintainer")),
		http.StatusConflict, auth.CodeResetSelf)
	expectError(t, "unknown member", pe.post(auth.ResetLinksPath, mt, auth.CreateResetLinkRequest{
		Member: "0190a1b2-0000-7000-8000-00000000ffff"}), http.StatusNotFound, auth.CodeMemberNotFound)
	expectError(t, "not a uuid", pe.post(auth.ResetLinksPath, mt, auth.CreateResetLinkRequest{Member: "test_nobody"}),
		http.StatusNotFound, auth.CodeMemberNotFound)
	expectError(t, "by login name", pe.post(auth.ResetLinksPath, mt, map[string]string{"loginName": newLogin}),
		http.StatusNotFound, auth.CodeMemberNotFound)
	pe.resetLink(mt, "test_other_maintainer")
	_ = other

	first, firstInfo := pe.resetLink(mt, newLogin)
	second, _ := pe.resetLink(mt, newLogin)
	if c := decode[auth.ResetCheckResponse](t, pe.post(auth.ResetCheckPath, "", auth.ResetTokenRequest{Token: first})); c.Valid {
		t.Error("the replaced link is still valid")
	}
	if c := decode[auth.ResetCheckResponse](t, pe.post(auth.ResetCheckPath, "", auth.ResetTokenRequest{Token: second})); !c.Valid {
		t.Error("the new link is not valid")
	}
	if rec := pe.post(passkeyRoute(auth.ResetLinkRevokePath, firstInfo.UUID), mt, nil); rec.Code != http.StatusOK ||
		decode[auth.ResetLinkResponse](t, rec).ResetLink.State != auth.ResetLinkRevoked {
		t.Errorf("revoke a replaced link: %d %s", rec.Code, rec.Body.String())
	}
	if rec := pe.resetPasskey(second, &passkeytest.Authenticator{}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	list := decode[auth.ResetLinkList](t, pe.reqJSON(http.MethodGet, auth.ResetLinksPath, mt, ""))
	var usedUUID string
	for _, l := range list.ResetLinks {
		if l.State == auth.ResetLinkUsed {
			usedUUID = l.UUID
		}
	}
	expectError(t, "revoke a used link", pe.post(passkeyRoute(auth.ResetLinkRevokePath, usedUUID), mt, nil),
		http.StatusConflict, auth.CodeResetLinkUsed)

	// A member who left gets no link.
	pe.passkeyMember(maintainer, "test_left")
	left := pe.memberByLogin("test_left")
	left.Set("left_at", types.NowDateTime())
	if err := pe.app.Save(left); err != nil {
		t.Fatal(err)
	}
	expectError(t, "a member who left", pe.post(auth.ResetLinksPath, mt, pe.resetFor("test_left")),
		http.StatusNotFound, auth.CodeMemberNotFound)

	// Creating a link needs a fresh session.
	pe.stale()
	expectError(t, "a stale maintainer", pe.post(auth.ResetLinksPath, mt, pe.resetFor(newLogin)),
		http.StatusForbidden, auth.CodeReauthRequired)
}

// TestResetLimits: invalid tokens count as failed attempts: a client that tries too many is locked out.
func TestResetLimits(t *testing.T) {
	pe := newPWEnv(t)
	threshold := auth.DefaultLimits().ClientFailures.Threshold
	for range threshold {
		pe.postFrom(client(50), auth.ResetCheckPath, auth.ResetTokenRequest{Token: "x"})
	}
	expectLimited(t, "after the failures", pe.postFrom(client(50), auth.ResetCheckPath,
		auth.ResetTokenRequest{Token: "x"}), "60")
}

// TestConcurrentResets: of two resets with the same link at once, one succeeds.
func TestConcurrentResets(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	pe.passkeyMember(maintainer, newLogin)
	token, _ := pe.resetLink(pe.session(maintainer), newLogin)
	type prepared struct{ state, cred string }
	var ps []prepared
	for range 2 {
		o := decode[optionsResponse](t, pe.post(auth.ResetPasskeyOptionsPath, "", auth.ResetTokenRequest{Token: token}))
		cred, err := (&passkeytest.Authenticator{}).Create(o.Options)
		if err != nil {
			t.Fatal(err)
		}
		ps = append(ps, prepared{o.State, string(cred)})
	}
	var wg sync.WaitGroup
	statuses := make([]int, len(ps))
	for i, p := range ps {
		wg.Go(func() {
			statuses[i] = pe.reqJSON(http.MethodPost, auth.ResetPasskeyPath, "", jsonBody(t, map[string]any{
				"token": token, "state": p.state, "credential": json.RawMessage(p.cred)})).Code
		})
	}
	wg.Wait()
	if slices.Index(statuses, http.StatusCreated) < 0 || slices.Contains(statuses[slices.Index(statuses, http.StatusCreated)+1:], http.StatusCreated) {
		t.Errorf("statuses %v", statuses)
	}
	if n := len(memberCredentials(t, pe.app, pe.memberByLogin(newLogin))); n != 1 {
		t.Errorf("%d credentials", n)
	}
}
