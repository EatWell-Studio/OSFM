package migrations_test

import (
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

const m2ModelMigration = "1795602300_m2_model.go"

// m1DataDir returns a pb_data directory as the last M1 release (v0.1.0-rc.1) leaves it: every migration before T023's
// applied, with made-up rows in the collections whose new fields the T023 migration fills in.
func m1DataDir(t *testing.T) (dir string, ids map[string][]string) {
	t.Helper()
	dir = t.TempDir()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
	store.BindHooks(app)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Fatal(err)
		}
	}()
	var m1 core.MigrationsList
	found := false
	for _, item := range core.AppMigrations.Items() {
		if item.File == m2ModelMigration {
			found = true
			continue
		}
		m1.Add(item)
	}
	if !found {
		t.Fatalf("no migration %s", m2ModelMigration)
	}
	if _, err := core.NewMigrationsRunner(app, m1).Up(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FindCollectionByNameOrId(store.Members); err != nil {
		t.Fatal(err)
	}
	if c, _ := app.FindCollectionByNameOrId(store.Reports); c.Fields.GetByName("note") != nil {
		t.Fatal("reports.note exists before the T023 migration")
	}
	ids = seedM1Rows(t, app)
	return dir, ids
}

// seedM1Rows writes rows as an M1 release could hold them and returns their IDs by collection.
func seedM1Rows(t *testing.T, app core.App) map[string][]string {
	t.Helper()
	ids := map[string][]string{}
	save := func(collection string, values map[string]any) string {
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
		ids[collection] = append(ids[collection], r.Id)
		return r.Id
	}
	// Full-width letters, an ideographic space and padding: the key folds them.
	a := save(store.Members, map[string]any{"login_name": "test_alpha", "display_name": " Ｔｅｓｔ\u3000Ａｌｐｈａ ",
		"pseudonym": "Test Alias", "role": "member"})
	b := save(store.Members, map[string]any{"login_name": "test_beta", "display_name": "Test Beta", "role": "member"})
	place := save(store.Places, map[string]any{"name": "Test Soup Shop", "lat": 0.34, "lon": 7.06,
		"precision": "exact", "category": "restaurant", "status": "open"})
	rec := save(store.Recommendations, map[string]any{"place": place, "author": a, "display_as": "main",
		"attitude": "good", "visibility": "circle", "text": "Test text"})
	save(store.Recommendations, map[string]any{"place": place, "author": b, "display_as": "main",
		"attitude": "ok", "visibility": "circle", "text": "Test hidden text", "hidden": true,
		"hidden_reason": "Test reason"})
	for _, text := range []string{"Test old text one", "Test old text two"} {
		save(store.RecommendationRevisions, map[string]any{"recommendation": rec, "editor_role": "author",
			"editor": a, "snapshot": map[string]any{"text": text}})
	}
	save(store.Reports, map[string]any{"recommendation": rec, "reporter": b, "reason": "Test free-text reason"})
	save(store.Reports, map[string]any{"recommendation": rec, "reporter": a, "reason": strings.Repeat("x", 800)})
	save(store.Notices, map[string]any{"member": a, "kind": "test"})
	save(store.Notices, map[string]any{"member": b, "kind": "test"})
	settings, err := app.FindRecordById(store.InstanceSettings, store.SettingsID)
	if err != nil {
		t.Fatal(err)
	}
	settings.Set("name", "Test Circle")
	if err := app.Save(settings); err != nil {
		t.Fatal(err)
	}
	return ids
}

