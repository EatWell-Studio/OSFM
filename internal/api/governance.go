package api

import "github.com/pocketbase/pocketbase/core"

// Invites and governance: members invite after the cooling-off period, maintainers manage invite permissions, the invite tree, removal of members and branches, and the maintainer role (T035) (T034). Routes, request and response types, and handlers of this feature go in this file (and in new files of its
// own, named after it); see the package documentation for the rules every route follows.

// registerGovernance registers the routes of this feature. It is empty until T034 fills it.
func registerGovernance(se *core.ServeEvent, s *server) {
	_, _ = se, s
}
