package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LockFileName is the lock file in the data directory (see [Config.LockDataDir]).
const LockFileName = "osfm.lock"

// ErrDataDirLocked is returned by [Config.LockDataDir] when another process holds a conflicting lock on the data
// directory: for a shared lock, a restore is running; for an exclusive one, the instance or another osfm command is
// using it.
var ErrDataDirLocked = errors.New("the data directory is in use by another osfm process")

// DataDirLock is a lock on the data directory, held until [DataDirLock.Release] or until the process ends.
type DataDirLock struct {
	f *os.File
}

// LockDataDir locks the data directory, creating it (mode 0750) and its lock file if they are missing. Every command
// that opens the instance (serve and the others that bootstrap PocketBase, and `osfm backup`) takes a shared lock;
// `osfm restore` takes an exclusive one, so that it never replaces the files of a running instance and no instance
// starts while it runs. The lock is an advisory lock of the operating system on the lock file (flock): it goes away
// with the process that held it, so a crashed instance leaves no stale lock. It does not work across machines (a data
// directory on a network file system shared by two hosts).
//
// It does not wait: when another process holds a conflicting lock, it returns [ErrDataDirLocked] at once.
func (c *Config) LockDataDir(exclusive bool) (*DataDirLock, error) {
	if err := os.MkdirAll(c.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	path := filepath.Join(c.DataDir, LockFileName)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) // #nosec G304 -- in the data directory the operator chose
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := lockFile(f, exclusive); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &DataDirLock{f: f}, nil
}

// Release releases the lock. It may be called more than once, and on a nil lock.
func (l *DataDirLock) Release() {
	if l == nil || l.f == nil {
		return
	}
	_ = l.f.Close() // closing the file releases the lock
	l.f = nil
}
