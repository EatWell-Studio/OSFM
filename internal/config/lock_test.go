package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/EatWell-Studio/OSFM/internal/config"
)

func TestLockDataDir(t *testing.T) {
	cfg := &config.Config{DataDir: filepath.Join(t.TempDir(), "data")}

	// Shared locks (the running instance, `osfm backup`) coexist; an exclusive one (`osfm restore`) waits for none.
	first, err := cfg.LockDataDir(false)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(cfg.DataDir, config.LockFileName)); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("lock file: %v, %v", info, err)
	}
	second, err := cfg.LockDataDir(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.LockDataDir(true); !errors.Is(err, config.ErrDataDirLocked) {
		t.Errorf("exclusive lock while shared ones are held: %v", err)
	}
	first.Release()
	if _, err := cfg.LockDataDir(true); !errors.Is(err, config.ErrDataDirLocked) {
		t.Errorf("exclusive lock while a shared one is held: %v", err)
	}
	second.Release()
	second.Release() // twice is fine

	exclusive, err := cfg.LockDataDir(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.LockDataDir(false); !errors.Is(err, config.ErrDataDirLocked) {
		t.Errorf("shared lock during a restore: %v", err)
	}
	exclusive.Release()
	again, err := cfg.LockDataDir(false)
	if err != nil {
		t.Fatal(err)
	}
	again.Release()
	var none *config.DataDirLock
	none.Release()
}

func TestReadKeys(t *testing.T) {
	dir := t.TempDir()
	if _, err := config.ReadKeys(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no keys: %v", err)
	}
	made, err := config.EnsureKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	read, err := config.ReadKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(read.Master) != string(made.Master) || !read.Instance.Equal(made.Instance) {
		t.Error("ReadKeys returned other keys")
	}
	if err := os.WriteFile(filepath.Join(dir, config.MasterKeyFile), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.ReadKeys(dir); err == nil {
		t.Error("a short master key was accepted")
	}
}
