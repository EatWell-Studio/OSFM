package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/auth/passkeytest"
	"github.com/EatWell-Studio/OSFM/internal/ratelimit"
	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// newCredEnv is a password and passkey test app whose request budget does not get in the way: these tests send many
// requests from one address. The failure counters are the defaults.
func newCredEnv(t *testing.T) *pwEnv {
	t.Helper()
	limits := auth.DefaultLimits()
	limits.Requests = ratelimit.Rate{Burst: 10_000, Every: time.Millisecond}
	return newPWEnvWith(t, &limits)
}

// stale moves the clock past the re-verification window of every session.
func (pe *pwEnv) stale() { pe.advance(auth.StepUpWindow + time.Second) }

// passkeyMember registers login with a passkey of a new authenticator and returns the session token, the
// authenticator and the recovery codes.
func (pe *pwEnv) passkeyMember(inviter *core.Record, login string) (string, *passkeytest.Authenticator, []string) {
	pe.t.Helper()
	invite, _ := pe.invite(inviter)
	a := &passkeytest.Authenticator{}
	rec := pe.registerPasskey(a, invite, login)
	return sessionToken(pe.t, rec), a, decode[auth.PasskeyRegisterResponse](pe.t, rec).RecoveryCodes
}

// credentials returns the member's credentials as the "me" page sees them.
func (pe *pwEnv) credentials(token string) auth.CredentialsResponse {
	pe.t.Helper()
	rec := pe.reqJSON(http.MethodGet, auth.MeCredentialsPath, token, "")
	if rec.Code != http.StatusOK {
		pe.t.Fatalf("credentials: %d %s", rec.Code, rec.Body.String())
	}
	return decode[auth.CredentialsResponse](pe.t, rec)
}

// addPasskey adds a passkey of a (a new credential on it) for the session's member.
func (pe *pwEnv) addPasskey(token string, a *passkeytest.Authenticator, name string) *httptest.ResponseRecorder {
	pe.t.Helper()
	rec := pe.post(auth.MePasskeyOptionsPath, token, nil)
	if rec.Code != http.StatusOK {
		return rec
	}
	o := decode[optionsResponse](pe.t, rec)
	cred, err := a.Create(o.Options)
	if err != nil {
		pe.t.Fatal(err)
	}
	return pe.post(auth.MePasskeysPath, token, map[string]any{"state": o.State, "credential": json.RawMessage(cred),
		"deviceName": name})
}

// assertWith signs an assertion for the options with one particular credential of a.
func assertWith(t *testing.T, a *passkeytest.Authenticator, cred *passkeytest.Credential, options []byte) []byte {
	t.Helper()
	var o struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &o); err != nil {
		t.Fatal(err)
	}
	b, err := a.Assert(cred, o.PublicKey.Challenge)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// signInWithCredential signs in with one particular passkey.
func (pe *pwEnv) signInWithCredential(a *passkeytest.Authenticator, cred *passkeytest.Credential) *httptest.ResponseRecorder {
	pe.t.Helper()
	o := pe.signInOptions()
	return pe.signInWith(o.State, assertWith(pe.t, a, cred, o.Options))
}

