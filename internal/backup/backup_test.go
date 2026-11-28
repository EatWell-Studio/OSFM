package backup_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/backup"
	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/export"
	"github.com/EatWell-Studio/OSFM/internal/pbtest"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// Unit tests of writing and checking backups. The whole round trip through the admin route, `osfm restore` and a
// start, with members signing in afterwards, is tested in cmd/osfm (backup_test.go).

func TestMain(m *testing.M) {
	code := m.Run()
	pbtest.RemoveTemplate()
	os.Exit(code)
}

var now = time.Date(2026, 11, 28, 12, 30, 0, 0, time.UTC)

// start returns a running app with the seeded made-up data and keys in its secrets directory, and its source.
func start(t *testing.T) (*testapp.App, backup.Source) {
	t.Helper()
	a := testapp.Start(t, testapp.Options{Bind: func(app core.App, _ *config.Config) { store.BindHooks(app) }})
	if err := a.Config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if _, err := config.EnsureKeys(a.Config.SecretsDir()); err != nil {
		t.Fatal(err)
	}
	return a, backup.Source{PBDataDir: a.Config.PBDataDir(), SecretsDir: a.Config.SecretsDir(), SoftwareVersion: "9.9.9"}
}

func newTarget(t *testing.T) backup.Target {
	t.Helper()
	dir := t.TempDir()
	return backup.Target{PBDataDir: filepath.Join(dir, "pb_data"), SecretsDir: filepath.Join(dir, "secrets")}
}

func entries(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = b
	}
	return out
}

