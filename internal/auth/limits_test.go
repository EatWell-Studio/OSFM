package auth_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/auth/passkeytest"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// client returns a made-up client address (documentation range) numbered n.
func client(n int) string { return fmt.Sprintf("192.0.2.%d:40000", n) }

// postFrom sends a JSON POST with the X-OSFM header from the client address addr; headers are name, value pairs.
func (pe *pwEnv) postFrom(addr, path string, body any, headers ...string) *httptest.ResponseRecorder {
	pe.t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		pe.t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(b)))
	r.RemoteAddr = addr
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(csrf, "1")
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	pe.h.ServeHTTP(rec, r)
	pe.responses = append(pe.responses, rec)
	return rec
}

// expectLimited checks a 429 rate_limited response with the given Retry-After.
func expectLimited(t *testing.T, what string, rec *httptest.ResponseRecorder, retryAfter string) {
	t.Helper()
	expectError(t, what, rec, http.StatusTooManyRequests, auth.CodeRateLimited)
	if got := rec.Header().Get("Retry-After"); got != retryAfter {
		t.Errorf("%s: Retry-After %q, want %q", what, got, retryAfter)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("%s: the 429 may be cached", what)
	}
}

// audits returns the audit entries with action, oldest first.
func (pe *pwEnv) audits(action string) []*core.Record {
	pe.t.Helper()
	rows, err := pe.app.FindRecordsByFilter(store.AuditLog, "action = {:a}", "created", 0, 0, dbx.Params{"a": action})
	if err != nil {
		pe.t.Fatal(err)
	}
	return rows
}

func details(t *testing.T, r *core.Record) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal([]byte(r.GetString("details")), &d); err != nil {
		t.Fatalf("details %q: %v", r.GetString("details"), err)
	}
	return d
}

// TestLockoutSameForUnknownLoginNames: after the threshold of wrong passwords, an existing and an unknown login name
// get the same 429, byte for byte, and no lookup happens before it. The lockout is per client: the member can still
// sign in from elsewhere.
func TestLockoutSameForUnknownLoginNames(t *testing.T) {
	pe := newPWEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	pe.register(maintainer, newLogin)
	threshold := auth.DefaultLimits().LoginFailures.Threshold

	var locked []*httptest.ResponseRecorder
	for i, login := range []string{newLogin, "test_nobody"} {
		addr := client(10 + i)
		for n := range threshold {
			expectError(t, fmt.Sprintf("%s, attempt %d", login, n+1), pe.postFrom(addr, auth.SignInPasswordPath,
				auth.SignInPasswordRequest{LoginName: login, Password: "wrong password, made up"}),
				http.StatusUnauthorized, auth.CodeCredentialsInvalid)
		}
		// Over the threshold, even the right password is refused from this client.
		rec := pe.postFrom(addr, auth.SignInPasswordPath,
			auth.SignInPasswordRequest{LoginName: strings.ToUpper(login), Password: testPassword})
		expectLimited(t, login, rec, "60")
		locked = append(locked, rec)
	}
	if a, b := locked[0], locked[1]; a.Code != b.Code || a.Body.String() != b.Body.String() ||
		!maps.EqualFunc(a.Header(), b.Header(), slices.Equal) {
		t.Errorf("the lockout differs:\n%d %v %s\n%d %v %s", a.Code, a.Header(), a.Body, b.Code, b.Header(), b.Body)
	}

	// The member is not locked out from another client.
	if rec := pe.postFrom(client(12), auth.SignInPasswordPath,
		auth.SignInPasswordRequest{LoginName: newLogin, Password: testPassword}); rec.Code != http.StatusOK {
		t.Errorf("sign-in from another client: %d %s", rec.Code, rec.Body.String())
	}
	// The lockout ends: one more attempt after a minute, then two minutes.
	pe.advance(time.Minute)
	expectError(t, "after the lockout", pe.postFrom(client(10), auth.SignInPasswordPath,
		auth.SignInPasswordRequest{LoginName: newLogin, Password: "wrong password, made up"}),
		http.StatusUnauthorized, auth.CodeCredentialsInvalid)
	expectLimited(t, "second lockout", pe.postFrom(client(10), auth.SignInPasswordPath,
		auth.SignInPasswordRequest{LoginName: newLogin, Password: testPassword}), "120")
}