// TestStepUp: sensitive changes need a session verified within the window. Signing in counts; later the member
// re-verifies with the password and a TOTP or recovery code, on this session only.
func TestStepUp(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	token, secret, codes := pe.register(maintainer, newLogin)
	other := pe.session(pe.memberByLogin(newLogin))

	if c := pe.credentials(token); c.VerifiedUntil == nil || c.PasswordTOTP == nil || len(c.Passkeys) != 0 ||
		c.RecoveryCodesLeft != auth.RecoveryCodeCount {
		t.Fatalf("credentials after registering: %+v", c)
	}
	pe.stale()
	if c := pe.credentials(token); c.VerifiedUntil != nil {
		t.Errorf("verifiedUntil %v after the window", c.VerifiedUntil)
	}
	for _, path := range []string{auth.MeRecoveryCodesPath, auth.MeTOTPStartPath, auth.MePasskeyOptionsPath,
		auth.MePasswordRemovePath} {
		expectError(t, path+" when stale", pe.post(path, token, nil), http.StatusForbidden, auth.CodeReauthRequired)
	}
	expectError(t, "change the password when stale", pe.post(auth.MePasswordChangePath, token,
		auth.PasswordRequest{Password: "new test password"}), http.StatusForbidden, auth.CodeReauthRequired)

	// Wrong password, wrong code, then the right ones.
	expectError(t, "verify with a wrong password", pe.post(auth.MeVerifyPath, token,
		auth.VerifyRequest{Password: "wrong password, made up", Code: pe.code(secret, 0)}),
		http.StatusUnauthorized, auth.CodeCredentialsInvalid)
	expectError(t, "verify with a wrong code", pe.post(auth.MeVerifyPath, token,
		auth.VerifyRequest{Password: testPassword, Code: wrongCode(pe.code(secret, 0))}),
		http.StatusUnauthorized, auth.CodeCodeInvalid)
	rec := pe.post(auth.MeVerifyPath, token, auth.VerifyRequest{Password: testPassword, Code: pe.code(secret, 0)})
	if rec.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", rec.Code, rec.Body.String())
	}
	if v := decode[auth.VerifiedResponse](t, rec); !v.VerifiedUntil.Equal(pe.clock.Add(auth.StepUpWindow)) {
		t.Errorf("verifiedUntil %v", v.VerifiedUntil)
	}
	// The same TOTP code does not verify twice.
	pe.stale()
	expectError(t, "the same code again", pe.post(auth.MeVerifyPath, token,
		auth.VerifyRequest{Password: testPassword, Code: pe.code(secret, -auth.StepUpWindow-time.Second)}),
		http.StatusUnauthorized, auth.CodeCodeInvalid)
	// A recovery code verifies too, and is used up.
	if rec := pe.post(auth.MeVerifyPath, token, auth.VerifyRequest{Password: testPassword, Code: codes[0]}); rec.Code != http.StatusOK {
		t.Fatalf("verify with a recovery code: %d %s", rec.Code, rec.Body.String())
	}
	if c := pe.credentials(token); c.RecoveryCodesLeft != auth.RecoveryCodeCount-1 {
		t.Errorf("%d recovery codes left", c.RecoveryCodesLeft)
	}
	// The other session of the member stays stale.
	expectError(t, "the other session", pe.post(auth.MeRecoveryCodesPath, other, nil),
		http.StatusForbidden, auth.CodeReauthRequired)

	// Regenerating the recovery codes: shown once; the old ones stop working.
	rec = pe.post(auth.MeRecoveryCodesPath, token, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("regenerate: %d %s", rec.Code, rec.Body.String())
	}
	fresh := decode[auth.RecoveryCodesResponse](t, rec).RecoveryCodes
	if len(fresh) != auth.RecoveryCodeCount {
		t.Fatalf("%d new codes", len(fresh))
	}
	expectError(t, "an old recovery code", pe.post(auth.SignInRecoveryPath, "",
		auth.SignInCodeRequest{Ticket: pe.ticket(newLogin, testPassword), Code: codes[1]}),
		http.StatusUnauthorized, auth.CodeCodeInvalid)
	if rec := pe.post(auth.SignInRecoveryPath, "", auth.SignInCodeRequest{Ticket: pe.ticket(newLogin, testPassword),
		Code: fresh[0]}); rec.Code != http.StatusOK {
		t.Errorf("a new recovery code: %d %s", rec.Code, rec.Body.String())
	}
	if n := len(pe.audits(auth.AuditRecoveryCodesRegenerate)); n != 1 {
		t.Errorf("%d regenerate audit entries", n)
	}
	if n := len(pe.audits(auth.AuditVerify)); n != 2 {
		t.Errorf("%d verify audit entries", n)
	}

	// Without a session: 401.
	if rec := pe.post(auth.MeRecoveryCodesPath, "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("without a session: %d", rec.Code)
	}
}

func wrongCode(right string) string {
	if right == "000000" {
		return "111111"
	}
	return "000000"
}

