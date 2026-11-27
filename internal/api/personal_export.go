package api

import "github.com/pocketbase/pocketbase/core"

// The personal data export: everything the server holds about the member, as one JSON file (T040). Routes, request and response types, and handlers of this feature go in this file (and in new files of its
// own, named after it); see the package documentation for the rules every route follows.

// registerPersonalExport registers the routes of this feature. It is empty until T040 fills it.
func registerPersonalExport(se *core.ServeEvent, s *server) {
	_, _ = se, s
}