// TestAccountLockoutAcrossClients: failures for one login name from many clients lock it everywhere, the same way for
// an existing and an unknown name; only the existing member's lockout is audited, and passkey sign-in stays open.
func TestAccountLockoutAcrossClients(t *testing.T) {
	pe := newPWEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	pe.register(maintainer, newLogin)
	limits := auth.DefaultLimits()
	perClient := limits.LoginFailures.Threshold - 1

	var locked []*httptest.ResponseRecorder
	for i, login := range []string{newLogin, "test_nobody"} {
		failures := 0
		for c := 0; failures < limits.AccountFailures.Threshold; c++ {
			addr := client(100*i + c + 1)
			for n := 0; n < perClient && failures < limits.AccountFailures.Threshold; n++ {
				expectError(t, login, pe.postFrom(addr, auth.SignInPasswordPath,
					auth.SignInPasswordRequest{LoginName: login, Password: "wrong password, made up"}),
					http.StatusUnauthorized, auth.CodeCredentialsInvalid)
				failures++
			}
		}
		rec := pe.postFrom(client(99+100*i), auth.SignInPasswordPath,
			auth.SignInPasswordRequest{LoginName: login, Password: testPassword})
		expectLimited(t, login+" from a new client", rec, "60")
		locked = append(locked, rec)
	}
	if a, b := locked[0], locked[1]; a.Body.String() != b.Body.String() ||
		!maps.EqualFunc(a.Header(), b.Header(), slices.Equal) {
		t.Errorf("the lockout differs: %v %s / %v %s", a.Header(), a.Body, b.Header(), b.Body)
	}

	rows := pe.audits(auth.AuditSignInLocked)
	if len(rows) != 1 {
		t.Fatalf("%d lockout audit entries, want 1 (the existing member's)", len(rows))
	}
	m := pe.memberByLogin(newLogin)
	if d := details(t, rows[0]); rows[0].GetString("target_uuid") != m.GetString(store.FieldUUID) ||
		d["factor"] != "password" || d["seconds"] != float64(60) {
		t.Errorf("lockout entry %s %v", rows[0].GetString("target_uuid"), d)
	}

	// Passkey sign-in has no login name and is not affected.
	if rec := pe.postFrom(client(250), auth.PasskeySignInOptionsPath, nil); rec.Code != http.StatusOK {
		t.Errorf("passkey options during the password lockout: %d", rec.Code)
	}
}

// TestTOTPLockoutWithinOneTicket: a ticket cannot be retried with codes beyond the per-member threshold, a new ticket
// or another client does not help, the recovery codes still work, and the attempts are audited.
func TestTOTPLockoutWithinOneTicket(t *testing.T) {
	pe := newPWEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	_, secret, codes := pe.register(maintainer, newLogin)
	pe.advance(30 * time.Second)
	threshold := auth.DefaultLimits().TOTPFailures.Threshold

	ticket := pe.ticket(newLogin, testPassword)
	right := pe.code(secret, 0)
	for n := range threshold {
		wrong := fmt.Sprintf("%06d", n)
		if wrong == right {
			wrong = "999999"
		}
		expectError(t, fmt.Sprintf("wrong code %d", n+1), pe.postFrom(client(n+1), auth.SignInTOTPPath,
			auth.SignInCodeRequest{Ticket: ticket, Code: wrong}), http.StatusUnauthorized, auth.CodeCodeInvalid)
	}
	expectLimited(t, "right code after the threshold", pe.postFrom(client(20), auth.SignInTOTPPath,
		auth.SignInCodeRequest{Ticket: ticket, Code: right}), "60")
	expectLimited(t, "new ticket", pe.postFrom(client(21), auth.SignInTOTPPath,
		auth.SignInCodeRequest{Ticket: pe.ticket(newLogin, testPassword), Code: right}), "60")

	m := pe.memberByLogin(newLogin)
	uuid := m.GetString(store.FieldUUID)
	failed := pe.audits(auth.AuditSignInFailed)
	if len(failed) != threshold {
		t.Errorf("%d failure entries, want %d", len(failed), threshold)
	}
	for _, r := range failed {
		if r.GetString("target_uuid") != uuid || details(t, r)["factor"] != "totp" || r.GetString("actor") != "" {
			t.Errorf("failure entry %v", r.PublicExport())
		}
	}
	locks := pe.audits(auth.AuditSignInLocked)
	if len(locks) != 1 || locks[0].GetString("target_uuid") != uuid {
		t.Fatalf("lockout entries %v", locks)
	}
	if d := details(t, locks[0]); d["factor"] != "totp" || d["failures"] != float64(threshold) ||
		d["seconds"] != float64(60) {
		t.Errorf("lockout details %v", d)
	}

	// Recovery codes are counted apart: they cannot be guessed.
	rec := pe.postFrom(client(22), auth.SignInRecoveryPath, auth.SignInCodeRequest{Ticket: ticket, Code: codes[0]})
	if rec.Code != http.StatusOK {
		t.Fatalf("recovery code during the TOTP lockout: %d %s", rec.Code, rec.Body.String())
	}

	// After the lockout, one attempt; the right code signs in and forgets the failures.
	pe.advance(time.Minute)
	ticket = pe.ticket(newLogin, testPassword)
	if rec := pe.postFrom(client(23), auth.SignInTOTPPath,
		auth.SignInCodeRequest{Ticket: ticket, Code: pe.code(secret, 0)}); rec.Code != http.StatusOK {
		t.Fatalf("after the lockout: %d %s", rec.Code, rec.Body.String())
	}
	pe.advance(30 * time.Second)
	for n := range threshold - 1 {
		expectError(t, "after the success", pe.postFrom(client(24), auth.SignInTOTPPath,
			auth.SignInCodeRequest{Ticket: ticket, Code: fmt.Sprintf("x%05d", n)}), http.StatusUnauthorized,
			auth.CodeCodeInvalid)
	}
}

