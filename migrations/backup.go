package migrations

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	dataDBFile = "data.db"      // PocketBase's main database in pb_data
	auxDBFile  = "auxiliary.db" // PocketBase's logs database in pb_data

	// BackupPrefix starts the name of every backup taken before migrations. The names match PocketBase's backup name
	// rule, so the backups appear in its backup list and can be restored like the ones it creates.
	BackupPrefix = "premigrate_"
)

// Bind makes app apply the OSFM migrations whenever it bootstraps (every command except --help and --version), and
// back up the database first when it already exists and has migrations to apply (G6).
//
// PocketBase runs its own system migrations inside Bootstrap, right after opening the database, with no hook in
// between; `serve` runs the remaining ones before it starts listening. So the backup is taken before Bootstrap, from
// its own short-lived connection (see [BackupBeforeMigrate]), and the OSFM migrations run right after Bootstrap. Both
// kinds of migration are therefore covered by the backup, and serve finds nothing left to apply.
func Bind(app core.App) {
	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if _, err := BackupBeforeMigrate(e.App.DataDir(), time.Now(), slog.Default()); err != nil {
			return fmt.Errorf("backup before migrations: %w", err)
		}
		if err := e.Next(); err != nil {
			return err
		}
		applied, err := core.NewMigrationsRunner(e.App, core.AppMigrations).Up()
		if err != nil {
			return err
		}
		for _, file := range applied {
			slog.Info("applied migration", slog.String("file", file))
		}
		return nil
	})
}

// BackupBeforeMigrate backs up the PocketBase data directory pbDataDir when its database exists and has migrations
// to apply: registered PocketBase system or OSFM migrations it has not recorded as applied. A new or empty database
// is not backed up. It must run while the database is not in use, that is before the app bootstraps.
//
// The backup is a zip in pb_data/backups in PocketBase's backup layout (consistent copies of data.db and auxiliary.db
// made with VACUUM INTO, and the other files of pb_data except earlier backups and temporary files), named
// premigrate_<UTC time>.zip. If data.db is unchanged since the newest such backup (a start that failed before any
// migration was committed, retried in a restart loop), that backup is kept and no new one is written.
//
// It returns the path of the backup, or "" when none was needed.
func BackupBeforeMigrate(pbDataDir string, now time.Time, logger *slog.Logger) (string, error) {
	dbPath := filepath.Join(pbDataDir, dataDBFile)
	info, err := os.Stat(dbPath)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && info.Size() == 0) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	db, err := core.DefaultDBConnect(dbPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	pending, err := pendingMigrations(db)
	if err != nil || len(pending) == 0 {
		return "", err
	}

	tempDir := filepath.Join(pbDataDir, core.LocalTempDirName) // emptied by PocketBase on every bootstrap
	if err := os.MkdirAll(tempDir, 0o750); err != nil {
		return "", err
	}
	dataCopy := filepath.Join(tempDir, BackupPrefix+dataDBFile)
	if err := vacuumInto(db, dataCopy); err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(dataCopy) }()
	digest, err := fileSHA256(dataCopy)
	if err != nil {
		return "", err
	}

	backupsDir := filepath.Join(pbDataDir, core.LocalBackupsDirName)
	if prev, err := newestBackup(backupsDir); err != nil {
		return "", err
	} else if prev != "" && zipComment(prev) == digest {
		logger.Info("database unchanged since the last backup before migrations; not backing it up again",
			slog.String("backup", prev), slog.Int("pending_migrations", len(pending)))
		return prev, nil
	}

	files := map[string]string{dataDBFile: dataCopy}
	if _, err := os.Stat(filepath.Join(pbDataDir, auxDBFile)); err == nil {
		auxCopy := filepath.Join(tempDir, BackupPrefix+auxDBFile)
		if err := vacuumFile(filepath.Join(pbDataDir, auxDBFile), auxCopy); err != nil {
			return "", err
		}
		defer func() { _ = os.Remove(auxCopy) }()
		files[auxDBFile] = auxCopy
	}

	if err := os.MkdirAll(backupsDir, 0o750); err != nil {
		return "", err
	}
	name := BackupPrefix + now.UTC().Format("20060102150405") + ".zip"
	tempZip := filepath.Join(tempDir, name)
	if err := writeZip(tempZip, pbDataDir, files, digest); err != nil {
		_ = os.Remove(tempZip)
		return "", err
	}
	dest := filepath.Join(backupsDir, name)
	if err := os.Rename(tempZip, dest); err != nil {
		_ = os.Remove(tempZip)
		return "", err
	}
	logger.Info("backed up the database before applying migrations",
		slog.String("backup", dest), slog.Int("pending_migrations", len(pending)))
	return dest, nil
}

