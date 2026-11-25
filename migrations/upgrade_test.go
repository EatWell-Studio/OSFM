package migrations_test

import (
	"archive/zip"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/migrations"
)

var uuidV7 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

var quiet = slog.New(slog.DiscardHandler)

// oldDataDir returns a pb_data directory as the previous release leaves it: PocketBase's system migrations only, with
// a record in its users collection and a file in storage.
func oldDataDir(t *testing.T) (dir, userID string) {
	t.Helper()
	dir = t.TempDir()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Fatal(err)
		}
	}()
	if _, err := app.FindCollectionByNameOrId(store.Members); err == nil {
		t.Fatal("members already exists before the OSFM migrations")
	}
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	user := core.NewRecord(users)
	user.Set("name", "Test Person")
	user.SetEmail("test@example.test")
	user.SetRandomPassword()
	if err := app.Save(user); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "storage"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "storage", "note.txt"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, user.Id
}

// start bootstraps an app on dir the way cmd/osfm does, and returns it bootstrapped.
func start(t *testing.T, dir string) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
	migrations.Bind(app)
	store.BindHooks(app)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	return app
}

func backups(t *testing.T, dir string) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, core.LocalBackupsDirName, migrations.BackupPrefix+"*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestFreshStartHasNoBackup(t *testing.T) {
	dir := t.TempDir()
	app := start(t, dir)
	checkSchema(t, app)
	if b := backups(t, dir); len(b) != 0 {
		t.Errorf("backups of a new database: %v", b)
	}
}

func TestUpgradeFromPreviousRelease(t *testing.T) {
	dir, userID := oldDataDir(t)

	app := start(t, dir)
	checkSchema(t, app)

	// The record that was in users is now a member with the required fields filled in.
	member, err := app.FindRecordById(store.Members, userID)
	if err != nil {
		t.Fatal(err)
	}
	if !uuidV7.MatchString(member.GetString(store.FieldUUID)) {
		t.Errorf("member uuid %q", member.GetString(store.FieldUUID))
	}
	if got := member.GetString("login_name"); got != "member-"+userID {
		t.Errorf("login_name %q", got)
	}
	if got := member.GetString("display_name"); got != "Test Person" {
		t.Errorf("display_name %q", got)
	}
	if got := member.GetString("role"); got != "member" {
		t.Errorf("role %q", got)
	}
	if member.GetDateTime("joined_at").IsZero() {
		t.Error("joined_at not set")
	}

	// The backup holds the database as it was before the OSFM migrations, and the rest of pb_data.
	list := backups(t, dir)
	if len(list) != 1 {
		t.Fatalf("backups: %v", list)
	}
	db := openBackupDB(t, list[0])
	var applied []string
	if err := db.Select("file").From(core.DefaultMigrationsTable).Column(&applied); err != nil {
		t.Fatal(err)
	}
	for _, m := range core.AppMigrations.Items() {
		for _, file := range applied {
			if file == m.File {
				t.Errorf("the backup already has %s applied", file)
			}
		}
	}
	var name string
	if err := db.Select("name").From("users").Where(dbx.HashExp{"id": userID}).Row(&name); err != nil {
		t.Fatalf("user in the backup: %v", err)
	}
	if name != "Test Person" {
		t.Errorf("user name in the backup: %q", name)
	}
	if got := zipEntry(t, list[0], "storage/note.txt"); got != "kept" {
		t.Errorf("storage file in the backup: %q", got)
	}

	// A further start has nothing to apply and takes no backup.
	if err := app.ClearBootstrap(); err != nil {
		t.Fatal(err)
	}
	start(t, dir)
	if b := backups(t, dir); len(b) != 1 {
		t.Errorf("backups after a start without pending migrations: %v", b)
	}
}

func TestBackupIsNotRepeatedForAnUnchangedDatabase(t *testing.T) {
	dir, _ := oldDataDir(t)
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	first, err := migrations.BackupBeforeMigrate(dir, now, quiet)
	if err != nil || first == "" {
		t.Fatalf("first backup: %q, %v", first, err)
	}
	if !strings.HasSuffix(first, "premigrate_20261125120000.zip") {
		t.Errorf("backup name %s", first)
	}
	// Same database, a minute later (a start that failed and is retried): the existing backup is kept.
	second, err := migrations.BackupBeforeMigrate(dir, now.Add(time.Minute), quiet)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Errorf("second backup %s, want the first one %s", second, first)
	}
	if b := backups(t, dir); len(b) != 1 {
		t.Errorf("backups: %v", b)
	}
}

func TestBackupOfMissingDatabase(t *testing.T) {
	path, err := migrations.BackupBeforeMigrate(t.TempDir(), time.Now(), quiet)
	if err != nil || path != "" {
		t.Errorf("BackupBeforeMigrate on an empty directory: %q, %v", path, err)
	}
}

// openBackupDB extracts data.db from the backup zip and opens it.
func openBackupDB(t *testing.T, zipPath string) *dbx.DB {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "data.db")
	if err := os.WriteFile(dest, []byte(zipEntry(t, zipPath, "data.db")), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := core.DefaultDBConnect(dest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func zipEntry(t *testing.T, zipPath, name string) string {
	t.Helper()
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	f, err := r.Open(name)
	if err != nil {
		t.Fatalf("%s in %s: %v", name, zipPath, err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
