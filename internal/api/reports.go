package api

import "github.com/pocketbase/pocketbase/core"

// Hiding, reports and their review: members report a recommendation, enough open reports hide it, maintainers hide, unhide and review (T036). Routes, request and response types, and handlers of this feature go in this file (and in new files of its
// own, named after it); see the package documentation for the rules every route follows.

// registerReports registers the routes of this feature. It is empty until T036 fills it.
func registerReports(se *core.ServeEvent, s *server) {
	_, _ = se, s
}