// TestUpgradeFromM1: starting on an M1 database applies the T023 migration after a backup, and the rows that existed
// before it get uuids, name keys, reason codes and hidden kinds.
func TestUpgradeFromM1(t *testing.T) {
	dir, ids := m1DataDir(t)
	app := start(t, dir)
	checkSchema(t, app)
	if b := backups(t, dir); len(b) != 1 {
		t.Errorf("backups: %v", b)
	}

	for _, name := range []string{store.Reports, store.RecommendationRevisions, store.Notices} {
		seen := map[string]bool{}
		for _, id := range ids[name] {
			r, err := app.FindRecordById(name, id)
			if err != nil {
				t.Fatal(err)
			}
			u := r.GetString(store.FieldUUID)
			if !uuidV7.MatchString(u) || seen[u] {
				t.Errorf("%s %s: uuid %q", name, id, u)
			}
			seen[u] = true
		}
	}

	alpha, err := app.FindRecordById(store.Members, ids[store.Members][0])
	if err != nil {
		t.Fatal(err)
	}
	if got := alpha.GetString(store.FieldDisplayNameKey); got != "test alpha" {
		t.Errorf("display_name_key %q", got)
	}
	if got := alpha.GetString(store.FieldPseudonymKey); got != "test alias" {
		t.Errorf("pseudonym_key %q", got)
	}
	beta, err := app.FindRecordById(store.Members, ids[store.Members][1])
	if err != nil {
		t.Fatal(err)
	}
	if beta.GetString(store.FieldDisplayNameKey) != "test beta" || beta.GetString(store.FieldPseudonymKey) != "" {
		t.Errorf("beta keys %q, %q", beta.GetString(store.FieldDisplayNameKey), beta.GetString(store.FieldPseudonymKey))
	}

	for i, wantNote := range []string{"Test free-text reason", strings.Repeat("x", store.MaxReportNoteLength)} {
		r, err := app.FindRecordById(store.Reports, ids[store.Reports][i])
		if err != nil {
			t.Fatal(err)
		}
		if r.GetString("reason") != store.ReasonOther || r.GetString("note") != wantNote ||
			r.GetString("resolution") != "" {
			t.Errorf("report %d: reason %q, note %q, resolution %q", i, r.GetString("reason"), r.GetString("note"),
				r.GetString("resolution"))
		}
		// The converted rows are valid records under the new schema.
		if err := app.Validate(r); err != nil {
			t.Errorf("report %d does not validate: %v", i, err)
		}
	}

	shown, err := app.FindRecordById(store.Recommendations, ids[store.Recommendations][0])
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := app.FindRecordById(store.Recommendations, ids[store.Recommendations][1])
	if err != nil {
		t.Fatal(err)
	}
	if shown.GetString("hidden_kind") != "" || hidden.GetString("hidden_kind") != store.HiddenByMaintainer ||
		hidden.GetString("hidden_reason") != "Test reason" {
		t.Errorf("hidden kinds: %q, %q (reason %q)", shown.GetString("hidden_kind"), hidden.GetString("hidden_kind"),
			hidden.GetString("hidden_reason"))
	}

	settings, err := app.FindRecordById(store.InstanceSettings, store.SettingsID)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.GetBool("geocoder_enabled") || settings.GetString("name") != "Test Circle" {
		t.Errorf("settings: geocoder_enabled %v, name %q", settings.GetBool("geocoder_enabled"),
			settings.GetString("name"))
	}
}

// TestUpgradeFromM1WithDuplicateRecommendations: an M1 database where a member has two live recommendations of one
// place cannot get the unique index; the migration fails with an error saying so instead of half-applying.
func TestUpgradeFromM1WithDuplicateRecommendations(t *testing.T) {
	app := newApp(t)
	runner := core.NewMigrationsRunner(app, core.AppMigrations)
	if _, err := runner.Down(1); err != nil {
		t.Fatal(err)
	}
	ids := seedM1Rows(t, app)
	c, err := app.FindCollectionByNameOrId(store.Recommendations)
	if err != nil {
		t.Fatal(err)
	}
	dup := core.NewRecord(c)
	dup.Load(map[string]any{"place": ids[store.Places][0], "author": ids[store.Members][0], "display_as": "pseudonym",
		"attitude": "must_try", "visibility": "circle", "text": "Test second text"})
	if err := app.Save(dup); err != nil {
		t.Fatal(err)
	}
	_, err = runner.Up()
	if err == nil || !strings.Contains(err.Error(), "more than one live recommendation") {
		t.Fatalf("up with duplicates: %v", err)
	}
	if c, _ := app.FindCollectionByNameOrId(store.Reports); c.Fields.GetByName("note") != nil {
		t.Error("the failed migration left reports.note behind")
	}
}

