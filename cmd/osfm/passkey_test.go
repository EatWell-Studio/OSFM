package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/auth/passkeytest"
	"github.com/EatWell-Studio/OSFM/internal/idscan"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// TestPasskeyResponsesHaveNoInternalIDs runs a whole passkey registration and sign-in against the app as `osfm serve`
// boots it, with the relying party derived from the default configuration (localhost, port 8090), and scans every
// response, the options with their user handle included, for the internal IDs of the seeded records and of every
// record the ceremonies created (member, credential, recovery codes, sessions).
func TestPasskeyResponsesHaveNoInternalIDs(t *testing.T) {
	app, seeded, _, _, h := bootForScan(t)
	a := &passkeytest.Authenticator{Origin: "http://localhost:8090"}

	type exchange struct {
		name string
		rec  *httptest.ResponseRecorder
	}
	var responses []exchange
	send := func(name, method, path, cookie string, body any, want int) *httptest.ResponseRecorder {
		t.Helper()
		var b []byte
		if body != nil {
			var err error
			if b, err = json.Marshal(body); err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest(method, path, strings.NewReader(string(b)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(auth.CSRFHeader, "1")
		if cookie != "" {
			r.Header.Set("Cookie", cookie)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != want {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		responses = append(responses, exchange{name, rec})
		return rec
	}
	var options struct {
		Options json.RawMessage `json:"options"`
		State   string          `json:"state"`
	}
	decodeInto := func(rec *httptest.ResponseRecorder, v any) {
		t.Helper()
		if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
			t.Fatal(err)
		}
	}
	cookieOf := func(rec *httptest.ResponseRecorder) string {
		t.Helper()
		for _, c := range rec.Result().Cookies() {
			if c.Name == auth.SessionCookieName(app) && c.Value != "" {
				return c.Name + "=" + c.Value
			}
		}
		t.Fatal("no session cookie")
		return ""
	}

	decodeInto(send("registration options", http.MethodPost, auth.PasskeyRegisterOptionsPath, "",
		map[string]string{"invite": idscan.InviteCode, "loginName": "test_passkey", "displayName": "Test Passkey"},
		http.StatusOK), &options)
	cred, err := a.Create(options.Options)
	if err != nil {
		t.Fatal(err)
	}
	rec := send("registration", http.MethodPost, auth.PasskeyRegisterPath, "",
		map[string]any{"invite": idscan.InviteCode, "state": options.State, "credential": json.RawMessage(cred)},
		http.StatusCreated)
	send("me after registration", http.MethodGet, auth.MePath, cookieOf(rec), nil, http.StatusOK)

	decodeInto(send("sign-in options", http.MethodPost, auth.PasskeySignInOptionsPath, "", nil, http.StatusOK), &options)
	assertion, err := a.Get(options.Options)
	if err != nil {
		t.Fatal(err)
	}
	rec = send("sign-in", http.MethodPost, auth.PasskeySignInPath, "",
		map[string]any{"state": options.State, "credential": json.RawMessage(assertion)}, http.StatusNoContent)
	send("me after sign-in", http.MethodGet, auth.MePath, cookieOf(rec), nil, http.StatusOK)

	ids := append([]string{}, seeded.IDs...)
	member, err := app.FindFirstRecordByData(store.Members, "login_name", "test_passkey")
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, member.Id)
	for _, collection := range []string{store.Credentials, store.RecoveryCodes, store.Sessions} {
		records, err := app.FindAllRecords(collection)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range records {
			ids = append(ids, r.Id)
		}
	}
	for _, x := range responses {
		for _, leak := range idscan.Find(x.rec.Header(), x.rec.Body.Bytes(), ids) {
			t.Errorf("%s: internal ID %s", x.name, leak)
		}
	}
}
