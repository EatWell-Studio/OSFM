package api

import "github.com/pocketbase/pocketbase/core"

// Backups: maintainers create, list, download and delete backups (T042). Routes, request and response types, and handlers of this feature go in this file (and in new files of its
// own, named after it); see the package documentation for the rules every route follows.

// registerBackups registers the routes of this feature. It is empty until T042 fills it.
func registerBackups(se *core.ServeEvent, s *server) {
	_, _ = se, s
}
