package migrations_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// collectionSpec is what a collection must look like after the migrations.
type collectionSpec struct {
	auth    bool
	fields  map[string]string // name → PocketBase field type
	indexes []string          // index names
}

var common = map[string]string{"id": core.FieldTypeText}

func fields(extra map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range common {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

const (
	tText     = core.FieldTypeText
	tNumber   = core.FieldTypeNumber
	tBool     = core.FieldTypeBool
	tDate     = core.FieldTypeDate
	tSelect   = core.FieldTypeSelect
	tJSON     = core.FieldTypeJSON
	tRelation = core.FieldTypeRelation
	tAutodate = core.FieldTypeAutodate
)

var wantCollections = map[string]collectionSpec{
	store.Members: {auth: true, fields: fields(map[string]string{
		"uuid": tText, "login_name": tText, "display_name": tText, "pseudonym": tText, "external_links": tJSON,
		"role": tSelect, "can_invite": tBool, "invite_allowed_from": tDate, "invited_by": tRelation,
		"joined_at": tDate, "left_at": tDate, "deleted_at": tDate, "created": tAutodate, "updated": tAutodate,
		"email": core.FieldTypeEmail, "password": core.FieldTypePassword, "tokenKey": tText,
		"emailVisibility": tBool, "verified": tBool,
	}), indexes: []string{"idx_members_uuid", "idx_members_login_name"}},
	store.Credentials: {fields: fields(map[string]string{
		"member": tRelation, "kind": tSelect, "credential_id": tText, "public_key": tText, "sign_count": tNumber,
		"device_name": tText, "password_hash": tText, "totp_secret_enc": tText, "totp_last_step": tNumber,
		"created": tAutodate, "updated": tAutodate,
	}), indexes: []string{"idx_credentials_member", "idx_credentials_credential_id"}},
	store.RecoveryCodes: {fields: fields(map[string]string{
		"member": tRelation, "code_hash": tText, "used_at": tDate, "created": tAutodate, "updated": tAutodate,
	}), indexes: []string{"idx_recovery_codes_member", "idx_recovery_codes_code_hash"}},
	store.Invites: {fields: fields(map[string]string{
		"uuid": tText, "code_hash": tText, "created_by": tRelation, "expires_at": tDate, "used_by": tRelation,
		"used_at": tDate, "revoked_at": tDate, "created": tAutodate, "updated": tAutodate,
	}), indexes: []string{"idx_invites_uuid", "idx_invites_code_hash", "idx_invites_created_by"}},
	store.ResetLinks: {fields: fields(map[string]string{
		"code_hash": tText, "member": tRelation, "created_by": tRelation, "expires_at": tDate, "used_at": tDate,
		"created": tAutodate, "updated": tAutodate,
	}), indexes: []string{"idx_reset_links_code_hash", "idx_reset_links_member"}},
	store.AuditLog: {fields: fields(map[string]string{
		"actor": tRelation, "action": tText, "target_type": tText, "target_uuid": tText, "details": tJSON,
		"created": tAutodate,
	}), indexes: []string{"idx_audit_log_created", "idx_audit_log_target", "idx_audit_log_actor"}},
	store.InstanceSettings: {fields: fields(map[string]string{
		"name": tText, "cities": tJSON, "report_threshold": tNumber, "invite_cooldown_days": tNumber,
		"geocoder_contact": tText, "created": tAutodate, "updated": tAutodate,
	})},
	store.Places: {fields: fields(map[string]string{
		"uuid": tText, "name": tText, "aliases": tJSON, "lat": tNumber, "lon": tNumber, "precision": tSelect,
		"position_note": tText, "landmark": tJSON, "osm_ref": tText, "category": tSelect, "cuisine": tJSON,
		"status": tSelect, "status_confirmed_at": tDate, "source_coord": tJSON, "deleted_at": tDate,
		"created": tAutodate, "updated": tAutodate,
	}), indexes: []string{"idx_places_uuid", "idx_places_lat_lon", "idx_places_osm_ref"}},
	store.Recommendations: {fields: fields(map[string]string{
		"uuid": tText, "place": tRelation, "author": tRelation, "display_as": tSelect, "attitude": tSelect,
		"visibility": tSelect, "text": tText, "dishes": tJSON, "time_slots": tSelect, "season": tText,
		"sources": tJSON, "visited_on": tDate, "hidden": tBool, "hidden_reason": tText,
		"edited_by_maintainer_at": tDate, "deleted_at": tDate, "created": tAutodate, "updated": tAutodate,
	}), indexes: []string{"idx_recommendations_uuid", "idx_recommendations_place", "idx_recommendations_author"}},
	store.RecommendationRevisions: {fields: fields(map[string]string{
		"recommendation": tRelation, "editor_role": tSelect, "editor": tRelation, "snapshot": tJSON,
		"created": tAutodate,
	}), indexes: []string{"idx_recommendation_revisions_recommendation"}},
	store.Reports: {fields: fields(map[string]string{
		"recommendation": tRelation, "reporter": tRelation, "reason": tText, "created": tAutodate,
	}), indexes: []string{"idx_reports_recommendation_reporter"}},
	store.Notices: {fields: fields(map[string]string{
		"member": tRelation, "kind": tText, "ref": tText, "read_at": tDate, "created": tAutodate,
	}), indexes: []string{"idx_notices_member"}},
}

// uniqueIndexes must be UNIQUE.
var uniqueIndexes = []string{
	"idx_members_uuid", "idx_members_login_name", "idx_credentials_credential_id", "idx_recovery_codes_code_hash",
	"idx_invites_uuid", "idx_invites_code_hash", "idx_reset_links_code_hash", "idx_places_uuid",
	"idx_recommendations_uuid", "idx_reports_recommendation_reporter",
}

func newApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	store.BindHooks(app)
	return app
}

func TestSchemaFromEmptyDataDir(t *testing.T) {
	app := newApp(t)
	checkSchema(t, app)
}

func checkSchema(t *testing.T, app core.App) {
	t.Helper()
	if _, err := app.FindCollectionByNameOrId("users"); err == nil {
		t.Error("PocketBase's users collection still exists")
	}

	for name, spec := range wantCollections {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Errorf("collection %s: %v", name, err)
			continue
		}
		if c.IsAuth() != spec.auth {
			t.Errorf("%s: auth = %v, want %v", name, c.IsAuth(), spec.auth)
		}
		for _, rule := range []*string{c.ListRule, c.ViewRule, c.CreateRule, c.UpdateRule, c.DeleteRule} {
			if rule != nil {
				t.Errorf("%s: API rule %q, want nil (superusers only)", name, *rule)
			}
		}
		if c.IsAuth() && c.ManageRule != nil {
			t.Errorf("%s: manage rule %q, want nil", name, *c.ManageRule)
		}
		got := map[string]string{}
		for _, f := range c.Fields {
			got[f.GetName()] = f.Type()
		}
		for field, typ := range spec.fields {
			if got[field] != typ {
				t.Errorf("%s.%s: type %q, want %q", name, field, got[field], typ)
			}
		}
		for field := range got {
			if _, ok := spec.fields[field]; !ok {
				t.Errorf("%s: unexpected field %s", name, field)
			}
		}
		for _, idx := range spec.indexes {
			var sqlText string
			err := app.DB().Select("sql").From("sqlite_master").
				Where(dbx.HashExp{"type": "index", "name": idx, "tbl_name": name}).Row(&sqlText)
			if err != nil {
				t.Errorf("%s: index %s missing: %v", name, idx, err)
				continue
			}
			if slices.Contains(uniqueIndexes, idx) != strings.HasPrefix(strings.ToUpper(sqlText), "CREATE UNIQUE INDEX") {
				t.Errorf("%s: index %s has the wrong uniqueness: %s", name, idx, sqlText)
			}
		}
	}

	// The places layer carries no member data (F1).
	places, err := app.FindCollectionByNameOrId(store.Places)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range places.Fields {
		if f.Type() == tRelation {
			t.Errorf("places.%s is a relation; places must not reference members", f.GetName())
		}
	}

	members, err := app.FindCollectionByNameOrId(store.Members)
	if err != nil {
		t.Fatal(err)
	}
	if email, _ := members.Fields.GetByName(core.FieldNameEmail).(*core.EmailField); email == nil || email.Required {
		t.Error("members.email must exist and be optional")
	}

	settings, err := app.FindRecordById(store.InstanceSettings, store.SettingsID)
	if err != nil {
		t.Fatalf("settings row: %v", err)
	}
	if settings.GetInt("report_threshold") != 3 || settings.GetInt("invite_cooldown_days") != 30 {
		t.Errorf("settings defaults: report_threshold %d, invite_cooldown_days %d",
			settings.GetInt("report_threshold"), settings.GetInt("invite_cooldown_days"))
	}
}

