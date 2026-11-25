// Command osfm runs an OSFM instance: the PocketBase core, the OSFM API and the embedded web app, in one process.
package main

import (
	"log"
	"net/http"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/web"
)

func main() {
	app := pocketbase.New()

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
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