func TestCreateAndRestore(t *testing.T) {
	a, src := start(t)
	dir := filepath.Join(src.PBDataDir, core.LocalBackupsDirName)
	path, m, err := backup.Create(t.Context(), dir, src, now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "osfm_20261128123000.zip" {
		t.Errorf("name %s", path)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode: %v, %v", info, err)
	}
	if m.Schema != backup.Schema || m.SoftwareVersion != "9.9.9" || !m.CreatedAt.Equal(now) {
		t.Errorf("manifest %+v", m)
	}
	if last := core.AppMigrations.Items()[len(core.AppMigrations.Items())-1].File; m.Migration != last {
		t.Errorf("migration %q, want %q", m.Migration, last)
	}
	// Nothing temporary is left behind.
	if left, _ := filepath.Glob(filepath.Join(src.PBDataDir, core.LocalTempDirName, "*")); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".*")); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}

	files := entries(t, path)
	var manifest backup.Manifest
	if err := json.Unmarshal(files[backup.ManifestName], &manifest); err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, f := range manifest.Files {
		listed = append(listed, f.Path)
		sum := sha256.Sum256(files[f.Path])
		if int64(len(files[f.Path])) != f.Size || hex.EncodeToString(sum[:]) != f.SHA256 {
			t.Errorf("%s does not match its manifest entry", f.Path)
		}
	}
	for name := range files {
		if name != backup.ManifestName && !slices.Contains(listed, name) {
			t.Errorf("%s is not in the manifest", name)
		}
	}
	key, err := os.ReadFile(filepath.Join(src.SecretsDir, config.MasterKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(files[backup.SecretsDir+config.MasterKeyFile], key) {
		t.Error("the master key in the backup differs")
	}
	// The layers are the circle's layers, as the export writes them.
	var want bytes.Buffer
	if err := export.WritePlaces(&want, a, export.Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(files[backup.PlacesName], want.Bytes()) {
		t.Errorf("places layer:\n%s\nwant\n%s", files[backup.PlacesName], want.Bytes())
	}
	if !strings.Contains(string(files[backup.RecommendsName]), a.Recommendation.GetString(store.FieldUUID)) {
		t.Error("the recommendations layer lacks the seeded recommendation")
	}

	// Two backups within one second.
	if _, _, err := backup.Create(t.Context(), dir, src, now); !errors.Is(err, backup.ErrExists) {
		t.Errorf("second backup in the same second: %v", err)
	}

	target := newTarget(t)
	r, err := backup.Restore(path, target, backup.RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Manifest.Migration != m.Migration || len(r.MovedAside) != 0 {
		t.Errorf("restored %+v", r)
	}
	if _, err := config.ReadKeys(target.SecretsDir); err != nil {
		t.Errorf("restored keys: %v", err)
	}
	for _, dir := range []string{target.PBDataDir, target.SecretsDir} {
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if info, err := d.Info(); err != nil || info.Mode().Perm() != 0o600 {
				t.Errorf("%s: %v %v", p, info.Mode(), err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if info, err := os.Stat(target.SecretsDir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("secrets directory: %v, %v", info, err)
	}
	restored, err := core.DefaultDBConnect(filepath.Join(target.PBDataDir, backup.DataDBName))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restored.Close() }()
	var uuids []string
	if err := restored.Select(store.FieldUUID).From(store.Recommendations).Column(&uuids); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(uuids, a.PrivateRecommendation.GetString(store.FieldUUID)) {
		t.Error("the private recommendation is missing from the restored database")
	}
}

func TestCreateNeedsDatabaseAndKeys(t *testing.T) {
	_, src := start(t)
	noKeys := src
	noKeys.SecretsDir = t.TempDir()
	if _, err := backup.Write(t.Context(), io.Discard, noKeys, now); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("without keys: %v", err)
	}
	noDB := src
	noDB.PBDataDir = t.TempDir()
	if _, err := backup.Write(t.Context(), io.Discard, noDB, now); !errors.Is(err, backup.ErrNoDatabase) {
		t.Errorf("without a database: %v", err)
	}
}

// addFutureMigration records a migration this program does not know in the database at path.
func addFutureMigration(t *testing.T, path string) {
	t.Helper()
	db, err := core.DefaultDBConnect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Insert(core.DefaultMigrationsTable, dbx.Params{"file": "9999999999_future.go",
		"applied": time.Now().UnixMicro()}).Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateRefusesNewerDatabase(t *testing.T) {
	a, src := start(t)
	if _, err := a.DB().Insert(core.DefaultMigrationsTable, dbx.Params{"file": "9999999999_future.go",
		"applied": time.Now().UnixMicro()}).Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Write(t.Context(), io.Discard, src, now); !errors.Is(err, backup.ErrNewerData) {
		t.Errorf("backup of a newer database: %v", err)
	}
}

// rewrite writes a copy of the zip at src with change applied to each entry (drop it by returning ok false) and the
// extra entries added. With fix, the manifest is made to match the new contents (and fix may change it further).
func rewrite(t *testing.T, src string, change func(name string, b []byte) ([]byte, bool), extra map[string][]byte,
	fix func(*backup.Manifest)) string {
	t.Helper()
	files := entries(t, src)
	out := map[string][]byte{}
	manifest := files[backup.ManifestName]
	for name, b := range files {
		if change != nil {
			var ok bool
			if b, ok = change(name, b); !ok {
				if name == backup.ManifestName {
					manifest = nil
				}
				continue
			}
		}
		if name != backup.ManifestName {
			out[name] = b
		}
	}
	for name, b := range extra {
		out[name] = b
	}
	if fix != nil {
		var m backup.Manifest
		if err := json.Unmarshal(manifest, &m); err != nil {
			t.Fatal(err)
		}
		m.Files = nil
		for _, name := range slices.Sorted(func(yield func(string) bool) {
			for name := range out {
				if !yield(name) {
					return
				}
			}
		}) {
			sum := sha256.Sum256(out[name])
			m.Files = append(m.Files, backup.File{Path: name, Size: int64(len(out[name])),
				SHA256: hex.EncodeToString(sum[:])})
		}
		fix(&m)
		var err error
		if manifest, err = json.Marshal(m); err != nil {
			t.Fatal(err)
		}
	}
	if manifest != nil {
		out[backup.ManifestName] = manifest
	}
	path := filepath.Join(t.TempDir(), "changed.zip")
	f, err := os.Create(path) // #nosec G304 -- a test directory
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, b := range out {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRestoreChecksTheBackup(t *testing.T) {
	_, src := start(t)
	path, _, err := backup.Create(t.Context(), t.TempDir(), src, now)
	if err != nil {
		t.Fatal(err)
	}
	newerDB := func() []byte {
		db := filepath.Join(t.TempDir(), "data.db")
		if err := os.WriteFile(db, entries(t, path)[backup.DataDBName], 0o600); err != nil {
			t.Fatal(err)
		}
		addFutureMigration(t, db)
		b, err := os.ReadFile(db) // #nosec G304 -- a test directory
		if err != nil {
			t.Fatal(err)
		}
		return b
	}()
	for _, tc := range []struct {
		name string
		path string
		want error
	}{
		{"no manifest (a PocketBase backup)", rewrite(t, path, func(name string, b []byte) ([]byte, bool) {
			return b, !strings.HasPrefix(name, backup.OSFMDir)
		}, nil, nil), backup.ErrInvalid},
		{"unknown format", rewrite(t, path, nil, nil, func(m *backup.Manifest) { m.Schema = "osfm-backup/2" }),
			backup.ErrInvalid},
		{"changed key", rewrite(t, path, func(name string, b []byte) ([]byte, bool) {
			if name == backup.SecretsDir+config.MasterKeyFile {
				b = bytes.Repeat([]byte{1}, len(b))
			}
			return b, true
		}, nil, nil), backup.ErrInvalid},
		{"unlisted file", rewrite(t, path, nil, map[string][]byte{"storage/extra.txt": []byte("x")}, nil),
			backup.ErrInvalid},
		{"unsafe name", rewrite(t, path, nil, map[string][]byte{"../evil.txt": []byte("x")},
			func(*backup.Manifest) {}), backup.ErrInvalid},
		{"no master key", rewrite(t, path, func(name string, b []byte) ([]byte, bool) {
			return b, name != backup.SecretsDir+config.MasterKeyFile
		}, nil, func(*backup.Manifest) {}), backup.ErrInvalid},
		{"invalid master key", rewrite(t, path, func(name string, b []byte) ([]byte, bool) {
			if name == backup.SecretsDir+config.MasterKeyFile {
				b = []byte("short")
			}
			return b, true
		}, nil, func(*backup.Manifest) {}), backup.ErrInvalid},
		{"manifest of another database", rewrite(t, path, nil, nil, func(m *backup.Manifest) {
			m.Migration = "1795600000_members_and_auth.go"
		}), backup.ErrInvalid},
		{"database of a newer version", rewrite(t, path, func(name string, b []byte) ([]byte, bool) {
			if name == backup.DataDBName {
				b = newerDB
			}
			return b, true
		}, nil, func(*backup.Manifest) {}), backup.ErrNewerData},
		{"not a zip", func() string {
			p := filepath.Join(t.TempDir(), "x.zip")
			if err := os.WriteFile(p, []byte("not a zip"), 0o600); err != nil {
				t.Fatal(err)
			}
			return p
		}(), backup.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := newTarget(t)
			if _, err := backup.Restore(tc.path, target, backup.RestoreOptions{}); !errors.Is(err, tc.want) {
				t.Fatalf("restore: %v, want %v", err, tc.want)
			}
			// Nothing was put in place, nothing temporary is left.
			left, _ := filepath.Glob(filepath.Join(filepath.Dir(target.PBDataDir), "*"))
			hidden, _ := filepath.Glob(filepath.Join(filepath.Dir(target.PBDataDir), ".*"))
			if len(left)+len(hidden) != 0 {
				t.Errorf("left behind: %v %v", left, hidden)
			}
		})
	}
}

func TestRestoreOverAnInstance(t *testing.T) {
	_, src := start(t)
	path, _, err := backup.Create(t.Context(), t.TempDir(), src, now)
	if err != nil {
		t.Fatal(err)
	}
	// Only backups (or nothing) in pb_data, and empty secrets: not an instance.
	target := newTarget(t)
	if err := os.MkdirAll(filepath.Join(target.PBDataDir, core.LocalBackupsDirName), 0o750); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(target.PBDataDir, core.LocalBackupsDirName, "premigrate_20260101000000.zip")
	if err := os.WriteFile(kept, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target.SecretsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Restore(path, target, backup.RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("the backups were not kept: %v", err)
	}
	// Now it holds one.
	if _, err := backup.Restore(path, target, backup.RestoreOptions{}); !errors.Is(err, backup.ErrNotEmpty) {
		t.Errorf("restore over an instance: %v", err)
	}
	r, err := backup.Restore(path, target, backup.RestoreOptions{Force: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{target.PBDataDir + ".before-restore-20261128123000",
		target.SecretsDir + ".before-restore-20261128123000"}
	if !slices.Equal(r.MovedAside, want) {
		t.Errorf("moved aside %v, want %v", r.MovedAside, want)
	}
	if _, err := os.Stat(filepath.Join(want[0], backup.DataDBName)); err != nil {
		t.Errorf("the previous database: %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("the backups did not move into the restored pb_data: %v", err)
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"osfm_20261128120000.zip", "premigrate_20261127000000.zip",
		"osfm_20261129000000.zip", "pb_backup_20261128.zip", ".osfm_20261128130000.zip.partial-1", "osfm_x.zip"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "osfm_20261128120000.zip"),
		filepath.Join(dir, "osfm_20261130000000.zip")); err != nil {
		t.Fatal(err)
	}
	list, err := backup.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range list {
		names = append(names, b.Name+" "+b.Kind)
	}
	want := []string{"osfm_20261129000000.zip osfm", "osfm_20261128120000.zip osfm",
		"premigrate_20261127000000.zip premigrate"}
	if !slices.Equal(names, want) {
		t.Errorf("list %v, want %v", names, want)
	}
	if list[0].Size != 4 || !list[0].CreatedAt.Equal(time.Date(2026, 11, 29, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("first %+v", list[0])
	}
	for _, name := range []string{"osfm_20261130000000.zip", "pb_backup_20261128.zip", "../osfm_20261128120000.zip",
		"osfm_20000101000000.zip"} {
		if _, err := backup.Stat(dir, name); !errors.Is(err, backup.ErrNotFound) {
			t.Errorf("Stat %s: %v", name, err)
		}
		if err := backup.Delete(dir, name); !errors.Is(err, backup.ErrNotFound) {
			t.Errorf("Delete %s: %v", name, err)
		}
	}
	if err := backup.Delete(dir, "osfm_20261128120000.zip"); err != nil {
		t.Fatal(err)
	}
	if list, _ := backup.List(dir); len(list) != 2 {
		t.Errorf("after deleting: %+v", list)
	}
	if list, err := backup.List(filepath.Join(dir, "missing")); err != nil || len(list) != 0 {
		t.Errorf("missing directory: %v, %v", list, err)
	}
}
