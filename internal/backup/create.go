package backup

import (
	"archive/zip"
	"compress/flate"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/export"
	"github.com/EatWell-Studio/OSFM/internal/store"
	_ "github.com/EatWell-Studio/OSFM/migrations" // the OSFM migrations, to tell newer databases and migrate copies
)

// Source is the instance to back up.
type Source struct {
	// PBDataDir is the PocketBase data directory (<data>/pb_data, or what --dir names).
	PBDataDir string
	// SecretsDir is <data>/secrets.
	SecretsDir string
	// SoftwareVersion is written to the manifest.
	SoftwareVersion string
}

// ErrNoDatabase: the data directory has no database to back up.
var ErrNoDatabase = errors.New("there is no database to back up")

// ErrExists: a backup of that name exists already (two backups within one second).
var ErrExists = errors.New("a backup of that name exists already")

// fileMode is the mode of backup files and of every file a restore writes.
const fileMode fs.FileMode = 0o600

// Create writes a backup of src into dir (normally pb_data/backups, created if missing) as [Name](now), with mode
// 0600. The zip is written under a temporary name in dir and renamed when complete, so a listing never shows a partial
// backup. It returns the path of the backup and its manifest.
func Create(ctx context.Context, dir string, src Source, now time.Time) (string, *Manifest, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", nil, err
	}
	dest := filepath.Join(dir, Name(now))
	m, err := CreateFile(ctx, dest, src, now)
	if err != nil {
		return "", nil, err
	}
	return dest, m, nil
}

// CreateFile writes a backup of src to the file dest (mode 0600), which must not exist yet ([ErrExists]). Like
// [Create], it writes a temporary file next to dest first.
func CreateFile(ctx context.Context, dest string, src Source, now time.Time) (m *Manifest, err error) {
	if _, err := os.Lstat(dest); err == nil {
		return nil, ErrExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	f, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".partial-*")
	if err != nil {
		return nil, err
	}
	temp := f.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(temp)
		}
	}()
	// CreateTemp makes the file 0600 already; set it explicitly all the same, the zip holds the master key.
	if err := f.Chmod(fileMode); err != nil {
		_ = f.Close()
		return nil, err
	}
	m, err = Write(ctx, f, src, now)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	// The name was free at the start; within one second a second backup could have taken it.
	if _, statErr := os.Lstat(dest); statErr == nil {
		return nil, ErrExists
	}
	if err = os.Rename(temp, dest); err != nil {
		return nil, err
	}
	return m, nil
}

// Write writes a backup of src to w as a zip (see the package documentation) and returns its manifest. It streams:
// no file is read into memory as a whole. It works while the instance runs and while it is stopped; it needs room in
// pb_data for a temporary copy of each database.
func Write(ctx context.Context, w io.Writer, src Source, now time.Time) (*Manifest, error) {
	dbPath := filepath.Join(src.PBDataDir, DataDBName)
	if info, err := os.Stat(dbPath); errors.Is(err, fs.ErrNotExist) || (err == nil && info.Size() == 0) {
		return nil, ErrNoDatabase
	} else if err != nil {
		return nil, err
	}
	// The keys are checked before anything is copied: a backup without valid keys could not be restored.
	if _, err := config.ReadKeys(src.SecretsDir); err != nil {
		return nil, err
	}

	// Temporary copies live in PocketBase's temp directory, which its backups skip and every bootstrap empties.
	tempRoot := filepath.Join(src.PBDataDir, core.LocalTempDirName)
	if err := os.MkdirAll(tempRoot, 0o750); err != nil {
		return nil, err
	}
	temp, err := os.MkdirTemp(tempRoot, "osfm_backup_")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(temp) }()

	m := &Manifest{Schema: Schema, CreatedAt: now.UTC().Truncate(time.Second), SoftwareVersion: src.SoftwareVersion}
	zw := newZipWriter(w, now)

	// The main database: copied, zipped as it is, then opened as an app of its own for the layers.
	snapDir := filepath.Join(temp, "snapshot")
	if err := os.Mkdir(snapDir, 0o700); err != nil {
		return nil, err
	}
	snapDB := filepath.Join(snapDir, DataDBName)
	if err := vacuumInto(dbPath, snapDB); err != nil {
		return nil, fmt.Errorf("copy %s: %w", DataDBName, err)
	}
	if err := zw.addFile(ctx, DataDBName, snapDB); err != nil {
		return nil, err
	}
	newest, unknown, err := migrationsOf(snapDB)
	if err != nil {
		return nil, err
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("%w (%s)", ErrNewerData, strings.Join(unknown, ", "))
	}
	m.Migration = newest

	// The logs database, if there is one.
	auxPath := filepath.Join(src.PBDataDir, AuxDBName)
	if info, err := os.Stat(auxPath); err == nil && info.Size() > 0 {
		auxCopy := filepath.Join(temp, AuxDBName)
		if err := vacuumInto(auxPath, auxCopy); err != nil {
			return nil, fmt.Errorf("copy %s: %w", AuxDBName, err)
		}
		if err := zw.addFile(ctx, AuxDBName, auxCopy); err != nil {
			return nil, err
		}
		_ = os.Remove(auxCopy)
	}

	// The rest of pb_data, as PocketBase's backups hold it.
	if err := zw.addPBData(ctx, src.PBDataDir); err != nil {
		return nil, err
	}

	// The keys.
	for _, name := range []string{config.MasterKeyFile, config.InstanceKeyFile} {
		if err := zw.addFile(ctx, SecretsDir+name, filepath.Join(src.SecretsDir, name)); err != nil {
			return nil, err
		}
	}

	// The layers, from the copy of the database.
	if err := writeLayers(ctx, zw, snapDir, now); err != nil {
		return nil, fmt.Errorf("export the layers: %w", err)
	}

	m.Files = zw.files
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := zw.addBytes(ManifestName, body); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return m, nil
}

