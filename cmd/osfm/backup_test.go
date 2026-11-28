package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/spf13/cobra"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/backup"
	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// configFor returns the configuration of an instance with its data directory at dir, as the tests boot them.
func configFor(t *testing.T, dir string) *config.Config {
	t.Helper()
	cfg, err := config.Parse(testapp.UnreachableSearch(func(name string) (string, bool) {
		if name == config.EnvDataDir {
			return dir, true
		}
		return "", false
	}))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// createBackup creates a backup through the route as a freshly signed-in maintainer and returns its path.
func createBackup(t *testing.T, a *testapp.App) string {
	t.Helper()
	info := testapp.Decode[api.BackupInfo](a.As(testapp.Maintainer).Post(api.BackupsPath, nil).
		Expect(http.StatusCreated))
	if info.Kind != api.BackupKindOSFM || !strings.HasPrefix(info.Name, "osfm_") || info.Size <= 0 {
		t.Fatalf("created %+v", info)
	}
	return filepath.Join(a.Config.PBDataDir(), core.LocalBackupsDirName, info.Name)
}

func collectionUUIDs(t *testing.T, a core.App, collection string) []string {
	t.Helper()
	var ids []string
	if err := a.DB().Select(store.FieldUUID).From(collection).OrderBy(store.FieldUUID).Column(&ids); err != nil {
		t.Fatal(err)
	}
	return ids
}

// TestBackupAndRestore is the acceptance test of G5: an instance with a password and TOTP member, a passkey member,
// places and recommendations is backed up from the admin route, restored into a new data directory with `osfm
// restore`, and started: the members, places and recommendations are the same, and the members sign in with TOTP (the
// master key came back with the backup), with the passkey and with a recovery code.
func TestBackupAndRestore(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	pw := a.RegisterPasswordTOTP("test_pw")
	pk := a.RegisterPasskey("test_pk")
	place := a.SeedPlace(testapp.PlaceSeed{Name: "Test Backup Cafe", Lat: 0.341, Lon: 7.061})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: place, Author: pw.Record, Text: "Test backup text"})
	a.SeedRecommendation(testapp.RecommendationSeed{Place: place, Author: pk.Record,
		Visibility: domain.VisibilityPrivate, Text: "Test private backup text"})

	path := createBackup(t, a)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("backup mode %v, want 0600", info.Mode().Perm())
	}
	checkBackupZip(t, path)

	dir := t.TempDir()
	cfg := configFor(t, dir)
	var out bytes.Buffer
	if err := runRestore(cfg, cfg.PBDataDir(), path, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "restored the backup") {
		t.Errorf("output %q", out.String())
	}
	for _, key := range []string{config.MasterKeyFile, config.InstanceKeyFile} {
		got, err := os.ReadFile(filepath.Join(cfg.SecretsDir(), key)) // #nosec G304 -- a test directory
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(a.Config.SecretsDir(), key)) // #nosec G304 -- a test directory
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from the original", key)
		}
	}

	b := testapp.Start(t, testapp.Options{Bind: bind, DataDir: dir})
	for _, collection := range []string{store.Members, store.Places, store.Recommendations, store.Credentials,
		store.RecoveryCodes} {
		if got, want := uuidsOrIDs(t, b, collection), uuidsOrIDs(t, a, collection); !slices.Equal(got, want) {
			t.Errorf("%s after the restore: %v, want %v", collection, got, want)
		}
	}

	// Password and TOTP: the TOTP secret is decrypted with the restored master key.
	pwB := pw.On(b)
	c := pwB.SignIn()
	if me := testapp.Decode[auth.Me](c.Get(auth.MePath).Expect(http.StatusOK)); me.UUID !=
		pw.Record.GetString(store.FieldUUID) {
		t.Errorf("signed in as %s", me.UUID)
	}
	// Password and a recovery code.
	rc := b.Client()
	ticket := testapp.Decode[auth.SignInTicketResponse](rc.Post(auth.SignInPasswordPath,
		auth.SignInPasswordRequest{LoginName: pw.LoginName, Password: testapp.Password}).Expect(http.StatusOK))
	rc.Post(auth.SignInRecoveryPath, auth.SignInCodeRequest{Ticket: ticket.Ticket, Code: pw.RecoveryCodes[0]}).
		Expect(http.StatusOK)
	rc.Get(auth.MePath).Expect(http.StatusOK)
	// Passkey.
	pkB := pk.On(b)
	if me := testapp.Decode[auth.Me](pkB.SignIn().Get(auth.MePath).Expect(http.StatusOK)); me.UUID !=
		pk.Record.GetString(store.FieldUUID) {
		t.Errorf("signed in as %s", me.UUID)
	}
	b.CheckNoInternalIDs()
	a.CheckNoInternalIDs()
}

// uuidsOrIDs returns the uuids of the records of collection, or their IDs for collections without uuids.
func uuidsOrIDs(t *testing.T, a core.App, collection string) []string {
	t.Helper()
	c, err := a.FindCachedCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}
	if c.Fields.GetByName(store.FieldUUID) != nil {
		return collectionUUIDs(t, a, collection)
	}
	var ids []string
	if err := a.DB().Select("id").From(collection).OrderBy("id").Column(&ids); err != nil {
		t.Fatal(err)
	}
	return ids
}

