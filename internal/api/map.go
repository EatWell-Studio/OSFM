package api

import "github.com/pocketbase/pocketbase/core"

// Map data and the place card: the places of a city's viewport with their counts and filters, one place with its visible recommendations, and the signatures to filter by (T027). Routes, request and response types, and handlers of this feature go in this file (and in new files of its
// own, named after it); see the package documentation for the rules every route follows.

// registerMap registers the routes of this feature. It is empty until T027 fills it.
func registerMap(se *core.ServeEvent, s *server) {
	_, _ = se, s
}
