// Command osfm runs an OSFM instance: the PocketBase core, the OSFM API and the embedded web app, in one process.
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/tiles"
	"github.com/EatWell-Studio/OSFM/web"
)

func main() {
	app := pocketbase.New()

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// Basemap tiles. Not behind authentication yet: member sessions arrive with T007.
		tileHandler, err := tiles.NewHandler(tilesDir(), "", se.App.Logger())
		if err != nil {
			return err
		}
		se.Router.GET(tiles.RoutePrefix+"{path...}", apis.WrapStdHandler(tileHandler))

		// The single-page app answers every path the API does not; unknown paths fall back to index.html so that
		// client-side routes work on reload.
		if !se.Router.HasRoute(http.MethodGet, "/{path...}") {
			se.Router.GET("/{path...}", apis.Static(web.FS(), true))
		}
		return se.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

// tilesDir is the directory of the per-city basemap archives: <data>/tiles, where <data> is OSFM_DATA_DIR (default
// ./data). Temporary: replaced by internal/config (T004).
func tilesDir() string {
	dataDir := os.Getenv("OSFM_DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}
	return filepath.Join(dataDir, "tiles")
}
