// Command osfm runs an OSFM instance: the PocketBase core, the OSFM API and the embedded web app, in one process.
package main

import (
	"log"
	"log/slog"
	"net/http"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/cmd"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/osutils"
	"github.com/spf13/cobra"

	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/federation"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/tiles"
	"github.com/EatWell-Studio/OSFM/internal/version"
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
	if err := app.Execute(); err != nil {
		log.Fatal(err)
	}
}

// bind registers OSFM's hooks and routes on app. Every OSFM route is registered here, in the OnServe hook: the tests
// boot an app through bind, so that what they check (such as the internal ID scan) is what `osfm serve` serves.
func bind(app core.App, cfg *config.Config) {
	// The data directory and the keys are prepared when the app bootstraps, before PocketBase opens its database, so
	// that `osfm --help` and `osfm --version` leave the file system alone.
	var keys *config.Keys
	app.OnBootstrap().BindFunc(func(be *core.BootstrapEvent) error {
		// The authentication code will take the master key (TOTP secrets) from here.
		var err error
		if keys, err = cfg.Prepare(be.App.DataDir(), slog.Default()); err != nil {
			return err
		}
		return be.Next()
	})
	// Bound after the hook above, so it runs once the data directory is ready: backs up an existing database that has
	// migrations to apply, bootstraps (PocketBase's system migrations) and applies the OSFM migrations (G6).
	migrations.Bind(app)
	// The data model's invariants (uuid, coordinate rounding, append-only collections) for every record save.
	store.BindHooks(app)

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// Basemap tiles from <data>/tiles; with OSFM_PUBLIC_URL set, the TileJSON gives absolute tile URLs. Not behind
		// authentication yet: member sessions arrive with T007.
		tileHandler, err := tiles.NewHandler(cfg.TilesDir(), cfg.PublicURL, se.App.Logger())
		if err != nil {
			return err
		}
		se.Router.GET(tiles.RoutePrefix+"{path...}", apis.WrapStdHandler(tileHandler))

		// The instance metadata for other instances (F5): public, outside any session check.
		if err := federation.Register(se.Router, keys.Instance.Public()); err != nil {
			return err
		}

		// The single-page app answers every path the API does not; unknown paths fall back to index.html so that
		// client-side routes work on reload.
		if !se.Router.HasRoute(http.MethodGet, "/{path...}") {
			se.Router.GET("/{path...}", apis.Static(web.FS(), true))
		}
		return se.Next()
	})
}

// newServeCommand returns PocketBase's serve command with the default of its --http flag taken from cfg
// (OSFM_LISTEN); an explicit --http flag still takes precedence.
func newServeCommand(app core.App, cfg *config.Config) *cobra.Command {
	serve := cmd.NewServeCommand(app, true)
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