// TestConcurrentGuessesCannotRace sends many wrong codes with one ticket at once, each from its own client: no more
// than the threshold reach the code check.
func TestConcurrentGuessesCannotRace(t *testing.T) {
	pe := newPWEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	pe.register(maintainer, newLogin)
	ticket := pe.ticket(newLogin, testPassword)
	threshold := auth.DefaultLimits().TOTPFailures.Threshold

	const n = 40
	statuses := make([]int, n)
	reqs := make([]*http.Request, n)
	for i := range n {
		body, _ := json.Marshal(auth.SignInCodeRequest{Ticket: ticket, Code: fmt.Sprintf("x%05d", i)})
		r := httptest.NewRequest(http.MethodPost, auth.SignInTOTPPath, strings.NewReader(string(body)))
		r.RemoteAddr = client(i + 1)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(csrf, "1")
		reqs[i] = r
	}
	var start, wg sync.WaitGroup
	start.Add(1)
	for i := range n {
		wg.Go(func() {
			start.Wait()
			rec := httptest.NewRecorder()
			pe.h.ServeHTTP(rec, reqs[i])
			statuses[i] = rec.Code
		})
	}
	start.Done()
	wg.Wait()
	checked := 0
	for _, s := range statuses {
		switch s {
		case http.StatusUnauthorized:
			checked++
		case http.StatusTooManyRequests:
		default:
			t.Errorf("status %d", s)
		}
	}
	if checked > threshold {
		t.Fatalf("%d of %d concurrent guesses reached the code check, want at most %d", checked, n, threshold)
	}
	if len(pe.audits(auth.AuditSignInFailed)) != checked {
		t.Errorf("failure entries do not match the %d failures", checked)
	}
}

