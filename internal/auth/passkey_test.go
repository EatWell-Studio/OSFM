package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/auth/passkeytest"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

type optionsResponse struct {
	Options json.RawMessage `json:"options"`
	State   string          `json:"state"`
}

func jsonBody(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// registerOptions asks for registration options; it fails the test unless they come. An empty display name stands
// for "Test <login>".
func (en *env) registerOptions(code, login, display string) optionsResponse {
	en.t.Helper()
	if display == "" {
		display = "Test " + login
	}
	rec := en.reqJSON(http.MethodPost, auth.PasskeyRegisterOptionsPath, "",
		jsonBody(en.t, map[string]string{"invite": code, "loginName": login, "displayName": display}))
	if rec.Code != http.StatusOK {
		en.t.Fatalf("registration options: %d %s", rec.Code, rec.Body.String())
	}
	return decode[optionsResponse](en.t, rec)
}

// finishRegistration sends the authenticator's answer to the options.
func (en *env) finishRegistration(a *passkeytest.Authenticator, code string, o optionsResponse) *httptest.ResponseRecorder {
	en.t.Helper()
	cred, err := a.Create(o.Options)
	if err != nil {
		en.t.Fatal(err)
	}
	return en.reqJSON(http.MethodPost, auth.PasskeyRegisterPath, "",
		jsonBody(en.t, map[string]any{"invite": code, "state": o.State, "credential": json.RawMessage(cred)}))
}

// registerPasskey registers a member with a passkey of a; it fails the test unless that works.
func (en *env) registerPasskey(a *passkeytest.Authenticator, code, login string) *httptest.ResponseRecorder {
	en.t.Helper()
	rec := en.finishRegistration(a, code, en.registerOptions(code, login, ""))
	if rec.Code != http.StatusCreated {
		en.t.Fatalf("registration: %d %s", rec.Code, rec.Body.String())
	}
	return rec
}

func (en *env) signInOptions() optionsResponse {
	en.t.Helper()
	rec := en.reqJSON(http.MethodPost, auth.PasskeySignInOptionsPath, "", "")
	if rec.Code != http.StatusOK {
		en.t.Fatalf("sign-in options: %d %s", rec.Code, rec.Body.String())
	}
	return decode[optionsResponse](en.t, rec)
}

func (en *env) signInWith(state string, assertion []byte) *httptest.ResponseRecorder {
	en.t.Helper()
	return en.reqJSON(http.MethodPost, auth.PasskeySignInPath, "",
		jsonBody(en.t, map[string]any{"state": state, "credential": json.RawMessage(assertion)}))
}

// passkeySignIn runs a whole sign-in with a.
func (en *env) passkeySignIn(a *passkeytest.Authenticator) *httptest.ResponseRecorder {
	en.t.Helper()
	o := en.signInOptions()
	assertion, err := a.Get(o.Options)
	if err != nil {
		en.t.Fatal(err)
	}
	return en.signInWith(o.State, assertion)
}

// sessionToken returns the session cookie a response set.
func sessionToken(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c.Value
		}
	}
	t.Fatalf("no session cookie in %v", rec.Header().Values("Set-Cookie"))
	return ""
}

func (en *env) memberByLogin(login string) *core.Record {
	en.t.Helper()
	m, err := en.app.FindFirstRecordByData(store.Members, "login_name", login)
	if err != nil {
		en.t.Fatalf("member %s: %v", login, err)
	}
	return m
}

func (en *env) passkeyOf(member *core.Record) *core.Record {
	en.t.Helper()
	r, err := en.app.FindFirstRecordByData(store.Credentials, "member", member.Id)
	if err != nil {
		en.t.Fatal(err)
	}
	return r
}

