package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// pbDatabaseFile is the name of PocketBase's main database in its data directory.
const pbDatabaseFile = "data.db"

// EnsureDirs creates the data directory and its subdirectories if they are missing: pb_data/ and tiles/ with mode
// 0750, secrets/ with mode 0700. Existing directories are left as they are.
func (c *Config) EnsureDirs() error {
	for _, dir := range []string{c.DataDir, c.PBDataDir(), c.TilesDir()} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
	}
	if err := os.MkdirAll(c.SecretsDir(), 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	return nil
}

// Prepare gets the data directory ready before PocketBase opens its database: it logs the effective configuration,
// creates the subdirectories and loads or generates the instance keys.
//
// pbDataDir is the directory PocketBase actually uses: normally [Config.PBDataDir], or another directory when
// `--dir` is given. If its database already exists but a key had to be generated, the existing data was created with
// a key that is now missing, and a warning is logged.
func (c *Config) Prepare(pbDataDir string, logger *slog.Logger) (*Keys, error) {
	publicURL := c.PublicURL
	if publicURL == "" {
		publicURL = "(not set)"
	}
	if abs, err := filepath.Abs(pbDataDir); err == nil {
		pbDataDir = abs
	}
	logger.Info("OSFM configuration",
		slog.String("data_dir", c.DataDir),
		slog.String("pb_data", pbDataDir),
		slog.String("tiles", c.TilesDir()),
		slog.String("secrets", c.SecretsDir()),
		slog.String("listen", c.Listen),
		slog.String("public_url", publicURL),
		slog.Bool("admin_ui", c.AdminUI),
	)

	if err := c.EnsureDirs(); err != nil {
		return nil, err
	}

	hasData, err := fileExists(filepath.Join(pbDataDir, pbDatabaseFile))
	if err != nil {
		return nil, err
	}

	keys, err := EnsureKeys(c.SecretsDir())
	if err != nil {
		return nil, err
	}

	if keys.MasterGenerated {
		path := filepath.Join(c.SecretsDir(), MasterKeyFile)
		if hasData {
			logger.Warn("!!! A NEW MASTER KEY WAS GENERATED FOR EXISTING DATA !!! Secrets encrypted with the previous "+
				"master key (such as TOTP secrets) can no longer be decrypted. If you have a backup of the old key, stop "+
				"the server and put it back in place of the new one.", slog.String("path", path))
		} else {
			logger.Info("generated the instance master key; include it in your backups", slog.String("path", path))
		}
	}
	if keys.InstanceGenerated {
		path := filepath.Join(c.SecretsDir(), InstanceKeyFile)
		if hasData {
			logger.Warn("!!! A NEW INSTANCE SIGNING KEY WAS GENERATED FOR EXISTING DATA !!! The identity of this "+
				"instance has changed. If you have a backup of the old key, stop the server and put it back in place "+
				"of the new one.", slog.String("path", path))
		} else {
			logger.Info("generated the instance signing key; include it in your backups", slog.String("path", path))
		}
	}

	return keys, nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("check for existing data: %w", err)
}
