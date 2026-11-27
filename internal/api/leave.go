package api

import "github.com/pocketbase/pocketbase/core"

// Leaving the circle: the member leaves, deleting or keeping their recommendations (T038). Routes, request and response types, and handlers of this feature go in this file (and in new files of its
// own, named after it); see the package documentation for the rules every route follows.

// registerLeave registers the routes of this feature. It is empty until T038 fills it.
func registerLeave(se *core.ServeEvent, s *server) {
	_, _ = se, s
}