// TestRequestBudgetAndClientFailures: every client has a budget of requests to the limited routes, and its failures
// on any of them lock it.
func TestRequestBudgetAndClientFailures(t *testing.T) {
	pe := newPWEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	invite, _ := pe.invite(maintainer)
	limits := auth.DefaultLimits()

	for n := range limits.Requests.Burst {
		if rec := pe.postFrom(client(1), auth.InviteCheckPath, auth.CheckInviteRequest{Code: invite}); rec.Code != http.StatusOK {
			t.Fatalf("request %d: %d", n+1, rec.Code)
		}
	}
	expectLimited(t, "over the budget", pe.postFrom(client(1), auth.InviteCheckPath,
		auth.CheckInviteRequest{Code: invite}), "10")
	pe.advance(limits.Requests.Every)
	if rec := pe.postFrom(client(1), auth.InviteCheckPath, auth.CheckInviteRequest{Code: invite}); rec.Code != http.StatusOK {
		t.Fatalf("after refilling: %d", rec.Code)
	}

	// Invalid invite codes, wrong passwords and bad tickets all count for the client.
	for n := range limits.ClientFailures.Threshold {
		var rec *httptest.ResponseRecorder
		switch n % 3 {
		case 0:
			rec = pe.postFrom(client(2), auth.InviteCheckPath, auth.CheckInviteRequest{Code: "x"})
		case 1:
			rec = pe.postFrom(client(2), auth.SignInPasswordPath,
				auth.SignInPasswordRequest{LoginName: fmt.Sprintf("test_n%d", n), Password: "made up"})
		default:
			rec = pe.postFrom(client(2), auth.SignInTOTPPath, auth.SignInCodeRequest{Ticket: "x", Code: "123456"})
		}
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("failure %d: 429 before the threshold", n+1)
		}
	}
	expectLimited(t, "client locked", pe.postFrom(client(2), auth.InviteCheckPath,
		auth.CheckInviteRequest{Code: invite}), "60")
	expectLimited(t, "client locked, passkey", pe.postFrom(client(2), auth.PasskeySignInOptionsPath, nil), "60")
	// Other clients are not affected.
	if rec := pe.postFrom(client(3), auth.InviteCheckPath, auth.CheckInviteRequest{Code: invite}); rec.Code != http.StatusOK {
		t.Errorf("another client: %d", rec.Code)
	}
}

// TestClientAddress: with a trusted proxy header the client is the header's rightmost address; an IPv6 /64 is one
// client; without the setting the header is ignored.
func TestClientAddress(t *testing.T) {
	budget := func(pe *pwEnv, addr string, headers ...string) int {
		t.Helper()
		n := 0
		for range 100 {
			if pe.postFrom(addr, auth.PasskeySignInOptionsPath, nil, headers...).Code != http.StatusOK {
				break
			}
			n++
		}
		return n
	}
	burst := auth.DefaultLimits().Requests.Burst

	pe := newPWEnvWith(t, nil, func(app core.App) {
		s := app.Settings()
		s.TrustedProxy.Headers = []string{"X-Forwarded-For"}
		if err := app.Save(s); err != nil {
			t.Fatal(err)
		}
	})
	if n := budget(pe, "127.0.0.1:1", "X-Forwarded-For", "203.0.113.9, 198.51.100.1"); n != burst {
		t.Errorf("first client through the proxy: %d requests, want %d", n, burst)
	}
	if n := budget(pe, "127.0.0.1:1", "X-Forwarded-For", "203.0.113.9, 198.51.100.2"); n != burst {
		t.Errorf("second client through the proxy: %d requests, want %d", n, burst)
	}
	if n := budget(pe, "127.0.0.1:1", "X-Forwarded-For", "198.51.100.3, 198.51.100.1"); n != 0 {
		t.Errorf("a spoofed leftmost address made a new client: %d requests", n)
	}
	if n := budget(pe, "127.0.0.1:1", "X-Forwarded-For", "2001:db8:1:2::a"); n != burst {
		t.Errorf("IPv6 client: %d requests, want %d", n, burst)
	}
	if n := budget(pe, "127.0.0.1:1", "X-Forwarded-For", "2001:db8:1:2:ffff::b"); n != 0 {
		t.Errorf("another address in the same /64: %d requests, want 0", n)
	}

	pe = newPWEnv(t)
	if n := budget(pe, "127.0.0.1:1", "X-Forwarded-For", "203.0.113.9"); n != burst {
		t.Fatalf("%d requests", n)
	}
	if n := budget(pe, "127.0.0.1:1", "X-Forwarded-For", "203.0.113.10"); n != 0 {
		t.Errorf("an untrusted header made a new client: %d requests", n)
	}
}

