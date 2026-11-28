package auth_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/auth/passkeytest"
	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Made-up values only.
const (
	// otherSetupCode has the right form but is never the current setup code.
	otherSetupCode = "0000-0000-0000-0000"
	// testInstanceName has a colon, which may not appear in a TOTP issuer, and CJK characters.
	testInstanceName = "测试圈: Test Circle"
)

func (pe *pwEnv) savedName() string {
	pe.t.Helper()
	inst, err := settings.Load(pe.app)
	if err != nil {
		pe.t.Fatal(err)
	}
	return inst.Name
}

func (pe *pwEnv) setupStep(token string) (int, string) {
	pe.t.Helper()
	rec := pe.reqJSON(http.MethodGet, auth.SetupPath, token, "")
	if rec.Code != http.StatusOK {
		return rec.Code, ""
	}
	return rec.Code, decode[auth.SetupStatusResponse](pe.t, rec).Step
}

func (pe *pwEnv) maintainers() int64 {
	pe.t.Helper()
	n, err := pe.app.CountRecords(store.Members, dbx.HashExp{"role": auth.RoleMaintainer})
	if err != nil {
		pe.t.Fatal(err)
	}
	return n
}

// TestSetupWithPassword: the setup wizard creates the first maintainer with password and TOTP; it needs the current
// setup code at every step, and once the maintainer exists every route that creates one answers 404.
func TestSetupWithPassword(t *testing.T) {
	pe := newPWEnv(t)
	code := auth.NewSetupCode(t, pe.app)

	if status, step := pe.setupStep(""); status != http.StatusOK || step != auth.SetupStepMaintainer {
		t.Fatalf("setup status: %d %q", status, step)
	}
	startReq := func(setupCode string) auth.SetupPasswordStartRequest {
		return auth.SetupPasswordStartRequest{SetupCode: setupCode, InstanceName: testInstanceName,
			LoginName: " Test_First ", DisplayName: "Test First",
			Password: testPassword}
	}
	for _, c := range []string{"", "x", otherSetupCode, code + "0"} {
		expectError(t, "start with code "+c, pe.post(auth.SetupPasswordStartPath, "", startReq(c)),
			http.StatusForbidden, auth.CodeSetupCodeInvalid)
	}
	// The instance name is required, by the rules of the settings.
	for _, name := range []string{"", "  ", "Test\u0007Circle", strings.Repeat("x", 101)} {
		req := startReq(code)
		req.InstanceName = name
		// From a client of its own, so that the requests count towards no other client's limits.
		expectError(t, "start with instance name "+name, pe.postFrom(client(9), auth.SetupPasswordStartPath, req),
			http.StatusBadRequest, auth.CodeInstanceNameInvalid)
	}
	// Case, spaces and dashes do not matter, as for recovery codes.
	rec := pe.post(auth.SetupPasswordStartPath, "", startReq(strings.ToLower(strings.ReplaceAll(code, "-", " "))))
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	start := decode[auth.RegisterPasswordStartResponse](t, rec)
	// The TOTP issuer is the instance name; a colon, which separates issuer and account in the label, becomes a space.
	uri, err := url.Parse(start.OTPAuthURI)
	if err != nil {
		t.Fatal(err)
	}
	const issuer = "测试圈 Test Circle"
	if got := uri.Query().Get("issuer"); got != issuer {
		t.Errorf("issuer %q in %s", got, start.OTPAuthURI)
	}
	if uri.Path != "/"+issuer+":test_first" || strings.ContainsAny(uri.RawPath+uri.RawQuery, " 测") {
		t.Errorf("label %q (raw %q) in %s", uri.Path, uri.RawPath, start.OTPAuthURI)
	}
	if pe.savedName() != "" {
		t.Error("the start saved the instance name")
	}

	confirm := func(setupCode string) auth.SetupPasswordConfirmRequest {
		return auth.SetupPasswordConfirmRequest{SetupCode: setupCode, Enrolment: start.Enrolment,
			Code: pe.code(start.Secret, 0)}
	}
	expectError(t, "confirm with another code", pe.post(auth.SetupPasswordConfirmPath, "", confirm(otherSetupCode)),
		http.StatusForbidden, auth.CodeSetupCodeInvalid)
	// An enrolment of the setup is no registration with an invite.
	expectError(t, "confirm as an invited registration", pe.post(auth.RegisterPasswordConfirmPath, "",
		auth.RegisterPasswordConfirmRequest{Invite: code, Enrolment: start.Enrolment, Code: pe.code(start.Secret, 0)}),
		http.StatusBadRequest, auth.CodeEnrolmentExpired)
	if pe.maintainers() != 0 || pe.savedName() != "" {
		t.Fatal("a refused confirmation created a maintainer or saved the name")
	}

	// The name sealed into the enrolment is saved, whatever the confirmation says.
	body := map[string]string{"setupCode": code, "enrolment": start.Enrolment, "code": pe.code(start.Secret, 0),
		"instanceName": "Test Other Name"}
	rec = pe.post(auth.SetupPasswordConfirmPath, "", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
	}
	registered := decode[auth.RegisteredResponse](t, rec)
	if registered.Me.Role != auth.RoleMaintainer || len(registered.RecoveryCodes) != auth.RecoveryCodeCount {
		t.Errorf("registered: %+v", registered)
	}
	token := responseCookie(rec).Value
	m := pe.memberByLogin("test_first")
	if m.GetString("role") != auth.RoleMaintainer || m.GetString("invited_by") != "" || !m.GetBool("can_invite") ||
		m.GetDateTime("joined_at").IsZero() {
		t.Errorf("first maintainer: role %q, invited_by %q, can_invite %v", m.GetString("role"),
			m.GetString("invited_by"), m.GetBool("can_invite"))
	}
	entry, err := pe.app.FindFirstRecordByData(store.AuditLog, "action", auth.AuditSetupMaintainer)
	if err != nil {
		t.Fatal(err)
	}
	if entry.GetString("target_uuid") != m.GetString(store.FieldUUID) ||
		entry.GetString("details") != `{"method":"password_totp"}` {
		t.Errorf("audit entry: %s %s", entry.GetString("target_uuid"), entry.GetString("details"))
	}
	if got := pe.savedName(); got != testInstanceName {
		t.Errorf("saved instance name %q", got)
	}
	if entry, err := pe.app.FindFirstRecordByData(store.AuditLog, "action", auth.AuditSettingsUpdate); err != nil ||
		entry.GetString("actor") != m.Id {
		t.Errorf("settings audit entry: %v", err)
	}

	// The maintainer can sign in like anyone else.
	ticket := pe.ticket("test_first", testPassword)
	pe.advance(30e9)
	if rec := pe.post(auth.SignInTOTPPath, "", auth.SignInCodeRequest{Ticket: ticket,
		Code: pe.code(start.Secret, 0)}); rec.Code != http.StatusOK {
		t.Errorf("sign-in: %d %s", rec.Code, rec.Body.String())
	}

	// Setup is over for everyone but this maintainer, who still has to give the instance its cities.
	if status, _ := pe.setupStep(""); status != http.StatusNotFound {
		t.Errorf("setup status without a session: %d", status)
	}
	if status, step := pe.setupStep(token); status != http.StatusOK || step != auth.SetupStepCities {
		t.Errorf("setup status of the maintainer: %d %q", status, step)
	}
	for _, path := range []string{auth.SetupPasswordStartPath, auth.SetupPasswordConfirmPath, auth.SetupCheckPath,
		auth.SetupPasskeyOptionsPath, auth.SetupPasskeyPath} {
		for _, body := range []any{startReq(code), confirm(code), map[string]string{"code": code}} {
			expectError(t, path, pe.post(path, "", body), http.StatusNotFound, auth.CodeSetupDone)
		}
		if rec := pe.reqJSON(http.MethodPost, path, "", "{"); rec.Code != http.StatusNotFound {
			t.Errorf("%s with a malformed body: %d", path, rec.Code)
		}
	}
	if n := pe.maintainers(); n != 1 {
		t.Errorf("%d maintainers", n)
	}
}

