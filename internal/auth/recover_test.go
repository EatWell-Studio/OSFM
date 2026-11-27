package auth_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/auth/passkeytest"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// recoverTicket recovers login with code and returns the ticket; it fails the test unless that works.
func (pe *pwEnv) recoverTicket(login, code string) string {
	pe.t.Helper()
	rec := pe.post(auth.RecoverPath, "", auth.RecoverRequest{LoginName: login, Code: code})
	if rec.Code != http.StatusOK {
		pe.t.Fatalf("recover: %d %s", rec.Code, rec.Body.String())
	}
	if responseCookie(rec) != nil {
		pe.t.Fatal("the recovery code started a session")
	}
	return decode[auth.RecoverTicketResponse](pe.t, rec).Ticket
}

// recoverPasskey enrols a new passkey of a with the ticket.
func (pe *pwEnv) recoverPasskey(ticket string, a *passkeytest.Authenticator) *httptest.ResponseRecorder {
	pe.t.Helper()
	rec := pe.post(auth.RecoverPasskeyOptionsPath, "", auth.RecoverPasskeyOptionsRequest{Ticket: ticket})
	if rec.Code != http.StatusOK {
		return rec
	}
	o := decode[optionsResponse](pe.t, rec)
	cred, err := a.Create(o.Options)
	if err != nil {
		pe.t.Fatal(err)
	}
	return pe.post(auth.RecoverPasskeyPath, "", map[string]any{"ticket": ticket, "state": o.State,
		"credential": json.RawMessage(cred)})
}

// TestRecoveryWithPasskey: a passkey-only member who lost the passkey recovers with the login name and a recovery
// code. The ticket is no session; only after enrolling a new passkey is the member signed in, with new recovery
// codes, every other session ended, and the old passkey kept.
func TestRecoveryWithPasskey(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	oldToken, old, codes := pe.passkeyMember(maintainer, newLogin)
	member := pe.memberByLogin(newLogin)

	ticket := pe.recoverTicket(" Test_New ", codes[0])
	// The ticket opens nothing else: no cookie was set, so every other route is closed, and it is not a sign-in
	// ticket or a reset token either.
	for _, path := range []string{auth.MePath, auth.MeCredentialsPath} {
		if rec := pe.reqJSON(http.MethodGet, path, "", jsonBody(t, map[string]string{"ticket": ticket})); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s with the ticket: %d", path, rec.Code)
		}
	}
	if rec := pe.post(auth.MeRecoveryCodesPath, "", map[string]string{"ticket": ticket}); rec.Code != http.StatusUnauthorized {
		t.Errorf("regenerate with the ticket: %d", rec.Code)
	}
	expectError(t, "the ticket as a sign-in ticket", pe.post(auth.SignInTOTPPath, "",
		auth.SignInCodeRequest{Ticket: ticket, Code: "123456"}), http.StatusUnauthorized, auth.CodeTicketExpired)
	expectError(t, "the ticket as a reset token", pe.post(auth.ResetPasskeyOptionsPath, "",
		auth.ResetTokenRequest{Token: ticket}), http.StatusForbidden, auth.CodeResetInvalid)
	// The code is used up.
	expectError(t, "the same code again", pe.post(auth.RecoverPath, "",
		auth.RecoverRequest{LoginName: newLogin, Code: codes[0]}), http.StatusUnauthorized, auth.CodeRecoveryFailed)

	fresh := &passkeytest.Authenticator{}
	rec := pe.recoverPasskey(ticket, fresh)
	if rec.Code != http.StatusCreated {
		t.Fatalf("enrol: %d %s", rec.Code, rec.Body.String())
	}
	token := sessionToken(t, rec)
	done := decode[auth.EnrolledResponse](t, rec)
	if done.Me.UUID != member.GetString(store.FieldUUID) || len(done.RecoveryCodes) != auth.RecoveryCodeCount ||
		done.OtherPasskeys != 1 {
		t.Errorf("response %+v", done)
	}
	if pe.status(http.MethodGet, auth.MePath, token) != http.StatusOK {
		t.Error("no session after the enrolment")
	}
	if pe.status(http.MethodGet, auth.MePath, oldToken) != http.StatusUnauthorized {
		t.Error("an old session survived the recovery")
	}
	// The ticket enrols once.
	expectError(t, "the ticket again", pe.recoverPasskey(ticket, &passkeytest.Authenticator{}),
		http.StatusUnauthorized, auth.CodeRecoveryExpired)
	// Old recovery codes stop working, new ones work.
	expectError(t, "an old recovery code", pe.post(auth.RecoverPath, "",
		auth.RecoverRequest{LoginName: newLogin, Code: codes[1]}), http.StatusUnauthorized, auth.CodeRecoveryFailed)
	pe.recoverTicket(newLogin, done.RecoveryCodes[0])
	// Both passkeys sign in; the old one is listed and can be removed at once (the session is fresh).
	if rec := pe.passkeySignIn(fresh); rec.Code != http.StatusNoContent {
		t.Errorf("the new passkey: %d %s", rec.Code, rec.Body.String())
	}
	if rec := pe.passkeySignIn(old); rec.Code != http.StatusNoContent {
		t.Errorf("the old passkey: %d %s", rec.Code, rec.Body.String())
	}
	c := pe.credentials(token)
	if len(c.Passkeys) != 2 || c.VerifiedUntil == nil {
		t.Fatalf("credentials %+v", c)
	}
	if rec := pe.post(passkeyRoute(auth.MePasskeyRemovePath, c.Passkeys[0].UUID), token, nil); rec.Code != http.StatusOK {
		t.Errorf("remove the old passkey: %d %s", rec.Code, rec.Body.String())
	}

	// Audit: recovery started, credential added via the recovery, sessions ended, signed in.
	if a := pe.audits(auth.AuditRecoveryStart); len(a) != 2 || a[0].GetString("target_uuid") != member.GetString(store.FieldUUID) {
		t.Errorf("%d recovery audit entries", len(a))
	}
	added := pe.audits(auth.AuditCredentialAdd)
	if len(added) != 1 || details(t, added[0])["via"] != "recovery" {
		t.Errorf("credential audit entries %d", len(added))
	}
	revoked := pe.audits(auth.AuditRevokeSessions)
	if len(revoked) != 1 || details(t, revoked[0])["reason"] != auth.RevokeRecovery {
		t.Errorf("revoke audit entries %d", len(revoked))
	}
	signIns := pe.audits(auth.AuditSignIn)
	if len(signIns) == 0 || details(t, signIns[0])["method"] != auth.MethodRecovery {
		t.Errorf("sign-in audit entries %d", len(signIns))
	}
	checkNoSecretsInAudit(t, pe.app, codes[0], ticket)
}

