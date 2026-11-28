package main

import (
	"archive/zip"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/backup"
	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
	"github.com/EatWell-Studio/OSFM/migrations"
)

// lastM1Migration is the newest migration of the last M1 release (v0.1.0-rc.1); T023's migration came after it.
const lastM1Migration = "1795600600_credential_management.go"

// m1Instance returns the configuration of a stopped instance as the last M1 release leaves it: the migrations up to
// lastM1Migration applied, a member with a place and a recommendation (made up), and its keys.
func m1Instance(t *testing.T) (cfg *config.Config, ids map[string]string) {
	t.Helper()
	cfg = configFor(t, t.TempDir())
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if _, err := config.EnsureKeys(cfg.SecretsDir()); err != nil {
		t.Fatal(err)
	}
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: cfg.PBDataDir()})
	store.BindHooks(app)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.ClearBootstrap() }()
	var m1 core.MigrationsList
	for _, item := range core.AppMigrations.Items() {
		if item.File <= lastM1Migration {
			m1.Add(item)
		}
	}
	if _, err := core.NewMigrationsRunner(app, m1).Up(); err != nil {
		t.Fatal(err)
	}
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
		return r
	}
	member := save(store.Members, map[string]any{"login_name": "test_old", "display_name": "Test Old Member",
		"role": "maintainer"})
	place := save(store.Places, map[string]any{"name": "Test Old Noodles", "lat": 0.34, "lon": 7.06,
		"precision": "exact", "category": "restaurant", "status": "open"})
	rec := save(store.Recommendations, map[string]any{"place": place.Id, "author": member.Id, "display_as": "main",
		"attitude": "good", "visibility": "circle", "text": "Test old text"})
	return cfg, map[string]string{store.Members: member.GetString(store.FieldUUID),
		store.Places: place.GetString(store.FieldUUID), store.Recommendations: rec.GetString(store.FieldUUID)}
}

// TestRestoreOlderBackup: a backup of an instance of an older version (the last M1 release's migrations), taken with
// `osfm backup` while it is stopped, restores into a new data directory, and the next start migrates it to the
// current version after a backup before migrations, as an upgrade does.
func TestRestoreOlderBackup(t *testing.T) {
	old, ids := m1Instance(t)
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	if err := runBackup(t.Context(), old, old.PBDataDir(), "", io.Discard, io.Discard, now); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(old.PBDataDir(), core.LocalBackupsDirName, backup.Name(now))

	// The manifest names the M1 migration, and the zip holds the database as it was (not migrated). The layers were
	// exported from a migrated copy.
	m := readManifestOf(t, path)
	if m.Migration != lastM1Migration || m.Schema != backup.Schema {
		t.Errorf("manifest: %+v", m)
	}

	dir := t.TempDir()
	cfg := configFor(t, dir)
	if err := runRestore(cfg, cfg.PBDataDir(), path, nil, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	b := testapp.Start(t, testapp.Options{Bind: bind, DataDir: dir})
	for collection, id := range ids {
		r, err := store.FindByUUID(b, collection, id)
		if err != nil {
			t.Errorf("%s %s after the restore: %v", collection, id, err)
			continue
		}
		if collection == store.Members && r.GetString("display_name_key") == "" {
			t.Error("the member has no name key: the M2 migration did not run")
		}
	}
	var applied []string
	if err := b.DB().Select("file").From(core.DefaultMigrationsTable).Column(&applied); err != nil {
		t.Fatal(err)
	}
	for _, item := range core.AppMigrations.Items() {
		if !slices.Contains(applied, item.File) {
			t.Errorf("%s not applied after the restore", item.File)
		}
	}
	premigrate, err := filepath.Glob(filepath.Join(cfg.PBDataDir(), core.LocalBackupsDirName,
		migrations.BackupPrefix+"*.zip"))
	if err != nil || len(premigrate) != 1 {
		t.Errorf("backups before migrations: %v, %v", premigrate, err)
	}
}

func readManifestOf(t *testing.T, path string) backup.Manifest {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	f, err := zr.Open(backup.ManifestName)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var m backup.Manifest
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}