// TestSetupWithPasskey: the first maintainer with a passkey, which signs in afterwards.
func TestSetupWithPasskey(t *testing.T) {
	pe := newPWEnv(t)
	code := auth.NewSetupCode(t, pe.app)
	a := &passkeytest.Authenticator{}

	options := func(setupCode string) string {
		return jsonBody(t, auth.SetupPasskeyOptionsRequest{SetupCode: setupCode, InstanceName: "Test Circle",
			LoginName: "test_first", DisplayName: "Test First"})
	}
	if rec := pe.reqJSON(http.MethodPost, auth.SetupPasskeyOptionsPath, "", options(otherSetupCode)); rec.Code !=
		http.StatusForbidden || errorCode(t, rec) != auth.CodeSetupCodeInvalid {
		t.Fatalf("options with another code: %d %s", rec.Code, rec.Body.String())
	}
	rec := pe.reqJSON(http.MethodPost, auth.SetupPasskeyOptionsPath, "", options(code))
	if rec.Code != http.StatusOK {
		t.Fatalf("options: %d %s", rec.Code, rec.Body.String())
	}
	o := decode[optionsResponse](t, rec)
	cred, err := a.Create(o.Options)
	if err != nil {
		t.Fatal(err)
	}
	finish := func(setupCode, state string) string {
		return jsonBody(t, auth.SetupPasskeyRequest{SetupCode: setupCode, State: state, Credential: cred})
	}
	if rec := pe.reqJSON(http.MethodPost, auth.SetupPasskeyPath, "", finish(otherSetupCode, o.State)); rec.Code !=
		http.StatusForbidden {
		t.Fatalf("finish with another code: %d %s", rec.Code, rec.Body.String())
	}
	// A setup ceremony's state is not one of an invited registration.
	if rec := pe.reqJSON(http.MethodPost, auth.PasskeyRegisterPath, "", jsonBody(t, map[string]any{
		"invite": code, "state": o.State, "credential": cred})); rec.Code != http.StatusBadRequest {
		t.Fatalf("the setup state at the invited registration: %d %s", rec.Code, rec.Body.String())
	}
	// That attempt used the state up; a new ceremony.
	rec = pe.reqJSON(http.MethodPost, auth.SetupPasskeyOptionsPath, "", options(code))
	o = decode[optionsResponse](t, rec)
	if cred, err = a.Create(o.Options); err != nil {
		t.Fatal(err)
	}
	rec = pe.reqJSON(http.MethodPost, auth.SetupPasskeyPath, "", finish(code, o.State))
	if rec.Code != http.StatusCreated {
		t.Fatalf("finish: %d %s", rec.Code, rec.Body.String())
	}
	if codes := decode[auth.PasskeyRegisterResponse](t, rec).RecoveryCodes; len(codes) != auth.RecoveryCodeCount {
		t.Errorf("%d recovery codes", len(codes))
	}
	sessionToken(t, rec)
	if m := pe.memberByLogin("test_first"); m.GetString("role") != auth.RoleMaintainer {
		t.Errorf("role %q", m.GetString("role"))
	}
	if got := pe.savedName(); got != "Test Circle" {
		t.Errorf("saved instance name %q", got)
	}
	if rec := pe.passkeySignIn(a); rec.Code != http.StatusNoContent {
		t.Errorf("passkey sign-in: %d %s", rec.Code, rec.Body.String())
	}
	if rec := pe.reqJSON(http.MethodPost, auth.SetupPasskeyOptionsPath, "", options(code)); rec.Code !=
		http.StatusNotFound {
		t.Errorf("options after setup: %d", rec.Code)
	}
}