// TestM2ModelDown: the down migration removes what the up migration added, keeps a report's code and note in its
// free-text reason, and the up migration applies again.
func TestM2ModelDown(t *testing.T) {
	app := newApp(t)
	before := snapshot(t, app)
	runner := core.NewMigrationsRunner(app, core.AppMigrations)

	members, err := app.FindCollectionByNameOrId(store.Members)
	if err != nil {
		t.Fatal(err)
	}
	member := core.NewRecord(members)
	member.SetRandomPassword()
	member.Load(map[string]any{"login_name": "test_down", "display_name": "Test Down", "role": "member"})
	if err := app.Save(member); err != nil {
		t.Fatal(err)
	}
	report := seedRecommendationAndReport(t, app, member.Id)

	if _, err := runner.Down(1); err != nil {
		t.Fatal(err)
	}
	gone := map[string][]string{
		store.Recommendations:         {"hidden_kind", "hidden_at", "hidden_by"},
		store.Reports:                 {"uuid", "note", "resolved_at", "resolved_by", "resolution"},
		store.RecommendationRevisions: {"uuid"},
		store.Notices:                 {"uuid"},
		store.Members:                 {store.FieldDisplayNameKey, store.FieldPseudonymKey},
		store.InstanceSettings:        {"geocoder_enabled"},
	}
	for name, fields := range gone {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range fields {
			if c.Fields.GetByName(f) != nil {
				t.Errorf("%s.%s still exists after the down migration", name, f)
			}
		}
		for _, idx := range c.Indexes {
			for _, removed := range []string{"_place_author", "_name_key", "idx_reports_uuid", "revisions_uuid",
				"idx_notices_uuid"} {
				if strings.Contains(idx, removed) {
					t.Errorf("%s: index still there: %s", name, idx)
				}
			}
		}
	}
	places, err := app.FindCollectionByNameOrId(store.Places)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"category", "status"} {
		if !places.Fields.GetByName(f).(*core.SelectField).Required {
			t.Errorf("places.%s not required again", f)
		}
	}
	var reason string
	if err := app.DB().Select("reason").From(store.Reports).Where(dbx.HashExp{"id": report}).Row(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != "unverified_claim: Test note" {
		t.Errorf("reason after down: %q", reason)
	}

	if _, err := runner.Up(); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, app); after != before {
		t.Errorf("down and up changed the schema\nbefore: %s\nafter:  %s", before, after)
	}
}

// seedRecommendationAndReport writes a place, a recommendation of author and a report of it with a code and a note,
// and returns the report's ID.
func seedRecommendationAndReport(t *testing.T, app core.App, author string) string {
	t.Helper()
	place := newRecord(t, app, store.Places, map[string]any{"name": "Test Grill", "lat": 0.35, "lon": 7.07,
		"precision": "exact"})
	rec := newRecord(t, app, store.Recommendations, map[string]any{"place": place.Id, "author": author,
		"display_as": "main", "attitude": "good", "visibility": "circle", "text": "Test text"})
	return newRecord(t, app, store.Reports, map[string]any{"recommendation": rec.Id, "reporter": author,
		"reason": store.ReasonUnverifiedClaim, "note": "Test note"}).Id
}

func newRecord(t *testing.T, app core.App, collection string, values map[string]any) *core.Record {
	t.Helper()
	c, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}
	r := core.NewRecord(c)
	r.Load(values)
	if c.IsAuth() {
		r.SetRandomPassword()
	}
	if err := app.Save(r); err != nil {
		t.Fatalf("save %s: %v", collection, err)
	}
	return r
}