// pendingMigrations returns the registered system and app migrations that db has not applied. A database without
// any table counts as new and has none.
func pendingMigrations(db *dbx.DB) ([]string, error) {
	var tables []string
	if err := db.NewQuery("SELECT name FROM sqlite_master WHERE type = 'table'").Column(&tables); err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, nil
	}
	var applied []string
	if slices.Contains(tables, core.DefaultMigrationsTable) {
		if err := db.Select("file").From(core.DefaultMigrationsTable).Column(&applied); err != nil {
			return nil, err
		}
	}
	var pending []string
	for _, list := range []core.MigrationsList{core.SystemMigrations, core.AppMigrations} {
		for _, item := range list.Items() {
			if !slices.Contains(applied, item.File) {
				pending = append(pending, item.File)
			}
		}
	}
	return pending, nil
}

func vacuumInto(db *dbx.DB, dest string) error {
	if err := os.Remove(dest); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	_, err := db.NewQuery("VACUUM INTO {:path}").Bind(dbx.Params{"path": dest}).Execute()
	return err
}

func vacuumFile(src, dest string) error {
	db, err := core.DefaultDBConnect(src)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return vacuumInto(db, dest)
}

// newestBackup returns the path of the newest backup taken before migrations in dir, or "" if there is none.
func newestBackup(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	newest := ""
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type().IsRegular() && strings.HasPrefix(name, BackupPrefix) && strings.HasSuffix(name, ".zip") &&
			name > newest {
			newest = name
		}
	}
	if newest == "" {
		return "", nil
	}
	return filepath.Join(dir, newest), nil
}

// zipComment returns the comment of the zip file at path, or "" if it cannot be read.
func zipComment(path string) string {
	r, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer func() { _ = r.Close() }()
	return r.Comment
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- a file this package has just written
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeZip writes the backup zip to dest: the database copies in files (zip name → path) and the other files of
// pbDataDir, except earlier backups, temporary and runtime directories and the live database files. comment is the
// SHA-256 of the data.db copy.
func writeZip(dest, pbDataDir string, files map[string]string, comment string) (err error) {
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- under pb_data
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	zw := zip.NewWriter(f)

	for _, name := range []string{dataDBFile, auxDBFile} {
		if src, ok := files[name]; ok {
			if err := addFile(zw, name, src); err != nil {
				return err
			}
		}
	}

	skipDirs := []string{
		core.LocalBackupsDirName, core.LocalTempDirName, core.LocalNotifyDirName, core.LocalAutocertCacheDirName,
		"lost+found",
	}
	root := os.DirFS(pbDataDir)
	err = fs.WalkDir(root, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path.Dir(name) == "." && slices.Contains(skipDirs, name) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		base := path.Base(name)
		if path.Dir(name) == "." && (strings.HasPrefix(base, dataDBFile) || strings.HasPrefix(base, auxDBFile)) {
			return nil // data.db, auxiliary.db and their -wal and -shm files: copied above
		}
		return addFile(zw, name, filepath.Join(pbDataDir, filepath.FromSlash(name)))
	})
	if err != nil {
		return err
	}
	if err := zw.SetComment(comment); err != nil {
		return err
	}
	return zw.Close()
}

func addFile(zw *zip.Writer, name, src string) error {
	in, err := os.Open(src) // #nosec G304 -- a file under pb_data
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	h, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	h.Name = name
	h.Method = zip.Deflate
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, in)
	return err
}