// TestConcurrentSetup: two creations of the first maintainer with the setup code at the same time; exactly one
// succeeds, the other finds the maintainer.
func TestConcurrentSetup(t *testing.T) {
	for round := range 5 {
		pe := newPWEnv(t)
		code := auth.NewSetupCode(t, pe.app)
		start := make(chan struct{})
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i := range errs {
			wg.Go(func() {
				<-start
				_, errs[i] = auth.RegisterFirstMaintainer(pe.app, code, "Test Circle", "test",
					registerAs(fmt.Sprintf("test_r%d_%d", round, i)))
			})
		}
		close(start)
		wg.Wait()
		won := 0
		for _, err := range errs {
			switch {
			case err == nil:
				won++
			case !errors.Is(err, auth.ErrSetupDone) && !errors.Is(err, auth.ErrInvalidSetupCode):
				t.Errorf("round %d: %v", round, err)
			}
		}
		if won != 1 || pe.maintainers() != 1 {
			t.Fatalf("round %d: %d succeeded (%v), %d maintainers", round, won, errs, pe.maintainers())
		}
	}
}

// TestSetupCodeCheck: the check answers only whether the code is the current one; a new code replaces the old.
func TestSetupCodeCheck(t *testing.T) {
	pe := newPWEnv(t)
	old := auth.NewSetupCode(t, pe.app)
	code := auth.NewSetupCode(t, pe.app)
	check := func(c string) bool {
		t.Helper()
		rec := pe.post(auth.SetupCheckPath, "", auth.SetupCheckRequest{Code: c})
		if rec.Code != http.StatusOK {
			t.Fatalf("check %q: %d %s", c, rec.Code, rec.Body.String())
		}
		return decode[auth.SetupCheckResponse](t, rec).Valid
	}
	if !check(code) || !check(strings.ToLower(code)) {
		t.Error("the current code is not valid")
	}
	for _, c := range []string{old, "", "x", otherSetupCode} {
		if check(c) {
			t.Errorf("%q is valid", c)
		}
	}
}