// vacuumInto writes a consistent copy of the SQLite database at src to dest, from a connection of its own (the
// instance may be using the database meanwhile).
func vacuumInto(src, dest string) error {
	db, err := lightConnect(src)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.NewQuery("VACUUM INTO {:path}").Bind(dbx.Params{"path": dest}).Execute()
	return err
}

// lightConnect opens the SQLite database at path with a small page cache (2 MiB rather than PocketBase's 32 MiB) and
// without changing its journal mode: copying a large database must not grow the process by much (G2, a 2-core ARM
// board). The busy timeout is PocketBase's, for a database the instance writes to meanwhile.
func lightConnect(path string) (*dbx.DB, error) {
	return dbx.Open("sqlite", path+"?_pragma=busy_timeout(10000)&_pragma=cache_size(-2000)&_pragma=temp_store(FILE)")
}

// migrationsOf returns the newest applied OSFM migration of the database at path, and the applied migrations this
// program does not know.
func migrationsOf(path string) (string, []string, error) {
	db, err := lightConnect(path)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = db.Close() }()
	return appliedMigrations(db)
}

// writeLayers opens the copy of the database in dir as a PocketBase app, applies any migration it lacks (a stopped
// instance not yet started with this version), and writes both layers into the zip.
func writeLayers(ctx context.Context, zw *zipWriter, dir string, now time.Time) error {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
	store.BindHooks(app)
	if err := app.Bootstrap(); err != nil {
		return err
	}
	defer func() { _ = app.ClearBootstrap() }()
	if _, err := core.NewMigrationsRunner(app, core.AppMigrations).Up(); err != nil {
		return err
	}
	opts := export.Options{Now: now}
	for _, layer := range []struct {
		name  string
		write func(io.Writer, core.App, export.Options) error
	}{
		{PlacesName, export.WritePlaces},
		{RecommendsName, export.WriteRecommendations},
	} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := zw.add(layer.name, func(w io.Writer) error { return layer.write(w, app, opts) }); err != nil {
			return err
		}
	}
	return nil
}

// zipWriter writes zip entries and records each one's size and SHA-256 for the manifest.
type zipWriter struct {
	*zip.Writer
	modified time.Time
	files    []File
}

func newZipWriter(w io.Writer, now time.Time) *zipWriter {
	zw := zip.NewWriter(w)
	// The fastest compression, as PocketBase's backups use: the database compresses well even so, and a backup on a
	// small board should not take minutes of CPU.
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.BestSpeed)
	})
	return &zipWriter{Writer: zw, modified: now}
}

// add writes an entry named name with the content write produces.
func (z *zipWriter) add(name string, write func(io.Writer) error) error {
	h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: z.modified}
	h.SetMode(fileMode)
	w, err := z.CreateHeader(h)
	if err != nil {
		return err
	}
	sum := sha256.New()
	cw := &countingWriter{w: io.MultiWriter(w, sum)}
	if err := write(cw); err != nil {
		return err
	}
	z.files = append(z.files, File{Path: name, Size: cw.n, SHA256: hex.EncodeToString(sum.Sum(nil))})
	return nil
}

// addFile writes the file at src as the entry name.
func (z *zipWriter) addFile(ctx context.Context, name, src string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.Open(src) // #nosec G304 -- a file of the data directory or a copy of one
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return z.add(name, func(w io.Writer) error {
		_, err := io.Copy(w, f)
		return err
	})
}

// addBytes writes b as the entry name; for the manifest, which is not listed in itself.
func (z *zipWriter) addBytes(name string, b []byte) error {
	h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: z.modified}
	h.SetMode(fileMode)
	w, err := z.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// skippedPBData are the top-level entries of pb_data that a backup leaves out, as PocketBase's own backups do:
// backups, temporary and runtime directories. The databases are added from their copies.
var skippedPBData = []string{core.LocalBackupsDirName, core.LocalTempDirName, core.LocalNotifyDirName,
	core.LocalAutocertCacheDirName, "lost+found"}

// addPBData adds the regular files of pb_data other than the databases (and their -wal and -shm files) and the
// skipped directories.
func (z *zipWriter) addPBData(ctx context.Context, dir string) error {
	return fs.WalkDir(os.DirFS(dir), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		top := path.Dir(name) == "."
		if d.IsDir() {
			if top && (slices.Contains(skippedPBData, name) || name+"/" == OSFMDir) {
				return fs.SkipDir // osfm/: left in pb_data by PocketBase's own restore of an OSFM backup
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if top && (strings.HasPrefix(name, DataDBName) || strings.HasPrefix(name, AuxDBName)) {
			return nil
		}
		return z.addFile(ctx, name, filepath.Join(dir, filepath.FromSlash(name)))
	})
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