// TestPasskeyRegistrationAndSignIn: a new member registers with an invite and a passkey, is signed in and receives ten
// recovery codes; later they sign in with the passkey alone, without a login name.
func TestPasskeyRegistrationAndSignIn(t *testing.T) {
	en := newEnv(t)
	inviter := en.member("test_maintainer", auth.RoleMaintainer)
	code, invite := en.invite(inviter)
	a := &passkeytest.Authenticator{}

	o := en.registerOptions(code, "  Test_New ", "Test New Member")
	var opts struct {
		PublicKey struct {
			RP          struct{ ID, Name string }
			User        struct{ ID, Name, DisplayName string }
			Attestation string
			Selection   struct {
				ResidentKey      string `json:"residentKey"`
				UserVerification string `json:"userVerification"`
			} `json:"authenticatorSelection"`
		}
	}
	if err := json.Unmarshal(o.Options, &opts); err != nil {
		t.Fatal(err)
	}
	pk := opts.PublicKey
	if pk.RP.ID != "localhost" || pk.User.Name != "test_new" || pk.User.DisplayName != "Test New Member" ||
		pk.Attestation != "none" || pk.Selection.ResidentKey != "required" || pk.Selection.UserVerification != "required" {
		t.Errorf("creation options: %s", o.Options)
	}
	if invite = en.reload(invite); auth.InviteState(invite, time.Now()) != auth.InviteActive {
		t.Error("asking for options used the invite")
	}

	rec := en.finishRegistration(a, code, o)
	if rec.Code != http.StatusCreated {
		t.Fatalf("registration: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", rec.Header().Get("Cache-Control"))
	}
	codes := decode[auth.PasskeyRegisterResponse](t, rec).RecoveryCodes
	if len(codes) != auth.RecoveryCodeCount {
		t.Fatalf("%d recovery codes", len(codes))
	}
	token := sessionToken(t, rec)

	member := en.memberByLogin("test_new")
	if member.GetString("display_name") != "Test New Member" || member.GetString("role") != auth.RoleMember ||
		member.GetString("invited_by") != inviter.Id {
		t.Errorf("member: %v", member.FieldsData())
	}
	handle := member.GetString("passkey_user_handle")
	raw, err := base64.RawURLEncoding.DecodeString(handle)
	if err != nil || len(raw) != 32 {
		t.Errorf("user handle %q", handle)
	}
	if user, _ := base64.RawURLEncoding.DecodeString(pk.User.ID); string(user) != string(raw) {
		t.Error("the stored user handle is not the one in the options")
	}
	for _, id := range []string{member.Id, member.GetString(store.FieldUUID)} {
		if strings.Contains(string(raw), id) || strings.Contains(handle, id) {
			t.Errorf("the user handle contains %q", id)
		}
	}
	if invite = en.reload(invite); invite.GetString("used_by") != member.Id {
		t.Error("the invite was not used")
	}

	cred := en.passkeyOf(member)
	if cred.GetString("kind") != "passkey" || cred.GetString("credential_id") != base64.RawURLEncoding.EncodeToString(a.Credentials[0].ID) ||
		cred.GetString("public_key") == "" || cred.GetInt("sign_count") != 0 || !store.IsUUID(cred.GetString(store.FieldUUID)) ||
		!cred.GetBool("user_verified") || cred.GetString("attestation_format") != "none" ||
		cred.GetString("aaguid") != "00000000-0000-0000-0000-000000000000" ||
		strings.Join(cred.GetStringSlice("transports"), ",") != "internal,hybrid" {
		t.Errorf("credential: %v", cred.FieldsData())
	}

	// The recovery codes: ten distinct rows, hashes only.
	rows, err := en.app.FindAllRecords(store.RecoveryCodes, dbx.HashExp{"member": member.Id})
	if err != nil || len(rows) != auth.RecoveryCodeCount {
		t.Fatalf("%d recovery code rows, %v", len(rows), err)
	}
	stored := map[string]bool{}
	for _, r := range rows {
		stored[r.GetString("code_hash")] = true
	}
	for _, c := range codes {
		if !stored[auth.HashRecoveryCode(strings.ReplaceAll(c, "-", ""))] || strings.Contains(strings.Join(mapKeys(stored), ""), c) {
			t.Errorf("recovery code %q is not stored as its hash", c)
		}
	}

	if me := en.reqJSON(http.MethodGet, auth.MePath, token, ""); me.Code != http.StatusOK ||
		decode[auth.Me](t, me).UUID != member.GetString(store.FieldUUID) {
		t.Errorf("/me after registration: %d %s", me.Code, me.Body.String())
	}

	// Sign-in: discoverable, no login name.
	rec = en.passkeySignIn(a)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("sign-in: %d %s", rec.Code, rec.Body.String())
	}
	if me := en.reqJSON(http.MethodGet, auth.MePath, sessionToken(t, rec), ""); me.Code != http.StatusOK ||
		decode[auth.Me](t, me).UUID != member.GetString(store.FieldUUID) {
		t.Errorf("/me after sign-in: %d %s", me.Code, me.Body.String())
	}
	if got := en.reload(cred).GetInt("sign_count"); got != 1 {
		t.Errorf("sign count after sign-in: %d", got)
	}
	if rec = en.passkeySignIn(a); rec.Code != http.StatusNoContent {
		t.Errorf("second sign-in: %d", rec.Code)
	}
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestPasskeySignCountRegression: a counter that does not increase is refused (and audited); a counter that stays
// zero, as synced passkeys report it, is accepted.
func TestPasskeySignCountRegression(t *testing.T) {
	en := newEnv(t)
	inviter := en.member("test_maintainer", auth.RoleMaintainer)
	a := &passkeytest.Authenticator{}
	code, _ := en.invite(inviter)
	en.registerPasskey(a, code, "test_counter")
	member := en.memberByLogin("test_counter")

	for range 2 {
		if rec := en.passkeySignIn(a); rec.Code != http.StatusNoContent {
			t.Fatalf("sign-in: %d", rec.Code)
		}
	}
	cred := en.passkeyOf(member)
	if cred.GetInt("sign_count") != 2 {
		t.Fatalf("sign count %d", cred.GetInt("sign_count"))
	}

	// A clone that has signed once less: its next counter equals the stored one.
	a.Credentials[0].SignCount = 1
	rec := en.passkeySignIn(a)
	if rec.Code != http.StatusUnauthorized || errorCode(t, rec) != auth.CodePasskeyFailed {
		t.Errorf("equal counter: %d %s", rec.Code, rec.Body.String())
	}
	a.Credentials[0].SignCount = 0
	if rec := en.passkeySignIn(a); rec.Code != http.StatusUnauthorized {
		t.Errorf("lower counter: %d", rec.Code)
	}
	// Zero after non-zero is a regression too.
	a.ZeroCounter = true
	if rec := en.passkeySignIn(a); rec.Code != http.StatusUnauthorized {
		t.Errorf("zero after non-zero: %d", rec.Code)
	}
	if got := en.reload(cred).GetInt("sign_count"); got != 2 {
		t.Errorf("a refused sign-in changed the counter to %d", got)
	}
	n, err := en.app.CountRecords(store.AuditLog, dbx.HashExp{"action": auth.AuditPasskeyCloneWarning,
		"target_uuid": member.GetString(store.FieldUUID)})
	if err != nil || n != 3 {
		t.Errorf("%d audit entries, %v", n, err)
	}
	// The real authenticator still signs in.
	a.ZeroCounter = false
	a.Credentials[0].SignCount = 2
	if rec := en.passkeySignIn(a); rec.Code != http.StatusNoContent {
		t.Errorf("sign-in with an increasing counter: %d", rec.Code)
	}

	// Synced passkeys: zero, every time.
	synced := &passkeytest.Authenticator{ZeroCounter: true, BackupEligible: true}
	code, _ = en.invite(inviter)
	en.registerPasskey(synced, code, "test_synced")
	for range 3 {
		if rec := en.passkeySignIn(synced); rec.Code != http.StatusNoContent {
			t.Fatalf("synced sign-in: %d %s", rec.Code, rec.Body.String())
		}
	}
	if c := en.passkeyOf(en.memberByLogin("test_synced")); !c.GetBool("backup_eligible") || !c.GetBool("backup_state") {
		t.Errorf("backup flags: %v", c.FieldsData())
	}
}

