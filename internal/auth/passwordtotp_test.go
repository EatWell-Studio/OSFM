package auth_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pquerna/otp/totp"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Made-up values only.
const (
	testPassword = "test password, made up"
	newLogin     = "test_new"
)

// pwEnv is a test app with the password and TOTP routes, a master key and a clock the test moves.
type pwEnv struct {
	*env
	clock     time.Time
	responses []*httptest.ResponseRecorder // every response, for the internal ID check
}

func newPWEnv(t *testing.T) *pwEnv {
	t.Helper()
	return newPWEnvWith(t, nil)
}

// newPWEnvWith is newPWEnv with the given rate limits (nil: the defaults); prepare runs before the router is built.
func newPWEnvWith(t *testing.T, limits *auth.Limits, prepare ...func(core.App)) *pwEnv {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	// A time 5 s into a TOTP step, so that a step lasts for the whole test unless the test moves the clock.
	pe := &pwEnv{clock: time.Unix(1_800_000_000/30*30+5, 0).UTC()}
	auth.SetNow(t, func() time.Time { return pe.clock })
	pe.env = newEnvWith(t, auth.Options{MasterKey: func() []byte { return key }, Limits: limits}, prepare...)
	return pe
}

func (pe *pwEnv) advance(d time.Duration) { pe.clock = pe.clock.Add(d) }

// post sends a JSON body with the X-OSFM header, and the session cookie when token is set.
func (pe *pwEnv) post(path, token string, body any) *httptest.ResponseRecorder {
	pe.t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		pe.t.Fatal(err)
	}
	rec := pe.reqJSON(http.MethodPost, path, token, string(b))
	pe.responses = append(pe.responses, rec)
	return rec
}

// code returns the TOTP code of a base32 secret at the test clock plus d.
func (pe *pwEnv) code(secret string, d time.Duration) string {
	pe.t.Helper()
	c, err := totp.GenerateCode(secret, pe.clock.Add(d))
	if err != nil {
		pe.t.Fatal(err)
	}
	return c
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decode[auth.AuthError](t, rec).Code
}

func expectError(t *testing.T, what string, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("%s: status %d, want %d: %s", what, rec.Code, status, rec.Body.String())
		return
	}
	if got := errorCode(t, rec); got != code {
		t.Errorf("%s: code %q, want %q", what, got, code)
	}
	if responseCookie(rec) != nil {
		t.Errorf("%s: a session cookie was set", what)
	}
}

// start begins a registration and returns the response.
func (pe *pwEnv) start(invite, login, password string) *httptest.ResponseRecorder {
	pe.t.Helper()
	return pe.post(auth.RegisterPasswordStartPath, "", auth.RegisterPasswordStartRequest{
		Invite: invite, LoginName: login, DisplayName: "Test New", Password: password,
	})
}

