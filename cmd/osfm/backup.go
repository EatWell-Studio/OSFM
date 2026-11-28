package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"

	"github.com/EatWell-Studio/OSFM/internal/backup"
	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/version"
)

// noBootstrap marks commands that must run without PocketBase bootstrapping the app first (which opens the database,
// applies migrations and generates missing keys): backup and restore work on the files of the data directory.
const noBootstrap = "osfm:no-bootstrap"

// skipsBootstrap reports whether args select a command marked [noBootstrap].
func skipsBootstrap(root *cobra.Command, args []string) bool {
	c, _, err := root.Find(args)
	return err == nil && c.Annotations[noBootstrap] == "true"
}

// newBackupCommand returns the backup command: a backup of the instance, whether it is running or not (G5).
func newBackupCommand(app core.App, cfg *config.Config) *cobra.Command {
	var out string
	c := &cobra.Command{
		Use:   "backup",
		Short: "Writes a backup of the instance (database, layers and keys) to a zip file",
		Long: "Writes a backup of the instance to a zip file: a consistent copy of the database, the places and " +
			"recommendations layers as GeoJSON, and the instance keys. It works while the instance runs and while it " +
			"is stopped. Without --out, the backup goes to pb_data/backups, where the admin page lists it.\n\n" +
			"The backup contains the master key: keep it as safe as the keys themselves.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		Annotations:  map[string]string{noBootstrap: "true"},
		RunE: func(c *cobra.Command, _ []string) error {
			// Messages go to the standard error: with --out -, the standard output is the backup.
			return runBackup(c.Context(), cfg, app.DataDir(), out, c.OutOrStdout(), os.Stderr, time.Now())
		},
	}
	c.Flags().StringVar(&out, "out", "",
		"write the backup to this file (a new one) or into this directory; \"-\" writes it to the standard output")
	return c
}

// runBackup backs up the instance with pb_data at pbDataDir to out (see newBackupCommand); progress goes to msg.
func runBackup(ctx context.Context, cfg *config.Config, pbDataDir, out string, stdout, msg io.Writer,
	now time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	lock, err := cfg.LockDataDir(false)
	if errors.Is(err, config.ErrDataDirLocked) {
		return errors.New("a restore is running on this data directory; try again when it is done")
	}
	if err != nil {
		return err
	}
	defer lock.Release()

	src := backup.Source{PBDataDir: pbDataDir, SecretsDir: cfg.SecretsDir(), SoftwareVersion: version.String()}
	var m *backup.Manifest
	var dest string
	switch out {
	case "-":
		m, err = backup.Write(ctx, stdout, src, now)
	case "":
		dest, m, err = backup.Create(ctx, filepath.Join(pbDataDir, core.LocalBackupsDirName), src, now)
	default:
		dest = out
		if info, statErr := os.Stat(out); statErr == nil && info.IsDir() {
			dest = filepath.Join(out, backup.Name(now))
		}
		m, err = backup.CreateFile(ctx, dest, src, now)
	}
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if dest != "" {
		_, _ = fmt.Fprintf(msg, "wrote %s (migration %s); it contains the master key: keep it safe\n", dest,
			m.Migration)
	}
	return nil
}

// newRestoreCommand returns the restore command: puts a backup back into the data directory of a stopped instance.
func newRestoreCommand(app core.App, cfg *config.Config) *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "restore <backup.zip>",
		Short: "Restores a backup made by 'osfm backup' or the admin page (the instance must be stopped)",
		Long: "Restores the database and the keys from a backup into the data directory. The instance must be " +
			"stopped. The backup is checked completely before anything changes. A data directory that already holds " +
			"an instance is refused unless --force is given; then its pb_data and secrets are renamed to " +
			"<name>.before-restore-<time> (pb_data/backups moves into the restored pb_data). The next start applies " +
			"the migrations of this version, as after an upgrade.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Annotations:  map[string]string{noBootstrap: "true"},
		RunE: func(c *cobra.Command, args []string) error {
			return runRestore(cfg, app.DataDir(), args[0], force, c.OutOrStdout())
		},
	}
	c.Flags().BoolVar(&force, "force", false,
		"replace an existing instance (its pb_data and secrets are moved aside, not deleted)")
	return c
}

// runRestore restores the backup file into the data directory of cfg with pb_data at pbDataDir.
func runRestore(cfg *config.Config, pbDataDir, file string, force bool, out io.Writer) error {
	if _, err := os.Stat(file); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	lock, err := cfg.LockDataDir(true)
	if errors.Is(err, config.ErrDataDirLocked) {
		return fmt.Errorf("restore: the instance is running (or another osfm command uses %s): stop it first",
			cfg.DataDir)
	}
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	defer lock.Release()

	r, err := backup.Restore(file, backup.Target{PBDataDir: pbDataDir, SecretsDir: cfg.SecretsDir()},
		backup.RestoreOptions{Force: force})
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	_, _ = fmt.Fprintf(out, "restored the backup of %s (OSFM %s, migration %s) into %s\n",
		r.Manifest.CreatedAt.Format(time.RFC3339), r.Manifest.SoftwareVersion, r.Manifest.Migration, cfg.DataDir)
	for _, dir := range r.MovedAside {
		_, _ = fmt.Fprintf(out, "the previous data is in %s: delete it once the restored instance works\n", dir)
	}
	_, _ = fmt.Fprintln(out, "start the instance: it applies any migrations of this version on start")
	return nil
}
