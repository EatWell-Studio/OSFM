package api

import "github.com/pocketbase/pocketbase/core"

// Creating places and recommendations: a new place with its first recommendation, a recommendation for a place the member can see, and the nearby places with a similar name (T026). Routes, request and response types, and handlers of this feature go in this file (and in new files of its
// own, named after it); see the package documentation for the rules every route follows.

// registerPlaces registers the routes of this feature. It is empty until T026 fills it.
func registerPlaces(se *core.ServeEvent, s *server) {
	_, _ = se, s
}
