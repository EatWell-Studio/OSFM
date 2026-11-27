package api

import "github.com/pocketbase/pocketbase/core"

// The per-layer GeoJSON exports: the places layer and the recommendations layer (T041). Routes, request and response types, and handlers of this feature go in this file (and in new files of its
// own, named after it); see the package documentation for the rules every route follows.

// registerLayerExport registers the routes of this feature. It is empty until T041 fills it.
func registerLayerExport(se *core.ServeEvent, s *server) {
	_, _ = se, s
}
