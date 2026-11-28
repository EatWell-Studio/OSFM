package api

import "github.com/pocketbase/pocketbase/core"

// Invites and governance: members invite after the cooling-off period, maintainers manage invite permissions (T034),
// the invite tree, removal of members and branches, and the maintainer role (T035). Routes, request and response types,
// and handlers of this feature go in this file and in new files of its own, named after it; see the package
// documentation for the rules every route follows.

// registerGovernance registers the routes of this feature: the member invites and the cooling-off period
// (governance_invites.go, T034); the invite tree, removal and roles (governance_members.go, T035).
func registerGovernance(se *core.ServeEvent, s *server) {
	registerInvites(se, s)
	registerMembers(se, s)
}
