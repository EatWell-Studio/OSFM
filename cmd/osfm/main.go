// Command osfm runs an OSFM instance: the PocketBase core, the OSFM API and the embedded web app, in one process.
package main

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/cmd"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/osutils"
	"github.com/spf13/cobra"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/boundary"
	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/federation"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/tiles"
	"github.com/EatWell-Studio/OSFM/internal/version"
	"github.com/EatWell-Studio/OSFM/internal/webapp"
	"github.com/EatWell-Studio/OSFM/migrations"
	"github.com/EatWell-Studio/OSFM/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	// PocketBase keeps its data in <data>/pb_data; its --dir flag still overrides that.
	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDev:     osutils.IsProbablyGoRun(), // as pocketbase.New does
		DefaultDataDir: cfg.PBDataDir(),
	})
	app.RootCmd.Version = version.String()
	bind(app, cfg)

	// The same commands as app.Start registers, with the serve command's listen address taken from the configuration.
	app.RootCmd.AddCommand(cmd.NewSuperuserCommand(app))
	app.RootCmd.AddCommand(newServeCommand(app, cfg))
	app.RootCmd.AddCommand(newSetupCodeCommand(app, cfg))
	app.RootCmd.AddCommand(newBackupCommand(app, cfg), newRestoreCommand(app, cfg))
	// Execute bootstraps the app before any command runs; backup and restore must not (see noBootstrap).
	execute := app.Execute
	if skipsBootstrap(app.RootCmd, os.Args[1:]) {
		execute = app.RootCmd.Execute
	}
	if err := execute(); err != nil {
		log.Fatal(err)
	}
}

// bind registers OSFM's hooks and routes on app. Every OSFM route is registered here, in the OnServe hook: the tests
// boot an app through bind, so that what they check (such as the internal ID scan) is what `osfm serve` serves.
func bind(app core.App, cfg *config.Config) {
	// The data directory and the keys are prepared when the app bootstraps, before PocketBase opens its database, so
	// that `osfm --help` and `osfm --version` leave the file system alone.
	var keys *config.Keys
	// Every command that opens the instance holds a shared lock on the data directory until it ends, so that
	// `osfm restore` (exclusive) can tell that the instance is running, and no instance starts during a restore.
	var lock *config.DataDirLock
	app.OnBootstrap().BindFunc(func(be *core.BootstrapEvent) error {
		if lock == nil {
			l, err := cfg.LockDataDir(false)
			if errors.Is(err, config.ErrDataDirLocked) {
				return errors.New("a restore is running on the data directory; start the instance when it is done")
			}
			if err != nil {
				return err
			}
			lock = l
		}
		// The authentication code takes the master key (TOTP secrets) from here, see auth.Options.MasterKey.
		var err error
		if keys, err = cfg.Prepare(be.App.DataDir(), slog.Default()); err != nil {
			return err
		}
		return be.Next()
	})
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		lock.Release()
		lock = nil
		return e.Next()
	})
	// Bound after the hook above, so it runs once the data directory is ready: backs up an existing database that has
	// migrations to apply, bootstraps (PocketBase's system migrations) and applies the OSFM migrations (G6).
	migrations.Bind(app)
	// The data model's invariants (uuid, coordinate rounding, append-only collections) for every record save.
	store.BindHooks(app)
	// Only /api/osfm/*, /api/health and the web app on the main listener; the admin UI on its own listener (AD2). The
	// client address behind a reverse proxy comes from OSFM_TRUSTED_PROXY_HEADERS (rate limits).
	boundary.Bind(app, boundary.Options{AdminUI: cfg.AdminUI, AdminListen: cfg.AdminListen,
		TrustedProxyHeaders: cfg.TrustedProxyHeaders})
	// Member sessions: /api/osfm/* (except the routes marked public) and /tiles/* need one; GET /api/osfm/me and
	// POST /api/osfm/logout (AD6, E1); the invite routes under /api/osfm/invites (D1, D2).
	// Registration and sign-in with password and TOTP; the master key is read once the app has bootstrapped.
	// Passkey registration and sign-in under /api/osfm/passkey (D3, D4), for the relying party OSFM_PUBLIC_URL names.
	// The first-start setup wizard under /api/osfm/setup (G3): while there is no maintainer, the setup code goes to the
	// log and to the secrets directory.
	rpID, rpOrigins := cfg.PasskeyRP()
	auth.Bind(app, auth.Options{HostPrefix: cfg.PublicHTTPS(), PasskeyRPID: rpID, PasskeyOrigins: rpOrigins,
		MasterKey: func() []byte {
			if keys == nil {
				return nil
			}
			return keys.Master
		}, SetupCodeFile: setupCodeFile(cfg)})
	// The OSFM features under /api/osfm/ (internal/api): the instance settings, places and recommendations, and the
	// rest of M2, one register function per feature.
	api.Bind(app, api.Options{Config: cfg})
	// The web app on every path the API does not answer (client-side routes fall back to index.html, missing files get
	// 404), and the security headers of the main listener (§4.7).
	webapp.Bind(app, webapp.Options{FS: web.FS(), HSTS: cfg.PublicHTTPS()})

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// Basemap tiles from <data>/tiles; with OSFM_PUBLIC_URL set, the TileJSON gives absolute tile URLs. Members
		// only: the session middleware answers 401 without a session (E1).
		tileHandler, err := tiles.NewHandler(cfg.TilesDir(), cfg.PublicURL, se.App.Logger())
		if err != nil {
			return err
		}
		// Successful tile requests stay out of the request log: the tiles a member loads after "show my location"
		// would place them on the map (A8, E4).
		se.Router.GET(tiles.RoutePrefix+"{path...}", apis.WrapStdHandler(tileHandler)).Bind(apis.SkipSuccessActivityLog())

		// The instance metadata for other instances (F5): public, outside any session check.
		if err := federation.Register(se.Router, keys.Instance.Public()); err != nil {
			return err
		}
		return se.Next()
	})
}