// TestAuditEvents: registration, sign-in with each method, recovery-code use and sign-out are audited, and no audit
// entry contains a password, code, secret, ticket, invite code, login name or address.
func TestAuditEvents(t *testing.T) {
	pe := newPWEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	invite, inviteRecord := pe.invite(maintainer)
	start := decode[auth.RegisterPasswordStartResponse](t, pe.postFrom(client(1), auth.RegisterPasswordStartPath,
		auth.RegisterPasswordStartRequest{Invite: invite, LoginName: newLogin, Password: testPassword}))
	regCode := pe.code(start.Secret, 0)
	rec := pe.postFrom(client(1), auth.RegisterPasswordConfirmPath,
		auth.RegisterPasswordConfirmRequest{Invite: invite, Enrolment: start.Enrolment, Code: regCode})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	codes := decode[auth.RegisteredResponse](t, rec).RecoveryCodes
	m := pe.memberByLogin(newLogin)
	uuid := m.GetString(store.FieldUUID)

	pe.advance(30 * time.Second)
	ticket := pe.ticket(newLogin, testPassword)
	totpCode := pe.code(start.Secret, 0)
	pe.postFrom(client(1), auth.SignInTOTPPath, auth.SignInCodeRequest{Ticket: ticket, Code: "x00000"})
	rec = pe.postFrom(client(1), auth.SignInTOTPPath, auth.SignInCodeRequest{Ticket: ticket, Code: totpCode})
	if rec.Code != http.StatusOK {
		t.Fatalf("TOTP sign-in: %d", rec.Code)
	}
	token := responseCookie(rec).Value
	rec = pe.postFrom(client(1), auth.SignInRecoveryPath, auth.SignInCodeRequest{Ticket: ticket, Code: codes[0]})
	if rec.Code != http.StatusOK {
		t.Fatalf("recovery sign-in: %d", rec.Code)
	}
	if rec := pe.reqJSON(http.MethodPost, auth.LogoutPath, token, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}

	// Passkey registration and sign-in.
	pkInvite, pkInviteRecord := pe.invite(maintainer)
	a := &passkeytest.Authenticator{}
	pe.registerPasskey(a, pkInvite, "test_passkey")
	if rec := pe.passkeySignIn(a); rec.Code != http.StatusNoContent {
		t.Fatalf("passkey sign-in: %d", rec.Code)
	}
	pk := pe.memberByLogin("test_passkey")

	type want struct {
		action, actor, target string
		details               map[string]any
	}
	for _, w := range []want{
		{auth.AuditRegister, m.Id, uuid, map[string]any{"method": auth.MethodPasswordTOTP,
			"invite": inviteRecord.GetString(store.FieldUUID)}},
		{auth.AuditRegister, pk.Id, pk.GetString(store.FieldUUID), map[string]any{"method": auth.MethodPasskey,
			"invite": pkInviteRecord.GetString(store.FieldUUID)}},
		{auth.AuditSignIn, m.Id, uuid, map[string]any{"method": auth.MethodPasswordTOTP}},
		{auth.AuditSignIn, m.Id, uuid, map[string]any{"method": auth.MethodRecoveryCode,
			"recoveryCodesLeft": float64(auth.RecoveryCodeCount - 1)}},
		{auth.AuditSignIn, pk.Id, pk.GetString(store.FieldUUID), map[string]any{"method": auth.MethodPasskey}},
		{auth.AuditSignOut, m.Id, uuid, nil},
		{auth.AuditSignInFailed, "", uuid, map[string]any{"factor": "totp"}},
	} {
		found := false
		for _, r := range pe.audits(w.action) {
			if r.GetString("actor") != w.actor || r.GetString("target_uuid") != w.target ||
				r.GetString("target_type") != store.Members {
				continue
			}
			var d map[string]any
			_ = json.Unmarshal([]byte(r.GetString("details")), &d)
			if maps.EqualFunc(d, w.details, func(a, b any) bool { return a == b }) {
				found = true
			}
		}
		if !found {
			t.Errorf("no audit entry %s %v", w.action, w.details)
		}
	}

	all, err := pe.app.FindAllRecords(store.AuditLog)
	if err != nil {
		t.Fatal(err)
	}
	secrets := slices.Concat([]string{testPassword, start.Secret, start.Enrolment, ticket, regCode, totpCode, invite,
		pkInvite, token, newLogin, "test_passkey", "192.0.2."}, codes)
	for _, r := range all {
		b, _ := json.Marshal(r.PublicExport())
		for _, s := range secrets {
			if strings.Contains(string(b), s) {
				t.Errorf("audit entry %s contains %q", r.GetString("action"), s)
			}
		}
	}
}