func TestSettingsHasOneRow(t *testing.T) {
	app := newApp(t)
	c, err := app.FindCollectionByNameOrId(store.InstanceSettings)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", store.SettingsID, "otherrecord0001"} {
		r := core.NewRecord(c)
		r.Id = id
		r.Set("report_threshold", 3)
		if err := app.Save(r); err == nil {
			t.Errorf("a second settings row with id %q was saved", id)
		} else {
			t.Logf("id %q rejected: %v", id, err)
		}
	}
	n, err := app.CountRecords(c)
	if err != nil || n != 1 {
		t.Errorf("settings rows: %d, %v; want 1", n, err)
	}
}

// snapshot is the schema of every collection without timestamps, to compare before and after.
func snapshot(t *testing.T, app core.App) string {
	t.Helper()
	cols, err := app.FindAllCollections()
	if err != nil {
		t.Fatal(err)
	}
	type schema struct {
		Name, Type                                             string
		Fields                                                 core.FieldsList
		Indexes                                                []string
		ListRule, ViewRule, CreateRule, UpdateRule, DeleteRule *string
	}
	var all []schema
	for _, c := range cols {
		all = append(all, schema{c.Name, c.Type, c.Fields, c.Indexes, c.ListRule, c.ViewRule, c.CreateRule,
			c.UpdateRule, c.DeleteRule})
	}
	slices.SortFunc(all, func(a, b schema) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		}
		return 0
	})
	out, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestUpAgainChangesNothing(t *testing.T) {
	app := newApp(t)
	before := snapshot(t, app)
	for _, m := range core.AppMigrations.Items() {
		if err := m.Up(app); err != nil {
			t.Fatalf("%s again: %v", m.File, err)
		}
	}
	if after := snapshot(t, app); after != before {
		t.Errorf("running the migrations again changed the schema\nbefore: %s\nafter:  %s", before, after)
	}
	n, err := app.CountRecords(store.InstanceSettings)
	if err != nil || n != 1 {
		t.Errorf("settings rows after running again: %d, %v", n, err)
	}
}