// TestCompleteSetup: the maintainer names the instance and gives it its cities; then the setup is complete, and the
// wizard's routes answer 404 to the maintainer too.
func TestCompleteSetup(t *testing.T) {
	pe := newPWEnv(t)
	code := auth.NewSetupCode(t, pe.app)
	maintainer, err := auth.RegisterFirstMaintainer(pe.app, code, "Test Circle", "test", registerAs("test_first"))
	if err != nil {
		t.Fatal(err)
	}
	token := pe.session(maintainer)
	member := pe.member("test_member", auth.RoleMember)

	city := settings.City{Slug: "test-town", Name: " Test Town ", BBox: [4]float64{10, 20, 10.5, 20.5},
		Center: [2]float64{10.2, 20.2}}
	// Without a name, the one saved with the maintainer stays.
	valid := settings.Instance{Cities: []settings.City{city}}
	if rec := pe.post(auth.SetupCompletePath, pe.session(member), valid); rec.Code != http.StatusForbidden {
		t.Errorf("a member: %d", rec.Code)
	}
	if rec := pe.post(auth.SetupCompletePath, "", valid); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", rec.Code)
	}
	with := func(f func(c *settings.City)) settings.Instance {
		c := city
		f(&c)
		return settings.Instance{Name: "Test Circle", Cities: []settings.City{c}}
	}
	for name, tc := range map[string]struct {
		inst settings.Instance
		code string
	}{
		"control char":    {settings.Instance{Name: "Test\u0007", Cities: valid.Cities}, auth.CodeInstanceNameInvalid},
		"long name":       {settings.Instance{Name: strings.Repeat("x", 101), Cities: valid.Cities}, auth.CodeInstanceNameInvalid},
		"no cities":       {settings.Instance{Name: "Test Circle"}, auth.CodeCitiesInvalid},
		"bad slug":        {with(func(c *settings.City) { c.Slug = "Test Town" }), auth.CodeCitiesInvalid},
		"no city name":    {with(func(c *settings.City) { c.Name = "" }), auth.CodeCitiesInvalid},
		"empty bbox":      {with(func(c *settings.City) { c.BBox = [4]float64{10, 20, 10, 20.5} }), auth.CodeCitiesInvalid},
		"bbox off earth":  {with(func(c *settings.City) { c.BBox = [4]float64{170, 20, 190, 21} }), auth.CodeCitiesInvalid},
		"center outside":  {with(func(c *settings.City) { c.Center = [2]float64{11, 20.2} }), auth.CodeCitiesInvalid},
		"duplicate slugs": {settings.Instance{Name: "x", Cities: []settings.City{city, city}}, auth.CodeCitiesInvalid},
	} {
		expectError(t, name, pe.post(auth.SetupCompletePath, token, tc.inst), http.StatusBadRequest, tc.code)
	}

	rec := pe.post(auth.SetupCompletePath, token, valid)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body.String())
	}
	saved, err := settings.Load(pe.app)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Name != "Test Circle" || len(saved.Cities) != 1 || saved.Cities[0].Name != "Test Town" ||
		saved.Cities[0].Slug != "test-town" {
		t.Errorf("saved: %+v", saved)
	}
	for _, action := range []string{auth.AuditSettingsUpdate, auth.AuditSetupComplete} {
		entry, err := pe.app.FindFirstRecordByData(store.AuditLog, "action", action)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if entry.GetString("actor") != maintainer.Id {
			t.Errorf("%s: actor %q", action, entry.GetString("actor"))
		}
	}

	if status, _ := pe.setupStep(token); status != http.StatusNotFound {
		t.Errorf("setup status after completion: %d", status)
	}
	expectError(t, "complete again", pe.post(auth.SetupCompletePath, token, valid), http.StatusNotFound,
		auth.CodeSetupDone)
	expectError(t, "complete again with bad input", pe.post(auth.SetupCompletePath, token, settings.Instance{}),
		http.StatusNotFound, auth.CodeSetupDone)
}

