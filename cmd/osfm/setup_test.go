package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pquerna/otp/totp"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/auth/passkeytest"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// TestSetupResponsesHaveNoInternalIDs runs the whole setup wizard against an unconfigured app as `osfm serve` boots
// it, once with password and TOTP and once with a passkey: the setup code from the file the server wrote, the first
// maintainer, the instance's name and a city. It scans every response for the internal IDs of every record created on
// the way and checks that the setup routes are gone afterwards and the setup code with them.
func TestSetupResponsesHaveNoInternalIDs(t *testing.T) {
	for _, method := range []string{"password", "passkey"} {
		t.Run(method, func(t *testing.T) {
			app, cfg, _, r := bootServed(t, false)
			h, err := r.BuildMux()
			if err != nil {
				t.Fatal(err)
			}
			codeFile := filepath.Join(cfg.SecretsDir(), auth.SetupCodeFileName)
			info, err := os.Stat(codeFile)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("setup code file mode %v", info.Mode().Perm())
			}
			code, err := auth.ReadSetupCode(app, codeFile)
			if err != nil {
				t.Fatal(err)
			}

			var responses []*httptest.ResponseRecorder
			send := func(method, path, cookie string, body any, want int) *httptest.ResponseRecorder {
				t.Helper()
				var b []byte
				if body != nil {
					if b, err = json.Marshal(body); err != nil {
						t.Fatal(err)
					}
				}
				req := httptest.NewRequest(method, path, strings.NewReader(string(b)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set(auth.CSRFHeader, "1")
				if cookie != "" {
					req.Header.Set("Cookie", cookie)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != want {
					t.Fatalf("%s %s: %d, want %d: %s", method, path, rec.Code, want, rec.Body.String())
				}
				responses = append(responses, rec)
				return rec
			}
			decodeInto := func(rec *httptest.ResponseRecorder, v any) {
				t.Helper()
				if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
					t.Fatal(err)
				}
			}

			var status auth.SetupStatusResponse
			decodeInto(send(http.MethodGet, auth.SetupPath, "", nil, http.StatusOK), &status)
			if status.Step != auth.SetupStepMaintainer {
				t.Fatalf("step %q", status.Step)
			}
			var check auth.SetupCheckResponse
			decodeInto(send(http.MethodPost, auth.SetupCheckPath, "", map[string]string{"code": code}, http.StatusOK),
				&check)
			if !check.Valid {
				t.Fatal("the setup code from the file is not valid")
			}

			var created *httptest.ResponseRecorder
			switch method {
			case "password":
				var start auth.RegisterPasswordStartResponse
				decodeInto(send(http.MethodPost, auth.SetupPasswordStartPath, "", auth.SetupPasswordStartRequest{
					SetupCode: code, LoginName: "test_first", DisplayName: "Test First", Password: "test password, made up",
				}, http.StatusOK), &start)
				totpCode, err := totp.GenerateCode(start.Secret, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				created = send(http.MethodPost, auth.SetupPasswordConfirmPath, "", auth.SetupPasswordConfirmRequest{
					SetupCode: code, Enrolment: start.Enrolment, Code: totpCode,
				}, http.StatusCreated)
			case "passkey":
				a := &passkeytest.Authenticator{Origin: "http://localhost:8090"}
				var options auth.PasskeyOptionsResponse
				rec := send(http.MethodPost, auth.SetupPasskeyOptionsPath, "", auth.SetupPasskeyOptionsRequest{
					SetupCode: code, LoginName: "test_first",
				}, http.StatusOK)
				var raw struct {
					Options json.RawMessage `json:"options"`
				}
				decodeInto(rec, &raw)
				decodeInto(rec, &options)
				cred, err := a.Create(raw.Options)
				if err != nil {
					t.Fatal(err)
				}
				created = send(http.MethodPost, auth.SetupPasskeyPath, "", auth.SetupPasskeyRequest{
					SetupCode: code, State: options.State, Credential: cred,
				}, http.StatusCreated)
			}
			cookie := ""
			for _, c := range created.Result().Cookies() {
				if c.Name == auth.SessionCookieName(app) && c.Value != "" {
					cookie = c.Name + "=" + c.Value
				}
			}
			if cookie == "" {
				t.Fatal("no session cookie after creating the first maintainer")
			}
			var me auth.Me
			decodeInto(send(http.MethodGet, auth.MePath, cookie, nil, http.StatusOK), &me)
			if me.Role != auth.RoleMaintainer {
				t.Errorf("role %q", me.Role)
			}

			// The code is gone with the first maintainer.
			if _, err := os.Stat(codeFile); !os.IsNotExist(err) {
				t.Errorf("the setup code file is still there: %v", err)
			}
			if _, err := auth.ReadSetupCode(app, codeFile); err == nil {
				t.Error("ReadSetupCode succeeded after setup")
			}
			send(http.MethodGet, auth.SetupPath, "", nil, http.StatusNotFound)
			decodeInto(send(http.MethodGet, auth.SetupPath, cookie, nil, http.StatusOK), &status)
			if status.Step != auth.SetupStepInstance {
				t.Fatalf("step %q", status.Step)
			}
			send(http.MethodGet, settings.InstancePath, cookie, nil, http.StatusOK)

			inst := settings.Instance{Name: "Test Circle", Cities: []settings.City{{
				Slug: "testtown", Name: "Test Town", BBox: [4]float64{1, 1, 2, 2}, Center: [2]float64{1.5, 1.5},
			}}}
			send(http.MethodPost, auth.SetupCompletePath, cookie, inst, http.StatusOK)
			send(http.MethodGet, settings.InstancePath, cookie, nil, http.StatusOK)

			// Every setup route is gone now, for everyone.
			for _, c := range []string{"", cookie} {
				send(http.MethodGet, auth.SetupPath, c, nil, http.StatusNotFound)
				send(http.MethodPost, auth.SetupCheckPath, c, map[string]string{"code": code}, http.StatusNotFound)
				send(http.MethodPost, auth.SetupPasswordStartPath, c, auth.SetupPasswordStartRequest{
					SetupCode: code, LoginName: "test_second", Password: "test password, made up",
				}, http.StatusNotFound)
				send(http.MethodPost, auth.SetupPasskeyOptionsPath, c, auth.SetupPasskeyOptionsRequest{
					SetupCode: code, LoginName: "test_second",
				}, http.StatusNotFound)
			}
			send(http.MethodPost, auth.SetupCompletePath, cookie, inst, http.StatusNotFound)

			var ids []string
			for _, collection := range []string{store.Members, store.Credentials, store.RecoveryCodes, store.Sessions,
				store.AuditLog, store.InstanceSettings} {
				records, err := app.FindAllRecords(collection)
				if err != nil {
					t.Fatal(err)
				}
				for _, rec := range records {
					ids = append(ids, rec.Id)
				}
				c, err := app.FindCollectionByNameOrId(collection)
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, c.Id)
			}
			for i, rec := range responses {
				for _, leak := range idscan.Find(rec.Header(), rec.Body.Bytes(), ids) {
					t.Errorf("response %d: internal ID %s", i, leak)
				}
			}
			auditActions(t, app, auth.AuditSetupMaintainer, auth.AuditSettingsUpdate, auth.AuditSetupComplete)
		})
	}
}

// auditActions checks that the audit log has an entry for each action.
func auditActions(t *testing.T, app core.App, actions ...string) {
	t.Helper()
	for _, action := range actions {
		if _, err := app.FindFirstRecordByData(store.AuditLog, "action", action); err != nil {
			t.Errorf("no audit log entry %s: %v", action, err)
		}
	}
}