func TestDownAndUpAgain(t *testing.T) {
	app := newApp(t)
	runner := core.NewMigrationsRunner(app, core.AppMigrations)
	reverted, err := runner.Down(len(core.AppMigrations.Items()))
	if err != nil {
		t.Fatal(err)
	}
	if len(reverted) != len(core.AppMigrations.Items()) {
		t.Fatalf("reverted %v", reverted)
	}
	for name := range wantCollections {
		if _, err := app.FindCollectionByNameOrId(name); err == nil {
			t.Errorf("%s still exists after the down migrations", name)
		}
	}
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("users not restored: %v", err)
	}
	if users.Fields.GetByName("name") == nil || users.Fields.GetByName("login_name") != nil {
		t.Errorf("users fields not restored: %v", users.Fields.FieldNames())
	}

	if _, err := runner.Up(); err != nil {
		t.Fatal(err)
	}
	checkSchema(t, app)
}

func TestPlaceCategoriesAreTheSelectValues(t *testing.T) {
	app := newApp(t)
	c, err := app.FindCollectionByNameOrId(store.Places)
	if err != nil {
		t.Fatal(err)
	}
	category, _ := c.Fields.GetByName("category").(*core.SelectField)
	if category == nil || !slices.Equal(category.Values, store.PlaceCategories) {
		t.Errorf("places.category values: %v", category)
	}
}