// TestStepUpLockout: wrong codes in re-verifications count towards the member's second-step lockout, shared with the
// sign-in, so a session gives no extra guesses; a passkey-only member cannot use the password route.
func TestStepUpLockout(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	token, secret, _ := pe.register(maintainer, newLogin)
	threshold := auth.DefaultLimits().TOTPFailures.Threshold
	for n := range threshold - 1 {
		expectError(t, fmt.Sprintf("verify, attempt %d", n+1), pe.post(auth.MeVerifyPath, token,
			auth.VerifyRequest{Password: testPassword, Code: wrongCode(pe.code(secret, 0))}),
			http.StatusUnauthorized, auth.CodeCodeInvalid)
	}
	// The last failure before the lockout at sign-in.
	expectError(t, "sign-in", pe.post(auth.SignInTOTPPath, "", auth.SignInCodeRequest{
		Ticket: pe.ticket(newLogin, testPassword), Code: wrongCode(pe.code(secret, 0))}),
		http.StatusUnauthorized, auth.CodeCodeInvalid)
	expectLimited(t, "verify after the lockout", pe.post(auth.MeVerifyPath, token,
		auth.VerifyRequest{Password: testPassword, Code: pe.code(secret, 0)}), "60")
	if n := len(pe.audits(auth.AuditSignInLocked)); n != 1 {
		t.Errorf("%d lockout audit entries", n)
	}

	passkeyToken, _, _ := pe.passkeyMember(maintainer, "test_passkey")
	expectError(t, "a passkey-only member", pe.post(auth.MeVerifyPath, passkeyToken,
		auth.VerifyRequest{Password: testPassword, Code: "123456"}), http.StatusConflict, auth.CodeNoPassword)
}

// TestVerifyWithPasskey: a member re-verifies with one of their passkeys; another member's passkey does not do.
func TestVerifyWithPasskey(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	token, a, _ := pe.passkeyMember(maintainer, "test_one")
	_, b, _ := pe.passkeyMember(maintainer, "test_two")
	pe.stale()

	verify := func(auth_ *passkeytest.Authenticator, cred *passkeytest.Credential) *httptest.ResponseRecorder {
		rec := pe.post(auth.MeVerifyPasskeyOptionsPath, token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("options: %d %s", rec.Code, rec.Body.String())
		}
		o := decode[optionsResponse](t, rec)
		var opts struct {
			PublicKey struct {
				AllowCredentials []struct{ ID string } `json:"allowCredentials"`
				UserVerification string                `json:"userVerification"`
			} `json:"publicKey"`
		}
		if err := json.Unmarshal(o.Options, &opts); err != nil {
			t.Fatal(err)
		}
		if len(opts.PublicKey.AllowCredentials) != 1 || opts.PublicKey.UserVerification != "required" ||
			opts.PublicKey.AllowCredentials[0].ID != base64.RawURLEncoding.EncodeToString(a.Credentials[0].ID) {
			t.Errorf("options %s", o.Options)
		}
		return pe.post(auth.MeVerifyPasskeyPath, token, map[string]any{"state": o.State,
			"credential": json.RawMessage(assertWith(t, auth_, cred, o.Options))})
	}
	expectError(t, "another member's passkey", verify(b, b.Credentials[0]), http.StatusBadRequest,
		auth.CodePasskeyFailed)
	if rec := verify(a, a.Credentials[0]); rec.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", rec.Code, rec.Body.String())
	}
	c := pe.credentials(token)
	if c.VerifiedUntil == nil || len(c.Passkeys) != 1 || c.Passkeys[0].LastUsedAt == nil {
		t.Errorf("credentials %+v", c)
	}
}