// TestOneLiveRecommendationPerMemberAndPlace: the database rejects a member's second live recommendation of a place
// (M2-Q1), and accepts a new one once the first is soft-deleted; other members and other places are not affected.
func TestOneLiveRecommendationPerMemberAndPlace(t *testing.T) {
	app := newApp(t)
	var indexSQL string
	if err := app.DB().Select("sql").From("sqlite_master").
		Where(dbx.HashExp{"type": "index", "name": "idx_recommendations_place_author"}).Row(&indexSQL); err != nil {
		t.Fatal(err)
	}
	// The condition must match how soft delete marks a live record (store.NotDeleted: deleted_at is empty).
	if !strings.HasSuffix(indexSQL, "WHERE `deleted_at` = ''") {
		t.Errorf("index: %s", indexSQL)
	}
	alice := newRecord(t, app, store.Members, map[string]any{"login_name": "test_alice",
		"display_name": "Test Alice", "role": "member"})
	bob := newRecord(t, app, store.Members, map[string]any{"login_name": "test_bob", "display_name": "Test Bob",
		"role": "member"})
	place := newRecord(t, app, store.Places, map[string]any{"name": "Test Bakery", "lat": 0.33, "lon": 7.05,
		"precision": "exact"})
	other := newRecord(t, app, store.Places, map[string]any{"name": "Test Deli", "lat": 0.32, "lon": 7.04,
		"precision": "approx"})
	rec := func(placeID, author, displayAs string) (*core.Record, error) {
		c, err := app.FindCollectionByNameOrId(store.Recommendations)
		if err != nil {
			t.Fatal(err)
		}
		r := core.NewRecord(c)
		r.Load(map[string]any{"place": placeID, "author": author, "display_as": displayAs, "attitude": "good",
			"visibility": "circle", "text": "Test text"})
		return r, app.Save(r)
	}

	first, err := rec(place.Id, alice.Id, "main")
	if err != nil {
		t.Fatal(err)
	}
	// Under the pseudonym too: one recommendation per member, whatever the name it is shown under.
	for _, displayAs := range []string{"main", "pseudonym"} {
		if _, err := rec(place.Id, alice.Id, displayAs); err == nil {
			t.Errorf("a second live recommendation (%s) of the same place was saved", displayAs)
		}
	}
	if _, err := rec(place.Id, bob.Id, "main"); err != nil {
		t.Errorf("another member's recommendation of the place: %v", err)
	}
	if _, err := rec(other.Id, alice.Id, "main"); err != nil {
		t.Errorf("the member's recommendation of another place: %v", err)
	}
	if err := store.SoftDelete(app, first); err != nil {
		t.Fatal(err)
	}
	if _, err := rec(place.Id, alice.Id, "main"); err != nil {
		t.Errorf("a new recommendation after the first was deleted: %v", err)
	}
	n, err := store.Query(app, store.Recommendations).Where(dbx.HashExp{"place": place.Id, "author": alice.Id}).Count()
	if err != nil || n != 1 {
		t.Errorf("live recommendations of alice at the place: %d, %v", n, err)
	}
}

// TestM2ModelFields: the new fields accept their values and reject others; a place is saved without a category or a
// status (seedRecommendationAndReport).
func TestM2ModelFields(t *testing.T) {
	app := newApp(t)
	m := newRecord(t, app, store.Members, map[string]any{"login_name": "test_fields",
		"display_name": "Test Fields", "role": "maintainer"})
	report := seedRecommendationAndReport(t, app, m.Id)

	r, err := app.FindRecordById(store.Reports, report)
	if err != nil {
		t.Fatal(err)
	}
	if !uuidV7.MatchString(r.GetString(store.FieldUUID)) {
		t.Errorf("new report uuid %q", r.GetString(store.FieldUUID))
	}
	for _, code := range store.ReportReasons {
		r.Set("reason", code)
		if err := app.Validate(r); err != nil {
			t.Errorf("reason %q rejected: %v", code, err)
		}
	}
	for _, bad := range []string{"", "Other", "spam", "other "} {
		r.Set("reason", bad)
		if err := app.Validate(r); err == nil {
			t.Errorf("reason %q accepted", bad)
		}
	}
	r.Set("reason", store.ReasonOther)
	r.Set("note", strings.Repeat("y", store.MaxReportNoteLength+1))
	if err := app.Validate(r); err == nil {
		t.Error("a note over the limit was accepted")
	}
	r.Set("note", "Test note")
	for _, res := range store.Resolutions {
		r.Set("resolution", res)
		if err := app.Validate(r); err != nil {
			t.Errorf("resolution %q rejected: %v", res, err)
		}
	}
	r.Set("resolution", "dismissed")
	if err := app.Validate(r); err == nil {
		t.Error("resolution dismissed accepted")
	}

	recs, err := store.Query(app, store.Recommendations).All()
	if err != nil || len(recs) != 1 {
		t.Fatalf("recommendations: %d, %v", len(recs), err)
	}
	for _, kind := range store.HiddenKinds {
		recs[0].Set("hidden_kind", kind)
		if err := app.Validate(recs[0]); err != nil {
			t.Errorf("hidden_kind %q rejected: %v", kind, err)
		}
	}
	recs[0].Set("hidden_kind", "author")
	if err := app.Validate(recs[0]); err == nil {
		t.Error("hidden_kind author accepted")
	}

	notice := newRecord(t, app, store.Notices, map[string]any{"member": m.Id, "kind": "test"})
	revision := newRecord(t, app, store.RecommendationRevisions, map[string]any{"recommendation": recs[0].Id,
		"editor_role": "author", "editor": m.Id, "snapshot": map[string]any{"text": "Test"}})
	for _, r := range []*core.Record{notice, revision} {
		if !uuidV7.MatchString(r.GetString(store.FieldUUID)) {
			t.Errorf("new %s uuid %q", r.Collection().Name, r.GetString(store.FieldUUID))
		}
	}
}
