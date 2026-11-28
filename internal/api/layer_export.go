package api

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/export"
	"github.com/EatWell-Studio/OSFM/internal/ratelimit"
	"github.com/EatWell-Studio/OSFM/internal/settings"
)

// The per-layer GeoJSON exports: the places layer and the recommendations layer (F1, H5, T041). Every member may
// download both (M2-Q17): they hold the circle's layer, what every member sees on the map whoever they are
// (internal/export), so the files are the same for every caller and hold no member data beyond the signatures readers
// see. The format is in docs/data-format.md.

// Routes of this feature. Both are GET with an optional ?city=<slug>, and answer a GeoJSON file to download
// (Content-Disposition: attachment).
const (
	// ExportPlacesPath is the places layer (osfm-places/1, ODbL 1.0).
	ExportPlacesPath = "/api/osfm/exports/places"
	// ExportRecommendationsPath is the recommendations layer (osfm-recommendations/1, CC BY-SA 4.0).
	ExportRecommendationsPath = "/api/osfm/exports/recommendations"
)

// ExportParamCity limits an export to the places inside the city with this slug, and their recommendations; absent
// or empty for the whole layer.
const ExportParamCity = "city"

// CodeExportUnknownCity: the city parameter names no city of the instance (400).
const CodeExportUnknownCity = "unknown_city"

// ExportContentType is the media type of the files (RFC 7946 §12).
const ExportContentType = "application/geo+json"

// exportBudgetID is the route middleware of the per-member export budget.
const exportBudgetID = "osfmAPIExportBudget"

// exportRate is every member's budget of exports, both layers together: 10 at once, then one every 30 seconds. A
// member downloads a few files now and then; an export reads every place or recommendation of the circle.
var exportRate = ratelimit.Rate{Burst: 10, Every: 30 * time.Second}

// registerLayerExport registers the routes of this feature.
func registerLayerExport(se *core.ServeEvent, s *server) {
	budget := s.MemberBudget(exportBudgetID, exportRate)
	se.Router.GET(ExportPlacesPath, func(e *core.RequestEvent) error {
		return handleLayerExport(e, "places", export.WritePlaces)
	}).Bind(budget)
	se.Router.GET(ExportRecommendationsPath, func(e *core.RequestEvent) error {
		return handleLayerExport(e, "recommendations", export.WriteRecommendations)
	}).Bind(budget)
}

// handleLayerExport answers the layer as a file named osfm-<layer>[-<city>]-<UTC date>.geojson. The file is written
// to memory first, so that an error is a 500 and not a truncated download (a layer of a few thousand recommendations
// is a few megabytes).
func handleLayerExport(e *core.RequestEvent, layer string, write func(io.Writer, core.App, export.Options) error) error {
	opts := export.Options{Now: time.Now()}
	if slug := e.Request.URL.Query().Get(ExportParamCity); slug != "" {
		inst, err := settings.Load(e.App)
		if err != nil {
			return e.InternalServerError("", err)
		}
		found := false
		for _, c := range inst.Cities {
			if c.Slug == slug {
				box := export.BBox(c.BBox)
				opts.BBox, opts.City, found = &box, c.Slug, true
				break
			}
		}
		if !found {
			return fail(e, http.StatusBadRequest, CodeExportUnknownCity, "No such city.")
		}
	}
	var buf bytes.Buffer
	if err := write(&buf, e.App, opts); err != nil {
		return e.InternalServerError("", err)
	}
	name := "osfm-" + layer
	if opts.City != "" {
		name += "-" + opts.City // a slug: lowercase letters, digits and "-"
	}
	name += "-" + opts.Now.UTC().Format(time.DateOnly) + ".geojson"
	h := e.Response.Header()
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	return e.Blob(http.StatusOK, ExportContentType, buf.Bytes())
}