// checkBackupZip checks the entries of a backup: the databases at the top as in PocketBase's backups, the keys, the
// layers and the manifest, each file with mode 0600.
func checkBackupZip(t *testing.T, path string) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v", f.Name, f.Mode().Perm())
		}
	}
	for _, want := range []string{backup.DataDBName, backup.AuxDBName, backup.SecretsDir + config.MasterKeyFile,
		backup.SecretsDir + config.InstanceKeyFile, backup.PlacesName, backup.RecommendsName, backup.ManifestName} {
		if !slices.Contains(names, want) {
			t.Errorf("no %s in %v", want, names)
		}
	}
}

// TestBackupRoutes: the backup routes as the four identities, the fresh proof, the audit log, the download and
// deleting.
func TestBackupRoutes(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	path := createBackup(t, a)
	name := filepath.Base(path)
	named := func(pattern, n string) string { return strings.Replace(pattern, "{name}", n, 1) }

	for _, tc := range []struct {
		name string
		send func(c *testapp.Client) *testapp.Response
		want map[testapp.Identity]int
	}{
		{"the list", func(c *testapp.Client) *testapp.Response { return c.Get(api.BackupsPath) },
			map[testapp.Identity]int{testapp.Anonymous: 401, testapp.Member: 403, testapp.Author: 403,
				testapp.Maintainer: 200}},
		{"downloading", func(c *testapp.Client) *testapp.Response { return c.Get(named(api.BackupPath, name)) },
			map[testapp.Identity]int{testapp.Anonymous: 401, testapp.Member: 403, testapp.Author: 403,
				testapp.Maintainer: 200}},
		{"deleting one that does not exist", func(c *testapp.Client) *testapp.Response {
			return c.Post(named(api.BackupDeletePath, "osfm_20000101000000.zip"), nil)
		}, map[testapp.Identity]int{testapp.Anonymous: 401, testapp.Member: 403, testapp.Author: 403,
			testapp.Maintainer: 404}},
		{"creating", func(c *testapp.Client) *testapp.Response { return c.Post(api.BackupsPath, nil) },
			map[testapp.Identity]int{testapp.Anonymous: 401, testapp.Member: 403, testapp.Author: 403}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for id, want := range tc.want {
				if r := tc.send(a.As(id)); r.Code != want {
					t.Errorf("%s: status %d, want %d: %s", id, r.Code, want, r.Body.String())
				}
			}
		})
	}

	maintainer := a.As(testapp.Maintainer)
	list := testapp.Decode[api.BackupList](maintainer.Get(api.BackupsPath).Expect(http.StatusOK))
	if len(list.Backups) != 1 || list.Backups[0].Name != name || list.Backups[0].Kind != api.BackupKindOSFM {
		t.Errorf("list %+v", list)
	}

	// The download is the file, as an attachment that is not cached.
	r := maintainer.Get(named(api.BackupPath, name)).Expect(http.StatusOK)
	want, err := os.ReadFile(path) // #nosec G304 -- a test directory
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(r.Body.Bytes(), want) {
		t.Error("the download differs from the file")
	}
	for header, value := range map[string]string{"Content-Type": api.BackupContentType,
		"Content-Disposition": `attachment; filename="` + name + `"`, "Cache-Control": "no-store"} {
		if got := r.Header().Get(header); got != value {
			t.Errorf("%s: %q, want %q", header, got, value)
		}
	}
	for _, bad := range []string{"x", "osfm_2026.zip", "..%2Fdata.db", "premigrate_20000101000000.zip",
		"osfm_20000101000000.zip"} {
		maintainer.Get(named(api.BackupPath, bad)).Expect(http.StatusNotFound)
	}

	// A session verified more than the window ago may list, not create, download or delete.
	stale := a.As(testapp.Maintainer)
	old := time.Now().Add(-auth.StepUpWindow - time.Minute).UTC().Format(types.DefaultDateLayout)
	if _, err := a.DB().Update(store.Sessions, dbx.Params{"verified_at": old},
		dbx.HashExp{"member": a.Maintainer.Id}).Execute(); err != nil {
		t.Fatal(err)
	}
	stale.Get(api.BackupsPath).Expect(http.StatusOK)
	for _, r := range []*testapp.Response{
		stale.Post(api.BackupsPath, nil),
		stale.Get(named(api.BackupPath, name)),
		stale.Post(named(api.BackupDeletePath, name), nil),
	} {
		r.Expect(http.StatusForbidden)
		if e := testapp.Decode[auth.AuthError](r); e.Code != auth.CodeReauthRequired {
			t.Errorf("%s %s: code %q", r.Method, r.Path, e.Code)
		}
	}

	// Each action is in the audit log, with the maintainer as actor and the file name.
	maintainer = a.As(testapp.Maintainer)
	maintainer.Post(named(api.BackupDeletePath, name), nil).Expect(http.StatusNoContent)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the backup is still there: %v", err)
	}
	maintainer.Post(named(api.BackupDeletePath, name), nil).Expect(http.StatusNotFound)
	for _, action := range []string{api.AuditBackupCreate, api.AuditBackupDownload, api.AuditBackupDelete} {
		entries, err := a.FindAllRecords(store.AuditLog, dbx.HashExp{"action": action})
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 0 {
			t.Errorf("no %s in the audit log", action)
			continue
		}
		if entries[0].GetString("actor") != a.Maintainer.Id || !strings.Contains(entries[0].GetString("details"), name) {
			t.Errorf("%s: actor %s, details %s", action, entries[0].GetString("actor"), entries[0].GetString("details"))
		}
	}
	a.CheckNoInternalIDs()
}

