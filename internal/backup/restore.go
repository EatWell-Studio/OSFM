package backup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/config"
)

// Target is where a backup is restored to.
type Target struct {
	// PBDataDir is the PocketBase data directory (<data>/pb_data, or what --dir names).
	PBDataDir string
	// SecretsDir is <data>/secrets.
	SecretsDir string
}

// RestoreOptions configure [Restore].
type RestoreOptions struct {
	// Force allows restoring over an existing instance: its pb_data and secrets are moved aside, not deleted.
	Force bool
	// Now names the directories moved aside; the zero value means the current time.
	Now time.Time
}

// Restored is the outcome of a [Restore].
type Restored struct {
	Manifest *Manifest
	// MovedAside lists the directories the existing instance was moved to (with Force), for the operator to delete
	// once the restored instance works.
	MovedAside []string
}

// Errors of [Restore].
var (
	// ErrNotEmpty: the target already holds an instance and Force is not set.
	ErrNotEmpty = errors.New("the data directory already holds an instance")
	// ErrInvalid: the file is not an OSFM backup, or not an intact one.
	ErrInvalid = errors.New("not a valid OSFM backup")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Restore puts the database and the keys of the backup at zipPath back into t. The instance must be stopped: the
// caller holds the exclusive lock on the data directory (config.Config.LockDataDir).
//
// Before anything in t changes, it checks the backup: the manifest's format, that the zip holds exactly the files the
// manifest lists with the listed sizes and SHA-256, that the keys are valid, that the database passes SQLite's quick
// check and that this program knows every migration applied to it ([ErrNewerData] otherwise; a backup from an older
// version is fine, the next start migrates it as after an upgrade). Everything is extracted into temporary
// directories next to pb_data and secrets first, and only then swapped in.
//
// Without opts.Force, a target that already holds an instance (files in pb_data other than backups, or any file in
// secrets) is refused with [ErrNotEmpty]. With it, the existing pb_data and secrets are renamed to
// <dir>.before-restore-<UTC time>, except pb_data/backups, which moves into the restored pb_data. tiles/ is never
// touched.
func Restore(zipPath string, t Target, opts RestoreOptions) (*Restored, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, invalid("%v", err)
	}
	defer func() { _ = zr.Close() }()

	m, files, err := readManifest(&zr.Reader)
	if err != nil {
		return nil, err
	}

	if !opts.Force {
		for _, dir := range []struct {
			path   string
			ignore []string
		}{
			{t.PBDataDir, []string{core.LocalBackupsDirName, core.LocalTempDirName, "lost+found"}},
			{t.SecretsDir, []string{"lost+found"}},
		} {
			used, err := inUse(dir.path, dir.ignore)
			if err != nil {
				return nil, err
			}
			if used {
				return nil, fmt.Errorf("%w (%s is not empty); use --force to replace it", ErrNotEmpty, dir.path)
			}
		}
	}

	stagePB, err := stageDir(t.PBDataDir, 0o750)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(stagePB) }()
	stageSecrets, err := stageDir(t.SecretsDir, 0o700)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(stageSecrets) }()

	for _, f := range m.Files {
		dest := ""
		switch {
		case strings.HasPrefix(f.Path, SecretsDir):
			dest = filepath.Join(stageSecrets, strings.TrimPrefix(f.Path, SecretsDir))
		case strings.HasPrefix(f.Path, OSFMDir):
			// The layers: checked, not restored.
		default:
			dest = filepath.Join(stagePB, filepath.FromSlash(f.Path))
		}
		if err := extract(files[f.Path], f, dest); err != nil {
			return nil, err
		}
	}

	if _, err := config.ReadKeys(stageSecrets); err != nil {
		return nil, invalid("%v", err)
	}
	if err := checkDatabase(filepath.Join(stagePB, DataDBName), m.Migration); err != nil {
		return nil, err
	}

	result := &Restored{Manifest: m}
	stamp := opts.Now.UTC().Format(nameTimeLayout)
	if err := swapIn(t.PBDataDir, stagePB, core.LocalBackupsDirName, stamp, result); err != nil {
		return nil, err
	}
	if err := swapIn(t.SecretsDir, stageSecrets, "", stamp, result); err != nil {
		return nil, fmt.Errorf("pb_data was restored but the keys were not, restore again: %w", err)
	}
	return result, nil
}