// TestSetupRoutesRateLimited: wrong setup codes count as failures of the client, so guessing is locked out like the
// sign-in; another client is not affected.
func TestSetupRoutesRateLimited(t *testing.T) {
	pe := newPWEnv(t)
	code := auth.NewSetupCode(t, pe.app)
	limits := auth.DefaultLimits()
	for n := range limits.ClientFailures.Threshold {
		var rec *httptest.ResponseRecorder
		if n%2 == 0 {
			rec = pe.postFrom(client(1), auth.SetupCheckPath, auth.SetupCheckRequest{Code: otherSetupCode})
		} else {
			rec = pe.postFrom(client(1), auth.SetupPasswordStartPath, auth.SetupPasswordStartRequest{
				SetupCode: otherSetupCode, InstanceName: "Test Circle", LoginName: "test_first", DisplayName: "Test First",
				Password: "made up password",
			})
		}
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("failure %d: 429 before the threshold", n+1)
		}
	}
	expectLimited(t, "client locked", pe.postFrom(client(1), auth.SetupCheckPath, auth.SetupCheckRequest{Code: code}),
		"60")
	rec := pe.postFrom(client(2), auth.SetupCheckPath, auth.SetupCheckRequest{Code: code})
	if rec.Code != http.StatusOK || !decode[auth.SetupCheckResponse](t, rec).Valid {
		t.Errorf("another client: %d %s", rec.Code, rec.Body.String())
	}
}

// TestFirstMaintainerAndNameTogether: the instance name is saved in the transaction that creates the first maintainer;
// a creation that fails leaves no name, and an invalid name creates no maintainer.
func TestFirstMaintainerAndNameTogether(t *testing.T) {
	pe := newPWEnv(t)
	code := auth.NewSetupCode(t, pe.app)
	failing := func(tx core.App, m *core.Record) error {
		if err := registerAs("test_first")(tx, m); err != nil {
			return err
		}
		return errors.New("test failure after saving the member")
	}
	if _, err := auth.RegisterFirstMaintainer(pe.app, code, "Test Circle", "test", failing); err == nil {
		t.Fatal("a failing registration succeeded")
	}
	if _, err := auth.RegisterFirstMaintainer(pe.app, code, " ", "test", registerAs("test_first")); !errors.Is(err,
		settings.ErrInvalidName) {
		t.Fatalf("an empty name: %v", err)
	}
	if pe.maintainers() != 0 || pe.savedName() != "" {
		t.Fatalf("after failures: %d maintainers, name %q", pe.maintainers(), pe.savedName())
	}
	if _, err := auth.RegisterFirstMaintainer(pe.app, code, " Test Circle ", "test", registerAs("test_first")); err != nil {
		t.Fatal(err)
	}
	if pe.maintainers() != 1 || pe.savedName() != "Test Circle" {
		t.Fatalf("%d maintainers, name %q", pe.maintainers(), pe.savedName())
	}
}
