package backup

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Info is a backup file in pb_data/backups.
type Info struct {
	Name string
	// Kind is [KindOSFM] or [KindPremigrate].
	Kind string
	Size int64
	// CreatedAt is the time in the name (UTC).
	CreatedAt time.Time
}

// ErrNotFound: no backup file of that name.
var ErrNotFound = errors.New("no such backup")

// List returns the backup files in dir (pb_data/backups), newest first: OSFM's and those taken before migrations.
// Other files (PocketBase's own backups, partial files) are left out. A missing dir has none.
func List(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Info{}, nil
	}
	if err != nil {
		return nil, err
	}
	list := []Info{}
	for _, entry := range entries {
		kind, t, ok := parseName(entry.Name())
		if !ok || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue // removed meanwhile
		}
		list = append(list, Info{Name: entry.Name(), Kind: kind, Size: info.Size(), CreatedAt: t})
	}
	slices.SortFunc(list, func(a, b Info) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.Name, a.Name)
	})
	return list, nil
}

// Stat returns the backup file name in dir: [ErrNotFound] for a name that is not a backup's or a file that is not a
// regular file.
func Stat(dir, name string) (Info, error) {
	kind, t, ok := parseName(name)
	if !ok {
		return Info{}, ErrNotFound
	}
	info, err := os.Lstat(filepath.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
		return Info{}, ErrNotFound
	}
	if err != nil {
		return Info{}, err
	}
	return Info{Name: name, Kind: kind, Size: info.Size(), CreatedAt: t}, nil
}

// Open opens the backup file name in dir for reading ([ErrNotFound] as for [Stat]).
func Open(dir, name string) (*os.File, Info, error) {
	info, err := Stat(dir, name)
	if err != nil {
		return nil, Info{}, err
	}
	f, err := os.Open(filepath.Join(dir, name)) // #nosec G304 -- name matches the backup name rule (no separators)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, Info{}, ErrNotFound
	}
	if err != nil {
		return nil, Info{}, err
	}
	return f, info, nil
}

// Delete removes the backup file name in dir ([ErrNotFound] as for [Stat]).
func Delete(dir, name string) error {
	if _, err := Stat(dir, name); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}