// TestRestoreRefuses: restore refuses a running instance and, without --force, a data directory that holds one.
func TestRestoreRefuses(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	path := createBackup(t, a)

	// The running instance holds the lock on its data directory.
	err := runRestore(a.Config, a.Config.PBDataDir(), path, true, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "the instance is running") {
		t.Errorf("restore into the running instance: %v", err)
	}

	// A stopped instance's data directory: refused without --force, moved aside with it.
	dir := t.TempDir()
	cfg := configFor(t, dir)
	if err := runRestore(cfg, cfg.PBDataDir(), path, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := runRestore(cfg, cfg.PBDataDir(), path, false, io.Discard); !errors.Is(err, backup.ErrNotEmpty) {
		t.Errorf("restore into a data directory with an instance: %v", err)
	}
	// The backup is in the restored pb_data/backups, as after restoring one made on the same server.
	inside := filepath.Join(cfg.PBDataDir(), core.LocalBackupsDirName, filepath.Base(path))
	if err := os.MkdirAll(filepath.Dir(inside), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, inside); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runRestore(cfg, cfg.PBDataDir(), inside, true, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), ".before-restore-") {
		t.Errorf("output %q", out.String())
	}
	if _, err := os.Stat(inside); err != nil {
		t.Errorf("pb_data/backups did not move into the restored pb_data: %v", err)
	}
	if err := runRestore(cfg, cfg.PBDataDir(), filepath.Join(dir, "missing.zip"), true, io.Discard); err == nil {
		t.Error("restore of a missing file succeeded")
	}
}

// TestBackupCommand: `osfm backup` on a stopped instance, to pb_data/backups, to a file and to a directory.
func TestBackupCommand(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})
	if err := a.ClearBootstrap(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	var msg bytes.Buffer
	if err := runBackup(t.Context(), a.Config, a.Config.PBDataDir(), "", io.Discard, &msg, now); err != nil {
		t.Fatal(err)
	}
	list, err := backup.List(filepath.Join(a.Config.PBDataDir(), core.LocalBackupsDirName))
	if err != nil || len(list) != 1 || list[0].Name != "osfm_20261128120000.zip" {
		t.Fatalf("backups %+v, %v", list, err)
	}
	if !strings.Contains(msg.String(), "master key") {
		t.Errorf("message %q", msg.String())
	}
	out := t.TempDir()
	if err := runBackup(t.Context(), a.Config, a.Config.PBDataDir(), out, io.Discard, io.Discard, now); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(out, "osfm_20261128120000.zip")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("backup into a directory: %v", err)
	}
	file := filepath.Join(out, "test.zip")
	if err := runBackup(t.Context(), a.Config, a.Config.PBDataDir(), file, io.Discard, io.Discard, now); err != nil {
		t.Fatal(err)
	}
	if err := runBackup(t.Context(), a.Config, a.Config.PBDataDir(), file, io.Discard, io.Discard, now); err == nil {
		t.Error("a backup overwrote an existing file")
	}
	var stdout bytes.Buffer
	if err := runBackup(t.Context(), a.Config, a.Config.PBDataDir(), "-", &stdout, io.Discard, now); err != nil {
		t.Fatal(err)
	}
	if _, err := zip.NewReader(bytes.NewReader(stdout.Bytes()), int64(stdout.Len())); err != nil {
		t.Errorf("the standard output is not a zip: %v", err)
	}
	checkBackupZip(t, file)
}

// TestSkipsBootstrap: backup and restore run without PocketBase's bootstrap, the other commands with it.
func TestSkipsBootstrap(t *testing.T) {
	cfg := configFor(t, t.TempDir())
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: cfg.PBDataDir()})
	root := &cobra.Command{Use: "osfm"}
	root.AddCommand(newServeCommand(app, cfg), newBackupCommand(app, cfg), newRestoreCommand(app, cfg),
		newSetupCodeCommand(app, cfg))
	for args, want := range map[string]bool{"backup": true, "backup --out x.zip": true, "restore x.zip": true,
		"setup-code": false, "serve": false, "": false} {
		if got := skipsBootstrap(root, strings.Fields(args)); got != want {
			t.Errorf("%q: %v, want %v", args, got, want)
		}
	}
}