// setupCodeFile is where the server writes the setup code while the instance has no maintainer.
func setupCodeFile(cfg *config.Config) string {
	return filepath.Join(cfg.SecretsDir(), auth.SetupCodeFileName)
}

// newSetupCodeCommand returns the setup-code command: it prints the setup code that the running (or last started)
// server generated, for operators who cannot easily read its log.
func newSetupCodeCommand(app core.App, cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:          "setup-code",
		Short:        "Prints the setup code of an instance that has no maintainer yet",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(c *cobra.Command, _ []string) error {
			code, err := auth.ReadSetupCode(app, setupCodeFile(cfg))
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(c.OutOrStdout(), code)
			return err
		},
	}
}

// newServeCommand returns PocketBase's serve command with the default of its --http flag taken from cfg
// (OSFM_LISTEN); an explicit --http flag still takes precedence. PocketBase's start banner is off: it would point at
// the admin UI on the main listener, where it is not served; internal/boundary logs the addresses instead.
func newServeCommand(app core.App, cfg *config.Config) *cobra.Command {
	serve := cmd.NewServeCommand(app, false)
	useConfiguredListen(serve, cfg)
	return serve
}

// useConfiguredListen makes serve, a command with a persistent --http flag, listen on the configured address unless
// --http is given.
func useConfiguredListen(serve *cobra.Command, cfg *config.Config) {
	httpFlag := serve.PersistentFlags().Lookup("http")
	httpFlag.Usage = "TCP address to listen for the HTTP server\n(default " + config.EnvListen + ", or " +
		config.DefaultListen + "; with domain arguments 0.0.0.0:80 unless " + config.EnvListen + " is set)"

	run := serve.RunE
	serve.RunE = func(c *cobra.Command, args []string) error {
		if httpFlag.Changed {
			slog.Info("the --http flag overrides "+config.EnvListen, slog.String("listen", httpFlag.Value.String()))
		} else if addr := cfg.HTTPAddr(len(args) > 0); addr != "" {
			if err := httpFlag.Value.Set(addr); err != nil {
				return err
			}
		}
		return run(c, args)
	}
}