// register registers login with a new invite of inviter and returns the member's session token, the TOTP secret
// and the recovery codes.
func (pe *pwEnv) register(inviter *core.Record, login string) (token, secret string, codes []string) {
	pe.t.Helper()
	invite, _ := pe.invite(inviter)
	rec := pe.start(invite, login, testPassword)
	if rec.Code != http.StatusOK {
		pe.t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	s := decode[auth.RegisterPasswordStartResponse](pe.t, rec)
	rec = pe.post(auth.RegisterPasswordConfirmPath, "", auth.RegisterPasswordConfirmRequest{
		Invite: invite, Enrolment: s.Enrolment, Code: pe.code(s.Secret, 0),
	})
	if rec.Code != http.StatusCreated {
		pe.t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
	}
	c := responseCookie(rec)
	if c == nil {
		pe.t.Fatal("no session cookie after registering")
	}
	return c.Value, s.Secret, decode[auth.RegisteredResponse](pe.t, rec).RecoveryCodes
}

// ticket signs in with login and password and returns the ticket.
func (pe *pwEnv) ticket(login, password string) string {
	pe.t.Helper()
	rec := pe.post(auth.SignInPasswordPath, "", auth.SignInPasswordRequest{LoginName: login, Password: password})
	if rec.Code != http.StatusOK {
		pe.t.Fatalf("password step: %d %s", rec.Code, rec.Body.String())
	}
	if responseCookie(rec) != nil {
		pe.t.Fatal("the password step set a session cookie")
	}
	return decode[auth.SignInTicketResponse](pe.t, rec).Ticket
}

func (pe *pwEnv) memberByLogin(login string) *core.Record {
	pe.t.Helper()
	m, err := pe.app.FindFirstRecordByData(store.Members, "login_name", login)
	if err != nil {
		pe.t.Fatal(err)
	}
	return m
}

// TestPasswordTOTPEndToEnd registers with an invite and a generated TOTP code, then signs in with password and TOTP
// and with password and a recovery code: no session before both factors pass, no TOTP code or recovery code twice.
func TestPasswordTOTPEndToEnd(t *testing.T) {
	pe := newPWEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	invite, inviteRecord := pe.invite(maintainer)

	// Step 1: nothing is created yet, the invite stays unused, and no cookie is set.
	rec := pe.post(auth.RegisterPasswordStartPath, "", auth.RegisterPasswordStartRequest{
		Invite: invite, LoginName: "  Test_New ", DisplayName: " Test New ", Password: testPassword,
	})
	if rec.Code != http.StatusOK || responseCookie(rec) != nil {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("the start response may be cached")
	}
	start := decode[auth.RegisterPasswordStartResponse](t, rec)
	if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(start.Secret); err != nil ||
		len(start.Secret) != 32 {
		t.Errorf("secret %q: %v", start.Secret, err)
	}
	if !strings.HasPrefix(start.OTPAuthURI, "otpauth://totp/OSFM:test_new?") ||
		!strings.Contains(start.OTPAuthURI, "secret="+start.Secret) || !strings.Contains(start.OTPAuthURI, "period=30") ||
		!strings.Contains(start.OTPAuthURI, "digits=6") {
		t.Errorf("otpauth URI %q", start.OTPAuthURI)
	}
	if strings.Contains(start.Enrolment, start.Secret) || strings.Contains(start.Enrolment, newLogin) {
		t.Error("the pending enrolment is readable")
	}
	if n, _ := pe.app.CountRecords(store.Members, dbx.HashExp{"login_name": newLogin}); n != 0 {
		t.Fatal("the member was created before the TOTP code was confirmed")
	}
	if !pe.reload(inviteRecord).GetDateTime("used_at").IsZero() {
		t.Fatal("the invite was consumed before the TOTP code was confirmed")
	}

	// Step 2 with a wrong code: refused, nothing created.
	wrong := "000000"
	if pe.code(start.Secret, 0) == wrong {
		wrong = "111111"
	}
	expectError(t, "confirm with a wrong code", pe.post(auth.RegisterPasswordConfirmPath, "",
		auth.RegisterPasswordConfirmRequest{Invite: invite, Enrolment: start.Enrolment, Code: wrong}),
		http.StatusBadRequest, auth.CodeCodeInvalid)
	if n, _ := pe.app.CountRecords(store.Members, dbx.HashExp{"login_name": newLogin}); n != 0 {
		t.Fatal("a wrong code created the member")
	}

	// Step 2 with the code: member, credential, recovery codes, session.
	regCode := pe.code(start.Secret, 0)
	rec = pe.post(auth.RegisterPasswordConfirmPath, "", auth.RegisterPasswordConfirmRequest{
		Invite: invite, Enrolment: start.Enrolment, Code: regCode,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
	}
	cookie := responseCookie(rec)
	if cookie == nil || !cookie.HttpOnly || !cookie.Secure {
		t.Fatalf("session cookie %+v", cookie)
	}
	registered := decode[auth.RegisteredResponse](t, rec)
	member := pe.memberByLogin(newLogin)
	if registered.Me.UUID != member.GetString(store.FieldUUID) || registered.Me.DisplayName != "Test New" ||
		registered.Me.Role != auth.RoleMember {
		t.Errorf("me %+v", registered.Me)
	}
	if pe.status(http.MethodGet, auth.MePath, cookie.Value) != http.StatusOK {
		t.Error("the registration's session does not work")
	}
	if pe.reload(inviteRecord).GetString("used_by") != member.Id {
		t.Error("the invite was not consumed")
	}
	if member.GetString("email") != "" || member.ValidatePassword(testPassword) {
		t.Error("the member's PocketBase e-mail should be empty and its password random, not the member's")
	}

	// Stored: argon2id hash, the secret encrypted for this member, the confirming step, 10 code hashes.
	cred, err := pe.app.FindFirstRecordByData(store.Credentials, "member", member.Id)
	if err != nil {
		t.Fatal(err)
	}
	if cred.GetString("kind") != auth.CredentialPasswordTOTP ||
		!strings.HasPrefix(cred.GetString("password_hash"), "$argon2id$v=19$") {
		t.Errorf("credential kind %q, hash %q", cred.GetString("kind"), cred.GetString("password_hash"))
	}
	enc := cred.GetString("totp_secret_enc")
	if !strings.HasPrefix(enc, "v1.") || strings.Contains(enc, start.Secret) {
		t.Errorf("stored TOTP secret %q", enc)
	}
	raw, err := auth.OpenTOTPSecret(pe.app, member.GetString(store.FieldUUID), enc)
	if err != nil || base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw) != start.Secret {
		t.Errorf("the stored secret does not decrypt to the one shown: %v", err)
	}
	if _, err := auth.OpenTOTPSecret(pe.app, maintainer.GetString(store.FieldUUID), enc); err == nil {
		t.Error("the stored secret decrypts for another member")
	}
	if len(registered.RecoveryCodes) != auth.RecoveryCodeCount {
		t.Fatalf("%d recovery codes", len(registered.RecoveryCodes))
	}
	stored, err := pe.app.FindAllRecords(store.RecoveryCodes, dbx.HashExp{"member": member.Id})
	if err != nil || len(stored) != auth.RecoveryCodeCount {
		t.Fatalf("%d stored recovery codes: %v", len(stored), err)
	}
	for _, r := range stored {
		for _, c := range registered.RecoveryCodes {
			if strings.Contains(r.GetString("code_hash"), strings.ReplaceAll(c, "-", "")) {
				t.Error("a recovery code is stored in plain text")
			}
		}
	}

	// The password step alone gives no session.
	ticket := pe.ticket(newLogin, testPassword)
	if strings.Contains(ticket, member.GetString(store.FieldUUID)) {
		t.Error("the ticket is readable")
	}
	if pe.status(http.MethodGet, auth.MePath, ticket) != http.StatusUnauthorized {
		t.Error("the ticket works as a session")
	}

	// The code that confirmed the registration cannot sign in: its step is used.
	expectError(t, "the registration's code", pe.post(auth.SignInTOTPPath, "",
		auth.SignInCodeRequest{Ticket: ticket, Code: regCode}), http.StatusUnauthorized, auth.CodeCodeInvalid)

	// The next step's code signs in, once.
	pe.advance(30 * time.Second)
	code := pe.code(start.Secret, 0)
	rec = pe.post(auth.SignInTOTPPath, "", auth.SignInCodeRequest{Ticket: ticket, Code: code})
	if rec.Code != http.StatusOK || responseCookie(rec) == nil {
		t.Fatalf("TOTP step: %d %s", rec.Code, rec.Body.String())
	}
	if me := decode[auth.SignedInResponse](t, rec); me.Me.UUID != member.GetString(store.FieldUUID) ||
		me.RecoveryCodesLeft != nil {
		t.Errorf("signed in: %+v", me)
	}
	if pe.status(http.MethodGet, auth.MePath, responseCookie(rec).Value) != http.StatusOK {
		t.Error("the sign-in's session does not work")
	}
	expectError(t, "the same TOTP code again", pe.post(auth.SignInTOTPPath, "",
		auth.SignInCodeRequest{Ticket: pe.ticket(newLogin, testPassword), Code: code}),
		http.StatusUnauthorized, auth.CodeCodeInvalid)
	// An older code (within the skew) is refused once a newer one has been used.
	expectError(t, "an older TOTP code", pe.post(auth.SignInTOTPPath, "",
		auth.SignInCodeRequest{Ticket: pe.ticket(newLogin, testPassword), Code: pe.code(start.Secret, -30*time.Second)}),
		http.StatusUnauthorized, auth.CodeCodeInvalid)

	// A recovery code, typed carelessly, signs in once.
	typed := strings.ToLower(strings.ReplaceAll(registered.RecoveryCodes[3], "-", " "))
	rec = pe.post(auth.SignInRecoveryPath, "", auth.SignInCodeRequest{Ticket: pe.ticket(newLogin, testPassword),
		Code: typed})
	if rec.Code != http.StatusOK || responseCookie(rec) == nil {
		t.Fatalf("recovery step: %d %s", rec.Code, rec.Body.String())
	}
	if left := decode[auth.SignedInResponse](t, rec).RecoveryCodesLeft; left == nil || *left != auth.RecoveryCodeCount-1 {
		t.Errorf("recovery codes left: %v", left)
	}
	expectError(t, "the same recovery code again", pe.post(auth.SignInRecoveryPath, "",
		auth.SignInCodeRequest{Ticket: pe.ticket(newLogin, testPassword), Code: registered.RecoveryCodes[3]}),
		http.StatusUnauthorized, auth.CodeCodeInvalid)
	expectError(t, "another member's kind of code", pe.post(auth.SignInRecoveryPath, "",
		auth.SignInCodeRequest{Ticket: pe.ticket(newLogin, testPassword), Code: "0000-0000-0000-0000"}),
		http.StatusUnauthorized, auth.CodeCodeInvalid)

	// Tickets: made up, changed, expired.
	expectError(t, "a made-up ticket", pe.post(auth.SignInTOTPPath, "",
		auth.SignInCodeRequest{Ticket: "x", Code: pe.code(start.Secret, 0)}), http.StatusUnauthorized, auth.CodeTicketExpired)
	ticket = pe.ticket(newLogin, testPassword)
	changed := []byte(ticket)
	changed[len(changed)/2] ^= 1
	expectError(t, "a changed ticket", pe.post(auth.SignInTOTPPath, "",
		auth.SignInCodeRequest{Ticket: string(changed), Code: pe.code(start.Secret, 0)}),
		http.StatusUnauthorized, auth.CodeTicketExpired)
	pe.advance(5*time.Minute + time.Second)
	expectError(t, "an expired ticket", pe.post(auth.SignInRecoveryPath, "",
		auth.SignInCodeRequest{Ticket: ticket, Code: registered.RecoveryCodes[4]}),
		http.StatusUnauthorized, auth.CodeTicketExpired)

	// No response contained an internal ID (AD4).
	ids := []string{member.Id, cred.Id, maintainer.Id, inviteRecord.Id}
	for _, r := range stored {
		ids = append(ids, r.Id)
	}
	for _, rec := range pe.responses {
		var b bytes.Buffer
		_ = rec.Result().Header.Write(&b)
		b.Write(rec.Body.Bytes())
		for _, id := range ids {
			if strings.Contains(b.String(), id) {
				t.Errorf("a response contains the internal ID %s: %s", id, b.String())
			}
		}
	}
}

// TestSignInGivesNothingAway: an unknown login name, a wrong password, a member without a password and a member who
// left get the same response.
func TestSignInGivesNothingAway(t *testing.T) {
	pe := newPWEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	pe.register(maintainer, newLogin)
	pe.register(maintainer, "test_left")
	left := pe.memberByLogin("test_left")
	left.Set("left_at", time.Now())
	if err := pe.app.Save(left); err != nil {
		t.Fatal(err)
	}
	pe.member("test_passkey_only", auth.RoleMember)

	var first *httptest.ResponseRecorder
	for _, c := range []auth.SignInPasswordRequest{
		{LoginName: "test_nobody", Password: testPassword},
		{LoginName: newLogin, Password: testPassword + "x"},
		{LoginName: newLogin, Password: ""},
		{LoginName: "test_passkey_only", Password: testPassword},
		{LoginName: "test_left", Password: testPassword},
		{LoginName: "x", Password: testPassword},
		{LoginName: newLogin, Password: strings.Repeat("x", 200)},
	} {
		rec := pe.post(auth.SignInPasswordPath, "", c)
		expectError(t, c.LoginName, rec, http.StatusUnauthorized, auth.CodeCredentialsInvalid)
		if first == nil {
			first = rec
			continue
		}
		if rec.Body.String() != first.Body.String() || !headersEqual(rec.Header(), first.Header()) {
			t.Errorf("%+v: %v %s differs from %v %s", c, rec.Header(), rec.Body, first.Header(), first.Body)
		}
	}
	// Login names are matched as typed on a phone.
	pe.ticket("  Test_New", testPassword)
}

func headersEqual(a, b http.Header) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !slices.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

// TestRegistrationNeedsValidInvite: without a valid invite there is no registration, at either step, and every
// invalid invite gets the same answer.
func TestRegistrationNeedsValidInvite(t *testing.T) {
	pe := newPWEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	used, _ := pe.invite(maintainer)
	pe.register(maintainer, "test_first") // uses another invite
	usedRecord, _ := pe.app.FindFirstRecordByData(store.Invites, "code_hash", auth.HashToken(used))
	pe.setInvite(usedRecord, dbx.Params{"used_at": "2026-01-01 00:00:00.000Z"})
	revoked, revokedRecord := pe.invite(maintainer)
	if err := auth.RevokeInvite(pe.app, revokedRecord, maintainer); err != nil {
		t.Fatal(err)
	}

	var first *httptest.ResponseRecorder
	for _, invite := range []string{"", "x", auth.HashToken("x")[:43], used, revoked} {
		rec := pe.start(invite, newLogin, testPassword)
		expectError(t, "invite "+invite, rec, http.StatusForbidden, auth.CodeInviteInvalid)
		if first != nil && rec.Body.String() != first.Body.String() {
			t.Errorf("invite %q: %s differs from %s", invite, rec.Body, first.Body)
		}
		first = rec
	}

	// An invite revoked between the steps: the confirmation fails, nothing is created.
	invite, inviteRecord := pe.invite(maintainer)
	start := decode[auth.RegisterPasswordStartResponse](t, pe.start(invite, newLogin, testPassword))
	if err := auth.RevokeInvite(pe.app, inviteRecord, maintainer); err != nil {
		t.Fatal(err)
	}
	expectError(t, "revoked between the steps", pe.post(auth.RegisterPasswordConfirmPath, "",
		auth.RegisterPasswordConfirmRequest{Invite: invite, Enrolment: start.Enrolment, Code: pe.code(start.Secret, 0)}),
		http.StatusForbidden, auth.CodeInviteInvalid)
	if n, _ := pe.app.CountRecords(store.Members, dbx.HashExp{"login_name": newLogin}); n != 0 {
		t.Error("a member was created with a revoked invite")
	}

	// An enrolment confirmed with another invite than the one it was started with.
	a, _ := pe.invite(maintainer)
	b, _ := pe.invite(maintainer)
	start = decode[auth.RegisterPasswordStartResponse](t, pe.start(a, newLogin, testPassword))
	expectError(t, "another invite", pe.post(auth.RegisterPasswordConfirmPath, "",
		auth.RegisterPasswordConfirmRequest{Invite: b, Enrolment: start.Enrolment, Code: pe.code(start.Secret, 0)}),
		http.StatusBadRequest, auth.CodeEnrolmentExpired)

	// The enrolment works once: the invite is consumed by the first confirmation.
	confirm := auth.RegisterPasswordConfirmRequest{Invite: a, Enrolment: start.Enrolment, Code: pe.code(start.Secret, 0)}
	if rec := pe.post(auth.RegisterPasswordConfirmPath, "", confirm); rec.Code != http.StatusCreated {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
	}
	expectError(t, "the same enrolment again", pe.post(auth.RegisterPasswordConfirmPath, "", confirm),
		http.StatusForbidden, auth.CodeInviteInvalid)
}

// TestRegistrationErrors: useful errors for the login name, display name and password; a changed, expired or missing
// enrolment; a login name taken between the steps.
func TestRegistrationErrors(t *testing.T) {
	pe := newPWEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	invite, inviteRecord := pe.invite(maintainer)

	for _, c := range []struct {
		login, display, password string
		status                   int
		code                     string
	}{
		{"ab", "", testPassword, http.StatusBadRequest, auth.CodeLoginNameInvalid},
		{"test new", "", testPassword, http.StatusBadRequest, auth.CodeLoginNameInvalid},
		{strings.Repeat("a", 33), "", testPassword, http.StatusBadRequest, auth.CodeLoginNameInvalid},
		{"test_maintainer", "", testPassword, http.StatusConflict, auth.CodeLoginNameTaken},
		{"TEST_MAINTAINER", "", testPassword, http.StatusConflict, auth.CodeLoginNameTaken},
		{newLogin, strings.Repeat("x", 65), testPassword, http.StatusBadRequest, auth.CodeDisplayNameInvalid},
		{newLogin, "a\nb", testPassword, http.StatusBadRequest, auth.CodeDisplayNameInvalid},
		{newLogin, "", "short", http.StatusBadRequest, auth.CodePasswordTooShort},
		{newLogin, "", strings.Repeat("x", 129), http.StatusBadRequest, auth.CodePasswordTooLong},
		{"test_new_name", "", "Test_New_Name", http.StatusBadRequest, auth.CodePasswordIsLogin},
	} {
		rec := pe.post(auth.RegisterPasswordStartPath, "", auth.RegisterPasswordStartRequest{
			Invite: invite, LoginName: c.login, DisplayName: c.display, Password: c.password,
		})
		expectError(t, c.login+"/"+c.display+"/"+c.password, rec, c.status, c.code)
	}
	for _, body := range []string{"", "{", `{"invite":1}`} {
		rec := pe.reqJSON(http.MethodPost, auth.RegisterPasswordStartPath, "", body)
		expectError(t, "body "+body, rec, http.StatusBadRequest, auth.CodeInvalidRequest)
	}
	if rec := pe.reqJSON(http.MethodPost, auth.RegisterPasswordStartPath, "",
		`{"invite":"`+strings.Repeat("x", 5000)+`"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an oversized body: %d", rec.Code)
	}
	// Without the X-OSFM header: 403 (CSRF).
	r := httptest.NewRequest(http.MethodPost, auth.RegisterPasswordStartPath, strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	pe.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Errorf("without the CSRF header: %d", rec.Code)
	}

	start := decode[auth.RegisterPasswordStartResponse](t, pe.start(invite, newLogin, testPassword))
	confirm := func(enrolment string) *httptest.ResponseRecorder {
		return pe.post(auth.RegisterPasswordConfirmPath, "", auth.RegisterPasswordConfirmRequest{
			Invite: invite, Enrolment: enrolment, Code: pe.code(start.Secret, 0),
		})
	}
	changed := []byte(start.Enrolment)
	changed[10] ^= 1
	expectError(t, "a changed enrolment", confirm(string(changed)), http.StatusBadRequest, auth.CodeEnrolmentExpired)
	expectError(t, "no enrolment", confirm(""), http.StatusBadRequest, auth.CodeEnrolmentExpired)

	// Taken between the steps: refused, the invite stays unused.
	pe.member(newLogin, auth.RoleMember)
	expectError(t, "taken meanwhile", confirm(start.Enrolment), http.StatusConflict, auth.CodeLoginNameTaken)
	if !pe.reload(inviteRecord).GetDateTime("used_at").IsZero() {
		t.Error("the invite was consumed by a failed registration")
	}

	start = decode[auth.RegisterPasswordStartResponse](t, pe.start(invite, "test_other", testPassword))
	pe.advance(15*time.Minute + time.Second)
	expectError(t, "an expired enrolment", confirm(start.Enrolment), http.StatusBadRequest, auth.CodeEnrolmentExpired)
}

// TestLeavingEndsTickets: a ticket issued before the member left, or before the password changed, stops working.
func TestLeavingEndsTickets(t *testing.T) {
	pe := newPWEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	_, secret, _ := pe.register(maintainer, newLogin)
	pe.advance(30 * time.Second)

	ticket := pe.ticket(newLogin, testPassword)
	member := pe.memberByLogin(newLogin)
	cred, err := pe.app.FindFirstRecordByData(store.Credentials, "member", member.Id)
	if err != nil {
		t.Fatal(err)
	}
	cred.Set("password_hash", "$argon2id$v=19$m=256,t=1,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g")
	if err := pe.app.Save(cred); err != nil {
		t.Fatal(err)
	}
	expectError(t, "after a password change", pe.post(auth.SignInTOTPPath, "",
		auth.SignInCodeRequest{Ticket: ticket, Code: pe.code(secret, 0)}), http.StatusUnauthorized, auth.CodeTicketExpired)

	_, secret, _ = pe.register(maintainer, "test_leaving")
	pe.advance(30 * time.Second)
	ticket = pe.ticket("test_leaving", testPassword)
	leaving := pe.memberByLogin("test_leaving")
	leaving.Set("left_at", time.Now())
	if err := pe.app.Save(leaving); err != nil {
		t.Fatal(err)
	}
	expectError(t, "after leaving", pe.post(auth.SignInTOTPPath, "",
		auth.SignInCodeRequest{Ticket: ticket, Code: pe.code(secret, 0)}), http.StatusUnauthorized, auth.CodeTicketExpired)
}

// TestSecondFactorOnceUnderConcurrency: of several requests with the same TOTP code, or the same recovery code, only
// one signs in.
func TestSecondFactorOnceUnderConcurrency(t *testing.T) {
	pe := newPWEnv(t)
	maintainer := pe.member("test_maintainer", auth.RoleMaintainer)
	_, secret, codes := pe.register(maintainer, newLogin)
	pe.advance(30 * time.Second)

	for _, c := range []struct {
		path, code string
	}{{auth.SignInTOTPPath, pe.code(secret, 0)}, {auth.SignInRecoveryPath, codes[0]}} {
		// No more than the per-member TOTP threshold: a sixth concurrent attempt would be refused by the rate limit
		// (429) before reaching the code check that this test is about.
		const n = 5
		tickets := make([]string, n)
		for i := range tickets {
			tickets[i] = pe.ticket(newLogin, testPassword)
		}
		var wg sync.WaitGroup
		statuses := make([]int, n)
		for i := range n {
			wg.Go(func() {
				body, _ := json.Marshal(auth.SignInCodeRequest{Ticket: tickets[i], Code: c.code})
				statuses[i] = pe.reqJSON(http.MethodPost, c.path, "", string(body)).Code
			})
		}
		wg.Wait()
		ok := 0
		for _, s := range statuses {
			if s == http.StatusOK {
				ok++
			} else if s != http.StatusUnauthorized {
				t.Errorf("%s: status %d", c.path, s)
			}
		}
		if ok != 1 {
			t.Errorf("%s: %d of %d concurrent requests signed in, want 1 (%v)", c.path, ok, n, statuses)
		}
	}
}

// TestNewRecoveryCodes replaces a member's codes.
func TestNewRecoveryCodes(t *testing.T) {
	en := newEnv(t)
	m := en.member("test_member", auth.RoleMember)
	first, err := auth.NewRecoveryCodes(en.app, m)
	if err != nil {
		t.Fatal(err)
	}
	second, err := auth.NewRecoveryCodes(en.app, m)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != auth.RecoveryCodeCount || len(second) != auth.RecoveryCodeCount {
		t.Fatalf("%d, %d codes", len(first), len(second))
	}
	seen := map[string]bool{}
	for _, c := range append(first, second...) {
		if len(c) != 19 || seen[c] {
			t.Errorf("code %q (length or repeated)", c)
		}
		seen[c] = true
	}
	stored, err := en.app.FindAllRecords(store.RecoveryCodes, dbx.HashExp{"member": m.Id})
	if err != nil || len(stored) != auth.RecoveryCodeCount {
		t.Fatalf("%d stored: %v", len(stored), err)
	}
	for _, r := range stored {
		if r.GetString("code_hash") == auth.HashRecoveryCode(strings.ReplaceAll(first[0], "-", "")) {
			t.Error("an old code is still stored")
		}
	}
	if n, _ := auth.RemainingRecoveryCodes(en.app, m); n != auth.RecoveryCodeCount {
		t.Errorf("remaining %d", n)
	}
	if _, err := auth.NewRecoveryCodes(en.app, nil); err == nil {
		t.Error("codes for no member")
	}
}

// TestWithoutMasterKey: the routes answer 500 rather than storing a TOTP secret unencrypted.
func TestWithoutMasterKey(t *testing.T) {
	en := newEnv(t)
	maintainer := en.member("test_maintainer", auth.RoleMaintainer)
	invite, _ := en.invite(maintainer)
	body, _ := json.Marshal(auth.RegisterPasswordStartRequest{Invite: invite, LoginName: newLogin, Password: testPassword}) // #nosec G117 -- made up
	if rec := en.reqJSON(http.MethodPost, auth.RegisterPasswordStartPath, "", string(body)); rec.Code != http.StatusInternalServerError {
		t.Errorf("status %d", rec.Code)
	}
}