// readManifest reads and checks the manifest of zr and returns it with the zip's files by name: exactly the files
// the manifest lists, with the entries an OSFM backup needs.
func readManifest(zr *zip.Reader) (*Manifest, map[string]*zip.File, error) {
	files := map[string]*zip.File{}
	var manifest *zip.File
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") && f.UncompressedSize64 == 0 {
			continue // a directory entry
		}
		if !safeEntryName(f.Name) {
			return nil, nil, invalid("unsafe entry name %q", f.Name)
		}
		if _, dup := files[f.Name]; dup || f.Name == ManifestName && manifest != nil {
			return nil, nil, invalid("entry %s appears twice", f.Name)
		}
		if f.Name == ManifestName {
			manifest = f
			continue
		}
		files[f.Name] = f
	}
	if manifest == nil {
		return nil, nil, invalid("no %s (a PocketBase backup or another zip?)", ManifestName)
	}
	rc, err := manifest.Open()
	if err != nil {
		return nil, nil, invalid("%s: %v", ManifestName, err)
	}
	body, err := io.ReadAll(io.LimitReader(rc, maxManifestSize+1))
	_ = rc.Close()
	if err != nil {
		return nil, nil, invalid("%s: %v", ManifestName, err)
	}
	if len(body) > maxManifestSize {
		return nil, nil, invalid("%s is too large", ManifestName)
	}
	var m Manifest
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&m); err != nil {
		return nil, nil, invalid("%s: %v", ManifestName, err)
	}
	if m.Schema != Schema {
		return nil, nil, invalid("format %q, this version of OSFM restores %q", m.Schema, Schema)
	}

	listed := map[string]bool{}
	for _, f := range m.Files {
		if listed[f.Path] {
			return nil, nil, invalid("%s lists %s twice", ManifestName, f.Path)
		}
		listed[f.Path] = true
		if files[f.Path] == nil {
			return nil, nil, invalid("%s is listed but missing", f.Path)
		}
		if f.Size < 0 || len(f.SHA256) != sha256.Size*2 {
			return nil, nil, invalid("%s: bad size or checksum in the manifest", f.Path)
		}
		if strings.HasPrefix(f.Path, SecretsDir) && !slices.Contains(keyFiles, strings.TrimPrefix(f.Path, SecretsDir)) {
			return nil, nil, invalid("unexpected key file %s", f.Path)
		}
	}
	for name := range files {
		if !listed[name] {
			return nil, nil, invalid("%s is not listed in the manifest", name)
		}
	}
	for _, name := range append([]string{DataDBName}, secretEntries()...) {
		if !listed[name] {
			return nil, nil, invalid("%s is missing", name)
		}
	}
	return &m, files, nil
}

var keyFiles = []string{config.MasterKeyFile, config.InstanceKeyFile}

func secretEntries() []string {
	out := make([]string, len(keyFiles))
	for i, name := range keyFiles {
		out[i] = SecretsDir + name
	}
	return out
}

// extract checks the content of zf against f and writes it to dest (mode 0600) unless dest is "".
func extract(zf *zip.File, f File, dest string) (err error) {
	rc, err := zf.Open()
	if err != nil {
		return invalid("%s: %v", f.Path, err)
	}
	defer func() { _ = rc.Close() }()
	out := io.Discard
	if dest != "" {
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			return err
		}
		file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode) // #nosec G304 -- under a staging dir
		if err != nil {
			return err
		}
		defer func() {
			if syncErr := file.Sync(); err == nil {
				err = syncErr
			}
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
		}()
		out = file
	}
	sum := sha256.New()
	// One byte more than the manifest says, to notice a longer content without reading all of it.
	n, err := io.Copy(io.MultiWriter(out, sum), io.LimitReader(rc, f.Size+1))
	if err != nil {
		if errors.Is(err, zip.ErrChecksum) || errors.Is(err, zip.ErrFormat) {
			return invalid("%s: %v", f.Path, err)
		}
		return err
	}
	if n != f.Size || hex.EncodeToString(sum.Sum(nil)) != f.SHA256 {
		return invalid("%s does not match the manifest (damaged or changed)", f.Path)
	}
	return nil
}

// checkDatabase checks the restored database: SQLite's quick check, and that this program knows every migration
// applied to it and the newest one is the manifest's.
func checkDatabase(path, migration string) error {
	db, err := lightConnect(path)
	if err != nil {
		return invalid("%s: %v", DataDBName, err)
	}
	defer func() { _ = db.Close() }()
	var result []string
	if err := db.NewQuery("PRAGMA quick_check").Column(&result); err != nil {
		return invalid("%s: %v", DataDBName, err)
	}
	if len(result) != 1 || result[0] != "ok" {
		return invalid("%s fails SQLite's check: %s", DataDBName, strings.Join(result, "; "))
	}
	newest, unknown, err := appliedMigrations(db)
	if err != nil {
		return invalid("%s: %v", DataDBName, err)
	}
	if len(unknown) > 0 {
		return fmt.Errorf("%w (%s): restore it with that version or a later one", ErrNewerData,
			strings.Join(unknown, ", "))
	}
	if newest != migration {
		return invalid("the database's newest migration is %q, the manifest says %q", newest, migration)
	}
	return nil
}

// inUse reports whether dir holds anything besides the ignored top-level names.
func inUse(dir string, ignore []string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !slices.Contains(ignore, e.Name()) {
			return true, nil
		}
	}
	return false, nil
}

// stageDir creates a temporary directory next to dir (on the same file system, so it can be renamed into place).
func stageDir(dir string, mode fs.FileMode) (string, error) {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+".restore-*")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(stage, mode); err != nil {
		_ = os.RemoveAll(stage)
		return "", err
	}
	return stage, nil
}

// swapIn replaces dir with stage. An existing dir's entry keep (if any) moves into stage first; what remains of dir
// is renamed to dir.before-restore-<stamp> (recorded in r), or removed when nothing remains. On an error, what it
// moved is moved back.
func swapIn(dir, stage, keep, stamp string, r *Restored) (err error) {
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return os.Rename(stage, dir)
	} else if err != nil {
		return err
	}
	if keep != "" {
		from, to := filepath.Join(dir, keep), filepath.Join(stage, keep)
		if _, err := os.Lstat(from); err == nil {
			if err := os.Rename(from, to); err != nil {
				return err
			}
			defer func() {
				if err != nil {
					_ = os.Rename(to, from)
				}
			}()
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		if err := os.Remove(dir); err != nil {
			return err
		}
		if err := os.Rename(stage, dir); err != nil {
			_ = os.Mkdir(dir, 0o750)
			return err
		}
		return nil
	}
	aside := dir + ".before-restore-" + stamp
	if err := os.Rename(dir, aside); err != nil {
		return err
	}
	if err := os.Rename(stage, dir); err != nil {
		_ = os.Rename(aside, dir)
		return err
	}
	r.MovedAside = append(r.MovedAside, aside)
	return nil
}