// TestRecoveryWithPassword: the recovery can enrol password and TOTP instead; the member then signs in with them.
func TestRecoveryWithPassword(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	_, _, codes := pe.passkeyMember(maintainer, newLogin)
	ticket := pe.recoverTicket(newLogin, codes[3])

	rec := pe.post(auth.RecoverPasswordStartPath, "", auth.RecoverPasswordStartRequest{Ticket: ticket, Password: testPassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	start := decode[auth.RegisterPasswordStartResponse](t, rec)
	expectError(t, "a wrong code", pe.post(auth.RecoverPasswordConfirmPath, "", auth.RecoverPasswordConfirmRequest{
		Ticket: ticket, Enrolment: start.Enrolment, Code: wrongCode(pe.code(start.Secret, 0))}),
		http.StatusBadRequest, auth.CodeCodeInvalid)
	rec = pe.post(auth.RecoverPasswordConfirmPath, "", auth.RecoverPasswordConfirmRequest{
		Ticket: ticket, Enrolment: start.Enrolment, Code: pe.code(start.Secret, 0)})
	if rec.Code != http.StatusCreated || responseCookie(rec) == nil {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
	}
	done := decode[auth.EnrolledResponse](t, rec)
	if done.OtherPasskeys != 1 {
		t.Errorf("other passkeys %d", done.OtherPasskeys)
	}
	pe.advance(30 * time.Second)
	if rec := pe.post(auth.SignInTOTPPath, "", auth.SignInCodeRequest{Ticket: pe.ticket(newLogin, testPassword),
		Code: pe.code(start.Secret, 0)}); rec.Code != http.StatusOK {
		t.Errorf("sign in with the new password: %d %s", rec.Code, rec.Body.String())
	}
	// Now the member has a password: a valid code on this path says so.
	expectError(t, "a valid code with a password", pe.post(auth.RecoverPath, "",
		auth.RecoverRequest{LoginName: newLogin, Code: done.RecoveryCodes[0]}),
		http.StatusConflict, auth.CodeRecoveryUsePassword)
}

// TestRecoveryNoLeak: without a valid recovery code, nobody can tell an unknown login name from a member, a member
// with a password, a member who left, or a wrong code: status, body and headers are identical. A valid code of a
// member with a password says so, and is not used up.
func TestRecoveryNoLeak(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	_, _, passkeyCodes := pe.passkeyMember(maintainer, "test_passkey")
	_, _, pwCodes := pe.register(maintainer, "test_pw")
	_, _, leftCodes := pe.passkeyMember(maintainer, "test_left")
	left := pe.memberByLogin("test_left")
	left.Set("left_at", types.NowDateTime())
	if err := pe.app.Save(left); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ what, login, code string }{
		{"unknown login name", "test_nobody", passkeyCodes[0]},
		{"malformed login name", "x", passkeyCodes[0]},
		{"wrong code", "test_passkey", "0000-0000-0000-0000"},
		{"malformed code", "test_passkey", "x"},
		{"another member's code", "test_passkey", pwCodes[0]},
		{"member with a password, wrong code", "test_pw", "0000-0000-0000-0000"},
		{"member who left, valid code", "test_left", leftCodes[0]},
	}
	var first *httptest.ResponseRecorder
	for i, c := range cases {
		rec := pe.postFrom(client(20+i), auth.RecoverPath, auth.RecoverRequest{LoginName: c.login, Code: c.code})
		expectError(t, c.what, rec, http.StatusUnauthorized, auth.CodeRecoveryFailed)
		if first == nil {
			first = rec
			continue
		}
		if rec.Body.String() != first.Body.String() || !maps.EqualFunc(rec.Header(), first.Header(), slices.Equal) {
			t.Errorf("%s: the response differs:\n%v %s\n%v %s", c.what, rec.Header(), rec.Body, first.Header(), first.Body)
		}
	}
	// A valid code of a member with a password: told to sign in with the password; the code still works there.
	expectError(t, "member with a password, valid code", pe.post(auth.RecoverPath, "",
		auth.RecoverRequest{LoginName: "test_pw", Code: pwCodes[1]}), http.StatusConflict, auth.CodeRecoveryUsePassword)
	if rec := pe.post(auth.SignInRecoveryPath, "", auth.SignInCodeRequest{Ticket: pe.ticket("test_pw", testPassword),
		Code: pwCodes[1]}); rec.Code != http.StatusOK {
		t.Errorf("the code at sign-in: %d %s", rec.Code, rec.Body.String())
	}
	if n, _ := pe.app.CountRecords(store.RecoveryCodes, dbx.HashExp{"member": left.Id, "used_at": ""}); n != auth.RecoveryCodeCount {
		t.Errorf("the left member's codes: %d unused", n)
	}
	if len(pe.audits(auth.AuditRecoveryStart)) != 0 {
		t.Error("a failed recovery was audited as started")
	}
}

// TestRecoveryLimits: wrong recovery codes count as failures, per client and login name: after the threshold the
// login name is locked for that client (the same answer for an unknown one), not for others.
func TestRecoveryLimits(t *testing.T) {
	pe := newPWEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	_, _, codes := pe.passkeyMember(maintainer, newLogin)
	threshold := auth.DefaultLimits().LoginFailures.Threshold
	var locked []*httptest.ResponseRecorder
	for i, login := range []string{newLogin, "test_nobody"} {
		addr := client(30 + i)
		for n := range threshold {
			expectError(t, fmt.Sprintf("%s, attempt %d", login, n+1), pe.postFrom(addr, auth.RecoverPath,
				auth.RecoverRequest{LoginName: login, Code: "0000-0000-0000-0000"}),
				http.StatusUnauthorized, auth.CodeRecoveryFailed)
		}
		rec := pe.postFrom(addr, auth.RecoverPath, auth.RecoverRequest{LoginName: login, Code: codes[0]})
		expectLimited(t, login, rec, "60")
		locked = append(locked, rec)
	}
	if a, b := locked[0], locked[1]; a.Body.String() != b.Body.String() || !maps.EqualFunc(a.Header(), b.Header(), slices.Equal) {
		t.Error("the lockouts differ")
	}
	// Another client is not locked out, and a password-step lockout is separate.
	if rec := pe.postFrom(client(40), auth.RecoverPath, auth.RecoverRequest{LoginName: newLogin, Code: codes[0]}); rec.Code != http.StatusOK {
		t.Errorf("another client: %d %s", rec.Code, rec.Body.String())
	}
}

// memberCredentials returns the member's credential records.
func memberCredentials(t *testing.T, app core.App, member *core.Record) []*core.Record {
	t.Helper()
	rows, err := app.FindAllRecords(store.Credentials, dbx.HashExp{"member": member.Id})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
