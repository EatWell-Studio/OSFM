package api

import "github.com/pocketbase/pocketbase/core"

// Editing, revisions and deleting: an author or a maintainer edits a recommendation (with a revision each time), members read the revisions, the author deletes; members edit places and maintainers delete them (T031). Routes, request and response types, and handlers of this feature go in this file (and in new files of its
// own, named after it); see the package documentation for the rules every route follows.

// registerRecommendations registers the routes of this feature. It is empty until T031 fills it.
func registerRecommendations(se *core.ServeEvent, s *server) {
	_, _ = se, s
}
