package api

import (
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/domain"
)

// ViewerOf returns the caller of the request as a [domain.Viewer]: the member of the session with their role, or the
// zero Viewer (sees nothing) without one. It is the only way a handler should make a Viewer.
func ViewerOf(e *core.RequestEvent) domain.Viewer {
	return MemberViewer(auth.CurrentMember(e))
}

// MemberViewer returns member as a [domain.Viewer]; the zero Viewer for nil or a member who may not sign in (left,
// removed, deleted).
func MemberViewer(member *core.Record) domain.Viewer {
	if member == nil || !auth.CanSignIn(member) {
		return domain.Viewer{}
	}
	return domain.Viewer{MemberID: member.Id, Maintainer: auth.IsMaintainer(member)}
}