// TestPasskeyWrongOriginOrRPID: responses collected at another origin or for another relying party are refused, at
// registration (the invite stays unused) and at sign-in.
func TestPasskeyWrongOriginOrRPID(t *testing.T) {
	en := newEnv(t)
	inviter := en.member("test_maintainer", auth.RoleMaintainer)
	code, invite := en.invite(inviter)

	for name, a := range map[string]*passkeytest.Authenticator{
		"origin":            {Origin: "http://evil.localhost:8090"},
		"scheme":            {Origin: "https://localhost:8090"},
		"port":              {Origin: "http://localhost:8091"},
		"rp id":             {RPID: "example.org"},
		"user verification": {NoUserVerification: true},
	} {
		rec := en.finishRegistration(a, code, en.registerOptions(code, "test_new", ""))
		if rec.Code != http.StatusBadRequest || errorCode(t, rec) != auth.CodePasskeyFailed {
			t.Errorf("registration, wrong %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if auth.InviteState(en.reload(invite), time.Now()) != auth.InviteActive {
		t.Error("a failed registration used the invite")
	}
	if n, _ := en.app.CountRecords(store.Members); n != 1 {
		t.Errorf("%d members after failed registrations", n)
	}

	a := &passkeytest.Authenticator{}
	en.registerPasskey(a, code, "test_new")
	for name, change := range map[string]func(*passkeytest.Authenticator){
		"origin":            func(a *passkeytest.Authenticator) { a.Origin = "http://localhost:9999" },
		"rp id":             func(a *passkeytest.Authenticator) { a.RPID = "example.org" },
		"user verification": func(a *passkeytest.Authenticator) { a.NoUserVerification = true },
	} {
		wrong := *a
		change(&wrong)
		if rec := en.passkeySignIn(&wrong); rec.Code != http.StatusUnauthorized || errorCode(t, rec) != auth.CodePasskeyFailed {
			t.Errorf("sign-in, wrong %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if rec := en.passkeySignIn(a); rec.Code != http.StatusNoContent {
		t.Errorf("sign-in after the refused ones: %d", rec.Code)
	}
}

// TestPasskeyStateIsSingleUse: a state, and with it its challenge, works once; a state does not work for the other
// ceremony, nor after it expired, nor when changed.
func TestPasskeyStateIsSingleUse(t *testing.T) {
	en := newEnv(t)
	inviter := en.member("test_maintainer", auth.RoleMaintainer)
	code, _ := en.invite(inviter)
	a := &passkeytest.Authenticator{}
	en.registerPasskey(a, code, "test_member")

	// Replaying a sign-in that worked.
	o := en.signInOptions()
	assertion, err := a.Get(o.Options)
	if err != nil {
		t.Fatal(err)
	}
	if rec := en.signInWith(o.State, assertion); rec.Code != http.StatusNoContent {
		t.Fatalf("sign-in: %d", rec.Code)
	}
	if rec := en.signInWith(o.State, assertion); rec.Code != http.StatusUnauthorized {
		t.Errorf("replayed sign-in: %d", rec.Code)
	}
	// A failed attempt uses the state up too.
	o = en.signInOptions()
	if rec := en.signInWith(o.State, []byte(`{}`)); rec.Code != http.StatusUnauthorized {
		t.Errorf("garbage credential: %d", rec.Code)
	}
	assertion, _ = a.Get(o.Options)
	if rec := en.signInWith(o.State, assertion); rec.Code != http.StatusUnauthorized {
		t.Errorf("valid assertion after a failed attempt with the same state: %d", rec.Code)
	}
	// Another state with the challenge of an earlier one.
	o1, o2 := en.signInOptions(), en.signInOptions()
	assertion, _ = a.Get(o1.Options)
	if rec := en.signInWith(o2.State, assertion); rec.Code != http.StatusUnauthorized {
		t.Errorf("assertion for another challenge: %d", rec.Code)
	}
	// A changed state.
	o = en.signInOptions()
	assertion, _ = a.Get(o.Options)
	tampered := []byte(o.State)
	tampered[10] ^= 1
	if rec := en.signInWith(string(tampered), assertion); rec.Code != http.StatusUnauthorized {
		t.Errorf("tampered state: %d", rec.Code)
	}
	// An expired state.
	o = en.signInOptions()
	assertion, _ = a.Get(o.Options)
	auth.SetNow(t, func() time.Time { return time.Now().Add(auth.CeremonyTimeout + 2*time.Minute) })
	if rec := en.signInWith(o.State, assertion); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired state: %d", rec.Code)
	}
	auth.SetNow(t, time.Now)

	// A registration state does not open as a sign-in state, nor the reverse.
	code2, _ := en.invite(inviter)
	ro := en.registerOptions(code2, "test_other", "")
	assertion, _ = a.Assert(a.Credentials[0], "AAAAAAAAAAAAAAAAAAAAAA")
	if rec := en.signInWith(ro.State, assertion); rec.Code != http.StatusUnauthorized {
		t.Errorf("registration state used for sign-in: %d", rec.Code)
	}
	so := en.signInOptions()
	cred, err := (&passkeytest.Authenticator{}).Create(ro.Options)
	if err != nil {
		t.Fatal(err)
	}
	rec := en.reqJSON(http.MethodPost, auth.PasskeyRegisterPath, "",
		jsonBody(t, map[string]any{"invite": code2, "state": so.State, "credential": json.RawMessage(cred)}))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("sign-in state used for registration: %d", rec.Code)
	}

	// Replaying a registration that worked.
	other := &passkeytest.Authenticator{}
	ro = en.registerOptions(code2, "test_other", "")
	cred, _ = other.Create(ro.Options)
	body := jsonBody(t, map[string]any{"invite": code2, "state": ro.State, "credential": json.RawMessage(cred)})
	if rec := en.reqJSON(http.MethodPost, auth.PasskeyRegisterPath, "", body); rec.Code != http.StatusCreated {
		t.Fatalf("registration: %d %s", rec.Code, rec.Body.String())
	}
	if rec := en.reqJSON(http.MethodPost, auth.PasskeyRegisterPath, "", body); rec.Code != http.StatusBadRequest {
		t.Errorf("replayed registration: %d", rec.Code)
	}
}

// TestPasskeyRegistrationNeedsInvite: no options without a valid invite; the code at the end must be the one the
// ceremony began with and still valid; the names are checked.
func TestPasskeyRegistrationNeedsInvite(t *testing.T) {
	en := newEnv(t)
	inviter := en.member("test_maintainer", auth.RoleMaintainer)
	code, invite := en.invite(inviter)

	for _, c := range []string{"", "x", strings.Repeat("A", 43)} {
		rec := en.reqJSON(http.MethodPost, auth.PasskeyRegisterOptionsPath, "",
			jsonBody(t, map[string]string{"invite": c, "loginName": "test_new", "displayName": "Test New"}))
		if rec.Code != http.StatusForbidden || errorCode(t, rec) != auth.CodeInviteInvalid {
			t.Errorf("options with code %q: %d %s", c, rec.Code, rec.Body.String())
		}
	}
	for login, want := range map[string]string{
		"ab": auth.CodeLoginNameInvalid, "has space": auth.CodeLoginNameInvalid, "名字": auth.CodeLoginNameInvalid,
		strings.Repeat("a", 33): auth.CodeLoginNameInvalid, "test_maintainer": auth.CodeLoginNameTaken,
	} {
		rec := en.reqJSON(http.MethodPost, auth.PasskeyRegisterOptionsPath, "",
			jsonBody(t, map[string]string{"invite": code, "loginName": login, "displayName": "Test New"}))
		if errorCode(t, rec) != want {
			t.Errorf("login name %q: %d %s", login, rec.Code, rec.Body.String())
		}
	}
	// A taken login name or display name is not told without a valid invite.
	for _, body := range []map[string]string{
		{"invite": "x", "loginName": "test_maintainer", "displayName": "Test New"},
		{"invite": "x", "loginName": "test_new", "displayName": "Test test_maintainer"},
	} {
		rec := en.reqJSON(http.MethodPost, auth.PasskeyRegisterOptionsPath, "", jsonBody(t, body))
		if errorCode(t, rec) != auth.CodeInviteInvalid {
			t.Errorf("taken name without invite: %s", rec.Body.String())
		}
	}
	// The display name is required, is not the login name and is no one else's name (M2-Q6).
	for display, want := range map[string]string{
		"": auth.CodeDisplayNameInvalid, " TEST_new ": auth.CodeDisplayNameIsLogin,
		"ｔｅｓｔ TEST_maintainer": auth.CodeNameTaken,
	} {
		rec := en.reqJSON(http.MethodPost, auth.PasskeyRegisterOptionsPath, "",
			jsonBody(t, map[string]string{"invite": code, "loginName": "test_new", "displayName": display}))
		if errorCode(t, rec) != want {
			t.Errorf("display name %q: %d %s", display, rec.Code, rec.Body.String())
		}
	}
	rec := en.reqJSON(http.MethodPost, auth.PasskeyRegisterOptionsPath, "",
		jsonBody(t, map[string]string{"invite": code, "loginName": "test_new", "displayName": "Test\u202eName"}))
	if errorCode(t, rec) != auth.CodeDisplayNameInvalid {
		t.Errorf("display name with a bidi override: %s", rec.Body.String())
	}

	// Finishing with another (valid) code than the ceremony began with.
	other, _ := en.invite(inviter)
	a := &passkeytest.Authenticator{}
	rec = en.finishRegistration(a, other, en.registerOptions(code, "test_new", ""))
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != auth.CodePasskeyFailed {
		t.Errorf("finish with another code: %d %s", rec.Code, rec.Body.String())
	}
	// The invite is revoked between the two requests.
	o := en.registerOptions(code, "test_new", "")
	if err := auth.RevokeInvite(en.app, invite, inviter); err != nil {
		t.Fatal(err)
	}
	rec = en.finishRegistration(a, code, o)
	if rec.Code != http.StatusForbidden || errorCode(t, rec) != auth.CodeInviteInvalid {
		t.Errorf("finish with a revoked invite: %d %s", rec.Code, rec.Body.String())
	}
	// The login name is taken between the two requests.
	o = en.registerOptions(other, "test_race", "")
	en.member("test_race", auth.RoleMember)
	rec = en.finishRegistration(a, other, o)
	if rec.Code != http.StatusConflict || errorCode(t, rec) != auth.CodeLoginNameTaken {
		t.Errorf("finish with a login name taken meanwhile: %d %s", rec.Code, rec.Body.String())
	}
	// The display name is taken, as another member's pseudonym, between the two requests.
	o = en.registerOptions(other, "test_race2", "Test Raced Name")
	raced := en.member("test_race3", auth.RoleMember)
	raced.Set("pseudonym", "test raced name")
	if err := en.app.Save(raced); err != nil {
		t.Fatal(err)
	}
	rec = en.finishRegistration(a, other, o)
	if rec.Code != http.StatusConflict || errorCode(t, rec) != auth.CodeNameTaken {
		t.Errorf("finish with a display name taken meanwhile: %d %s", rec.Code, rec.Body.String())
	}
	if n, _ := en.app.CountRecords(store.Members); n != 3 {
		t.Errorf("%d members", n)
	}
	if n, _ := en.app.CountRecords(store.Credentials); n != 0 {
		t.Errorf("%d credentials after refused registrations", n)
	}

	// Without the X-OSFM header.
	r := httptest.NewRequest(http.MethodPost, auth.PasskeyRegisterOptionsPath, strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	en.h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("without X-OSFM: %d", w.Code)
	}
}

// TestPasskeySignInRefusals: a member who left or was deleted, an unknown passkey and a user handle that is not the
// member's all get the same 401.
func TestPasskeySignInRefusals(t *testing.T) {
	en := newEnv(t)
	inviter := en.member("test_maintainer", auth.RoleMaintainer)
	a, b := &passkeytest.Authenticator{}, &passkeytest.Authenticator{}
	code, _ := en.invite(inviter)
	en.registerPasskey(a, code, "test_left")
	code, _ = en.invite(inviter)
	en.registerPasskey(b, code, "test_deleted")

	var bodies []string
	refused := func(name string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		for _, c := range rec.Result().Cookies() {
			if c.Name == auth.CookieName && c.Value != "" {
				t.Errorf("%s: a session cookie was set", name)
			}
		}
		bodies = append(bodies, rec.Body.String())
	}

	// Unknown passkey: an authenticator whose credential was never registered.
	stranger := &passkeytest.Authenticator{}
	if _, err := stranger.Create([]byte(`{"publicKey":{"challenge":"AAAA","rp":{"id":"localhost"},"user":{"id":"AAAA"}}}`)); err != nil {
		t.Fatal(err)
	}
	refused("unknown passkey", en.passkeySignIn(stranger))

	// The right credential with another user handle.
	h := a.Credentials[0].UserHandle
	a.Credentials[0].UserHandle = b.Credentials[0].UserHandle
	refused("user handle of another member", en.passkeySignIn(a))
	a.Credentials[0].UserHandle = h

	left := en.memberByLogin("test_left")
	left.Set("left_at", types.NowDateTime())
	if err := en.app.Save(left); err != nil {
		t.Fatal(err)
	}
	refused("member who left", en.passkeySignIn(a))

	if err := store.SoftDelete(en.app, en.memberByLogin("test_deleted")); err != nil {
		t.Fatal(err)
	}
	refused("deleted member", en.passkeySignIn(b))

	for _, body := range bodies[1:] {
		if body != bodies[0] {
			t.Errorf("refusals differ: %s / %s", bodies[0], body)
		}
	}
}

// TestPasskeysUnavailable: a relying party configuration go-webauthn refuses (an IP address as the ID) makes the
// passkey routes answer 503 rather than keeping the instance from starting.
func TestPasskeysUnavailable(t *testing.T) {
	en := newEnvWith(t, auth.Options{PasskeyRPID: "192.0.2.10", PasskeyOrigins: []string{"https://192.0.2.10"}})
	for _, path := range []string{auth.PasskeySignInOptionsPath, auth.PasskeyRegisterOptionsPath} {
		rec := en.reqJSON(http.MethodPost, path, "", `{"invite":"x","loginName":"test_new"}`)
		if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != auth.CodePasskeysUnavailable {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
}
