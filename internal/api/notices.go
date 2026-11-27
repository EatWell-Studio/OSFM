package api

import "github.com/pocketbase/pocketbase/core"

// The member's notices: their own notices, newest first, with the unread count, and marking them read (T037). Routes, request and response types, and handlers of this feature go in this file (and in new files of its
// own, named after it); see the package documentation for the rules every route follows.

// registerNotices registers the routes of this feature. It is empty until T037 fills it.
func registerNotices(se *core.ServeEvent, s *server) {
	_, _ = se, s
}