// TestManagePasskeys: a passkey-only member adds a second passkey and removes the first; the last one cannot be
// removed, nor can a passkey of someone else.
func TestManagePasskeys(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	token, a, _ := pe.passkeyMember(maintainer, newLogin)
	member := pe.memberByLogin(newLogin)
	otherToken, _, _ := pe.passkeyMember(maintainer, "test_other")
	otherDevice := pe.session(member)

	c := pe.credentials(token)
	if len(c.Passkeys) != 1 || c.PasswordTOTP != nil {
		t.Fatalf("credentials %+v", c)
	}
	first := c.Passkeys[0].UUID
	expectError(t, "remove the only passkey", pe.post(passkeyRoute(auth.MePasskeyRemovePath, first), token, nil),
		http.StatusConflict, auth.CodeLastCredential)
	expectError(t, "remove someone else's passkey", pe.post(passkeyRoute(auth.MePasskeyRemovePath, first),
		otherToken, nil), http.StatusNotFound, auth.CodeCredentialNotFound)

	// Adding: the options exclude the passkey the member has and use their user handle.
	rec := pe.post(auth.MePasskeyOptionsPath, token, nil)
	o := decode[optionsResponse](t, rec)
	var opts struct {
		PublicKey struct {
			User    struct{ ID string }
			Exclude []struct{ ID string } `json:"excludeCredentials"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(o.Options, &opts); err != nil {
		t.Fatal(err)
	}
	if opts.PublicKey.User.ID != member.GetString("passkey_user_handle") || len(opts.PublicKey.Exclude) != 1 ||
		opts.PublicKey.Exclude[0].ID != base64.RawURLEncoding.EncodeToString(a.Credentials[0].ID) {
		t.Errorf("options %s", o.Options)
	}
	expectError(t, "a bad device name", pe.addPasskey(token, a, "bad\u0007name"), http.StatusBadRequest,
		auth.CodeDeviceNameInvalid)
	rec = pe.addPasskey(token, a, " Test Laptop ")
	if rec.Code != http.StatusCreated {
		t.Fatalf("add: %d %s", rec.Code, rec.Body.String())
	}
	c = decode[auth.CredentialsResponse](t, rec)
	if len(c.Passkeys) != 2 || c.Passkeys[1].DeviceName != "Test Laptop" {
		t.Fatalf("after adding: %+v", c)
	}
	second := c.Passkeys[1].UUID
	if rec := pe.post(passkeyRoute(auth.MePasskeyRenamePath, first), token,
		auth.RenamePasskeyRequest{DeviceName: "Test Phone"}); rec.Code != http.StatusOK ||
		decode[auth.CredentialsResponse](t, rec).Passkeys[0].DeviceName != "Test Phone" {
		t.Errorf("rename: %d %s", rec.Code, rec.Body.String())
	}

	// Removing the first passkey: it no longer signs in, the second does; the member's other session ended.
	if rec := pe.post(passkeyRoute(auth.MePasskeyRemovePath, first), token, nil); rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
	expectError(t, "sign in with the removed passkey", pe.signInWithCredential(a, a.Credentials[0]),
		http.StatusUnauthorized, auth.CodePasskeyFailed)
	// The authenticator also holds the credential of the refused attempt with a bad device name.
	if rec := pe.signInWithCredential(a, a.Credentials[len(a.Credentials)-1]); rec.Code != http.StatusNoContent {
		t.Errorf("sign in with the new passkey: %d %s", rec.Code, rec.Body.String())
	}
	if pe.status(http.MethodGet, auth.MePath, otherDevice) != http.StatusUnauthorized {
		t.Error("the member's other session survived removing a passkey")
	}
	if pe.status(http.MethodGet, auth.MePath, token) != http.StatusOK {
		t.Error("the current session ended")
	}
	expectError(t, "remove the remaining passkey", pe.post(passkeyRoute(auth.MePasskeyRemovePath, second), token, nil),
		http.StatusConflict, auth.CodeLastCredential)
	if n := len(pe.audits(auth.AuditCredentialAdd)); n != 1 {
		t.Errorf("%d add audit entries", n)
	}
	if n := len(pe.audits(auth.AuditCredentialRemove)); n != 1 {
		t.Errorf("%d remove audit entries", n)
	}
}

func passkeyRoute(pattern, uuid string) string { return strings.Replace(pattern, "{uuid}", uuid, 1) }

// TestConcurrentRemovals: of two removals that would together remove the last passkey, one fails.
func TestConcurrentRemovals(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	token, a, _ := pe.passkeyMember(maintainer, newLogin)
	if rec := pe.addPasskey(token, a, ""); rec.Code != http.StatusCreated {
		t.Fatalf("add: %d %s", rec.Code, rec.Body.String())
	}
	c := pe.credentials(token)
	var wg sync.WaitGroup
	statuses := make([]int, len(c.Passkeys))
	for i, p := range c.Passkeys {
		wg.Go(func() {
			statuses[i] = pe.reqJSON(http.MethodPost, passkeyRoute(auth.MePasskeyRemovePath, p.UUID), token, "").Code
		})
	}
	wg.Wait()
	ok := 0
	for _, s := range statuses {
		if s == http.StatusOK {
			ok++
		}
	}
	if ok != 1 || len(pe.credentials(token).Passkeys) != 1 {
		t.Errorf("statuses %v", statuses)
	}
}

// TestPasswordManagement: a passkey-only member sets up password and TOTP and signs in with them; then changes the
// password and replaces the TOTP secret (the old ones stop working, the other sessions end), and finally removes the
// passkey; the password cannot be removed then.
func TestPasswordManagement(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	token, _, _ := pe.passkeyMember(maintainer, newLogin)
	member := pe.memberByLogin(newLogin)

	expectError(t, "change a password there is not", pe.post(auth.MePasswordChangePath, token,
		auth.PasswordRequest{Password: "new test password"}), http.StatusConflict, auth.CodeNoPassword)
	expectError(t, "a short password", pe.post(auth.MePasswordStartPath, token, auth.PasswordRequest{Password: "x"}),
		http.StatusBadRequest, auth.CodePasswordTooShort)
	rec := pe.post(auth.MePasswordStartPath, token, auth.PasswordRequest{Password: testPassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	start := decode[auth.RegisterPasswordStartResponse](t, rec)
	if !strings.Contains(start.OTPAuthURI, ":"+newLogin+"?") {
		t.Errorf("otpauth URI %q", start.OTPAuthURI)
	}
	expectError(t, "confirm with a wrong code", pe.post(auth.MePasswordConfirmPath, token,
		auth.EnrolmentConfirmRequest{Enrolment: start.Enrolment, Code: wrongCode(pe.code(start.Secret, 0))}),
		http.StatusBadRequest, auth.CodeCodeInvalid)
	// An enrolment of another member does not do.
	otherToken, _, _ := pe.passkeyMember(maintainer, "test_other")
	expectError(t, "someone else's enrolment", pe.post(auth.MePasswordConfirmPath, otherToken,
		auth.EnrolmentConfirmRequest{Enrolment: start.Enrolment, Code: pe.code(start.Secret, 0)}),
		http.StatusBadRequest, auth.CodeEnrolmentExpired)
	rec = pe.post(auth.MePasswordConfirmPath, token,
		auth.EnrolmentConfirmRequest{Enrolment: start.Enrolment, Code: pe.code(start.Secret, 0)})
	if rec.Code != http.StatusCreated || decode[auth.CredentialsResponse](t, rec).PasswordTOTP == nil {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
	}
	expectError(t, "a second password", pe.post(auth.MePasswordStartPath, token,
		auth.PasswordRequest{Password: testPassword}), http.StatusConflict, auth.CodePasswordExists)
	pe.advance(30 * time.Second)
	secret := start.Secret
	if rec := pe.post(auth.SignInTOTPPath, "", auth.SignInCodeRequest{Ticket: pe.ticket(newLogin, testPassword),
		Code: pe.code(secret, 0)}); rec.Code != http.StatusOK {
		t.Fatalf("sign in with the new password: %d %s", rec.Code, rec.Body.String())
	}

	// Change the password: the old one and its tickets stop working; the other sessions end.
	oldTicket := pe.ticket(newLogin, testPassword)
	otherDevice := pe.session(member)
	const newPassword = "another test password"
	if rec := pe.post(auth.MePasswordChangePath, token, auth.PasswordRequest{Password: newPassword}); rec.Code != http.StatusOK {
		t.Fatalf("change: %d %s", rec.Code, rec.Body.String())
	}
	expectError(t, "the old password", pe.post(auth.SignInPasswordPath, "",
		auth.SignInPasswordRequest{LoginName: newLogin, Password: testPassword}),
		http.StatusUnauthorized, auth.CodeCredentialsInvalid)
	pe.advance(30 * time.Second)
	expectError(t, "a ticket of the old password", pe.post(auth.SignInTOTPPath, "",
		auth.SignInCodeRequest{Ticket: oldTicket, Code: pe.code(secret, 0)}),
		http.StatusUnauthorized, auth.CodeTicketExpired)
	if pe.status(http.MethodGet, auth.MePath, otherDevice) != http.StatusUnauthorized {
		t.Error("another session survived the password change")
	}

	// Replace the TOTP secret: the old secret's codes stop working.
	pe.advance(30 * time.Second)
	rec = pe.post(auth.MeTOTPStartPath, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("totp start: %d %s", rec.Code, rec.Body.String())
	}
	replacement := decode[auth.RegisterPasswordStartResponse](t, rec)
	if rec := pe.post(auth.MeTOTPConfirmPath, token, auth.EnrolmentConfirmRequest{Enrolment: replacement.Enrolment,
		Code: pe.code(replacement.Secret, 0)}); rec.Code != http.StatusOK {
		t.Fatalf("totp confirm: %d %s", rec.Code, rec.Body.String())
	}
	pe.advance(30 * time.Second)
	expectError(t, "the old secret", pe.post(auth.SignInTOTPPath, "", auth.SignInCodeRequest{
		Ticket: pe.ticket(newLogin, newPassword), Code: pe.code(secret, 0)}), http.StatusUnauthorized, auth.CodeCodeInvalid)
	if rec := pe.post(auth.SignInTOTPPath, "", auth.SignInCodeRequest{Ticket: pe.ticket(newLogin, newPassword),
		Code: pe.code(replacement.Secret, 0)}); rec.Code != http.StatusOK {
		t.Errorf("the new secret: %d %s", rec.Code, rec.Body.String())
	}
	// A TOTP replacement enrolment cannot set up a password, nor the reverse.
	expectError(t, "a replacement as a new password", pe.post(auth.MePasswordConfirmPath, token,
		auth.EnrolmentConfirmRequest{Enrolment: replacement.Enrolment, Code: pe.code(replacement.Secret, 0)}),
		http.StatusBadRequest, auth.CodeEnrolmentExpired)

	// Remove the passkey: the password remains, and now cannot be removed.
	c := pe.credentials(token)
	if rec := pe.post(passkeyRoute(auth.MePasskeyRemovePath, c.Passkeys[0].UUID), token, nil); rec.Code != http.StatusOK {
		t.Fatalf("remove the passkey: %d %s", rec.Code, rec.Body.String())
	}
	expectError(t, "remove the password", pe.post(auth.MePasswordRemovePath, token, nil),
		http.StatusConflict, auth.CodeLastCredential)
	for _, action := range []string{auth.AuditPasswordChange, auth.AuditTOTPReplace, auth.AuditRevokeOtherSessions} {
		if len(pe.audits(action)) == 0 {
			t.Errorf("no %s audit entry", action)
		}
	}
	checkNoSecretsInAudit(t, pe.app, testPassword, newPassword, secret, replacement.Secret)
}

// checkNoSecretsInAudit fails when an audit entry contains one of the values.
func checkNoSecretsInAudit(t *testing.T, app core.App, values ...string) {
	t.Helper()
	rows, err := app.FindAllRecords(store.AuditLog, dbx.NewExp("1=1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		text := r.GetString("action") + " " + r.GetString("details")
		for _, v := range values {
			if v != "" && strings.Contains(text, v) {
				t.Errorf("audit entry %s contains %q", r.GetString("action"), v)
			}
		}
	}
}

// TestInstanceSettings: maintainers change the instance's name and cities with an audit entry; members cannot.
func TestInstanceSettings(t *testing.T) {
	pe := newCredEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	member := pe.member("test_member", auth.RoleMember)
	body := map[string]any{"name": " Test Circle ", "cities": []map[string]any{{"slug": "testtown", "name": "Test Town",
		"bbox": []float64{1, 1, 2, 2}, "center": []float64{1.5, 1.5}}}}
	if rec := pe.post(auth.InstanceSettingsPath, pe.session(member), body); rec.Code != http.StatusForbidden {
		t.Errorf("a member: %d", rec.Code)
	}
	expectError(t, "no cities", pe.post(auth.InstanceSettingsPath, pe.session(maintainer),
		map[string]any{"name": "Test Circle", "cities": []any{}}), http.StatusBadRequest, auth.CodeCitiesInvalid)
	expectError(t, "no name", pe.post(auth.InstanceSettingsPath, pe.session(maintainer),
		map[string]any{"name": " ", "cities": body["cities"]}), http.StatusBadRequest, auth.CodeInstanceNameInvalid)
	rec := pe.post(auth.InstanceSettingsPath, pe.session(maintainer), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if inst, err := settings.Load(pe.app); err != nil || inst.Name != "Test Circle" || len(inst.Cities) != 1 ||
		inst.Cities[0].Slug != "testtown" {
		t.Errorf("instance %+v, %v", inst, err)
	}
	audits := pe.audits(auth.AuditSettingsUpdate)
	if len(audits) != 1 || audits[0].GetString("actor") != maintainer.Id {
		t.Fatalf("%d settings audit entries", len(audits))
	}
	if d := details(t, audits[0]); d["name"] != "Test Circle" {
		t.Errorf("details %v", d)
	}
}
