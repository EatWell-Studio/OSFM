package auth

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Roles of a member (D6), the values of members.role.
const (
	RoleMember     = "member"
	RoleMaintainer = "maintainer"
)

// ErrNotMember is returned for a record that does not belong to the members collection.
var ErrNotMember = errors.New("auth: not a member record")

// ErrCannotSignIn is returned when a session is requested for a member who may not be signed in: one who has left or
// been removed (left_at set) or whose record is soft-deleted.
var ErrCannotSignIn = errors.New("auth: this member cannot sign in")

// NewMemberRecord returns a new, unsaved record of the members collection. PocketBase requires every auth record to
// have a password; OSFM never uses it (members' passwords live in the credentials collection, D3), so the record gets
// a random one that is never revealed. OSFM never issues PocketBase auth tokens for members either: they sign in with a
// session (see [SignIn]).
func NewMemberRecord(app core.App) (*core.Record, error) {
	c, err := app.FindCachedCollectionByNameOrId(store.Members)
	if err != nil {
		return nil, err
	}
	r := core.NewRecord(c)
	r.SetRandomPassword()
	return r, nil
}

// CanSignIn reports whether member may have a session: a record of the members collection that has neither left the
// circle (left_at set) nor been soft-deleted. Leaving (D9) and removal by a maintainer (D7) must therefore set left_at
// or soft-delete the record; either ends the member's sessions at once (see [Bind]).
func CanSignIn(member *core.Record) bool {
	return isMember(member) && member.GetDateTime("left_at").IsZero() && !store.IsDeleted(member)
}

func isMember(r *core.Record) bool {
	return r != nil && r.Collection().Name == store.Members
}
