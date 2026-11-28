package migrations

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Member invites (T034, M2-Q11): members may invite once their cooling-off period is over, and new members get the
// invite permission (members.can_invite) when they register. Members who joined before have it off (M1 left it
// unset, since only maintainers invited); this migration switches it on for them, so that every member is treated
// alike. The schema does not change.
//
// A member whose permission a maintainer has set (an audit_log entry members.invite_permission) keeps it: the backfill
// decides only for members nobody has decided for, so running it again changes nothing. The down migration switches
// the permission off again for exactly those members (maintainers keep theirs).
func init() {
	m.Register(upMemberInvites, downMemberInvites)
}

// auditInvitePermission is auth.AuditInvitePermission (the migrations do not import internal/auth; a test checks that
// the two are the same).
const auditInvitePermission = "members.invite_permission"

// undecidedMembers matches the members whose invite permission no maintainer has set.
func undecidedMembers() dbx.Expression {
	return dbx.NewExp("[[uuid]] NOT IN (SELECT [[target_uuid]] FROM {{"+store.AuditLog+"}} "+
		"WHERE [[action]] = {:action} AND [[target_type]] = {:type})",
		dbx.Params{"action": auditInvitePermission, "type": store.Members})
}

func upMemberInvites(app core.App) error {
	_, err := app.DB().Update(store.Members, dbx.Params{"can_invite": true},
		dbx.And(dbx.HashExp{"can_invite": false}, undecidedMembers())).Execute()
	return err
}

func downMemberInvites(app core.App) error {
	// Maintainers had the permission in M1 already (the setup wizard sets it).
	_, err := app.DB().Update(store.Members, dbx.Params{"can_invite": false},
		dbx.And(dbx.HashExp{"can_invite": true, "role": "member"}, undecidedMembers())).Execute()
	return err
}
