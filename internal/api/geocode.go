package api

import "github.com/pocketbase/pocketbase/core"

// Place-name search: the server-side proxy to the search services, with its cache and limits (T039). Routes, request and response types, and handlers of this feature go in this file (and in new files of its
// own, named after it); see the package documentation for the rules every route follows.

// registerGeocode registers the routes of this feature. It is empty until T039 fills it.
func registerGeocode(se *core.ServeEvent, s *server) {
	_, _ = se, s
}
