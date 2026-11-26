package migrations_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// seedRecords creates one valid record in every OSFM collection and returns their IDs by collection, and an auth
// token of the member and of a superuser.
func seedRecords(t *testing.T, app core.App) (ids map[string]string, memberToken, superuserToken string) {
	t.Helper()
	ids = map[string]string{store.InstanceSettings: store.SettingsID}
	save := func(collection string, values map[string]any) *core.Record {
		t.Helper()
		c, err := app.FindCollectionByNameOrId(collection)
		if err != nil {
			t.Fatal(err)
		}
		r := core.NewRecord(c)
		for k, v := range values {
			r.Set(k, v)
		}
		if c.IsAuth() {
			r.SetRandomPassword()
		}
		if err := app.Save(r); err != nil {
			t.Fatalf("seed %s: %v", collection, err)
		}
		ids[collection] = r.Id
		return r
	}

	member := save(store.Members, map[string]any{"login_name": "test_member", "display_name": "Test Member",
		"role": "member"})
	m := member.Id
	save(store.Credentials, map[string]any{"member": m, "kind": "passkey", "credential_id": "Y3JlZA"})
	save(store.RecoveryCodes, map[string]any{"member": m, "code_hash": "hash-recovery"})
	save(store.Invites, map[string]any{"code_hash": "hash-invite", "created_by": m, "expires_at": "2030-01-01 00:00:00Z"})
	save(store.ResetLinks, map[string]any{"code_hash": "hash-reset", "member": m, "created_by": m,
		"expires_at": "2030-01-01 00:00:00Z"})
	save(store.AuditLog, map[string]any{"actor": m, "action": "test.seed"})
	place := save(store.Places, map[string]any{"name": "Test Place", "lat": 31.23, "lon": 121.47,
		"precision": "exact", "category": "cafe", "status": "open"})
	rec := save(store.Recommendations, map[string]any{"place": place.Id, "author": m, "display_as": "main",
		"attitude": "good", "visibility": "circle", "text": "Test text"})
	save(store.RecommendationRevisions, map[string]any{"recommendation": rec.Id, "editor_role": "author",
		"editor": m, "snapshot": map[string]any{"text": "Old text"}})
	save(store.Reports, map[string]any{"recommendation": rec.Id, "reporter": m, "reason": "test"})
	save(store.Notices, map[string]any{"member": m, "kind": "test"})

	var err error
	if memberToken, err = member.NewAuthToken(); err != nil {
		t.Fatal(err)
	}
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
	if superuserToken, err = su.NewAuthToken(); err != nil {
		t.Fatal(err)
	}
	return ids, memberToken, superuserToken
}

// TestCollectionAPIIsClosed checks AD2: PocketBase's record API rejects every list, view, create, update and delete
// request on every OSFM collection unless it comes from a superuser — including from an authenticated member.
func TestCollectionAPIIsClosed(t *testing.T) {
	app := newApp(t)
	ids, memberToken, superuserToken := seedRecords(t, app)
	if r, err := app.FindAuthRecordByToken(memberToken, core.TokenTypeAuth); err != nil || r.Id != ids[store.Members] {
		t.Fatalf("the member token is not a valid auth token: %v", err)
	}

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	do := func(method, url, token, body string) int {
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}

	for name := range wantCollections {
		base := "/api/collections/" + name + "/records"
		one := base + "/" + ids[name]
		requests := []struct{ method, url, body string }{
			{http.MethodGet, base, ""},
			{http.MethodGet, one, ""},
			{http.MethodPost, base, `{}`},
			{http.MethodPatch, one, `{}`},
			{http.MethodDelete, one, ""},
		}
		for _, who := range []struct{ name, token string }{{"member", memberToken}, {"anonymous", ""}} {
			for _, r := range requests {
				if code := do(r.method, r.url, who.token, r.body); code != http.StatusForbidden {
					t.Errorf("%s %s %s: status %d, want 403", who.name, r.method, r.url, code)
				}
			}
		}
		// The route itself works: a superuser may list (so the 403 above comes from the rules).
		if code := do(http.MethodGet, base, superuserToken, ""); code != http.StatusOK {
			t.Errorf("superuser GET %s: status %d, want 200", base, code)
		}
		if _, err := app.FindRecordById(name, ids[name]); err != nil {
			t.Errorf("%s record gone after the rejected requests: %v", name, err)
		}
	}
}
