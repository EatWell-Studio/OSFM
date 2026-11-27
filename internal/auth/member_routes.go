package auth

import (
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/boundary"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// The member list of the admin page (architecture §3.3, §4.2): the maintainers see who is in the circle, how each
// member can sign in and whether a reset link is waiting for them, and create reset links from it.
//
//   - Members are named by uuid and main name only. The login name is not part of the list: it is half of a password
//     sign-in and of a recovery, and the maintainer needs neither to tell members apart (the main name, the join date
//     and the inviter do) nor to create a reset link (by uuid). The member who opens a reset link sees their login
//     name there. The pseudonym is not part of it either.
//   - Sign-in methods are counts and flags, never anything about the credentials themselves.
//   - Members who left or were removed (left_at set) are listed with leftAt; soft-deleted members (tombstones) are not
//     listed, but can still be named as someone's inviter.

// MembersPath lists the members (GET); maintainers only.
const MembersPath = boundary.OSFMPrefix + "members"

// MemberInfo is a member as the maintainers' list shows it. leftAt is null for a member who is still in the circle;
// invitedBy is null for the first maintainer. pendingResetLink is the member's reset link that can still be used,
// null when there is none.
type MemberInfo struct {
	UUID             string            `json:"uuid"`
	DisplayName      string            `json:"displayName"`
	Role             string            `json:"role"`
	JoinedAt         time.Time         `json:"joinedAt"`
	CanInvite        bool              `json:"canInvite"`
	InvitedBy        *MemberRef        `json:"invitedBy"`
	LeftAt           *time.Time        `json:"leftAt"`
	Passkeys         int64             `json:"passkeys"`
	PasswordTOTP     bool              `json:"passwordTotp"`
	PendingResetLink *PendingResetLink `json:"pendingResetLink"`
}

// PendingResetLink is a reset link that can still be used, as the member list shows it.
type PendingResetLink struct {
	UUID      string    `json:"uuid"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// MemberList is the response of GET [MembersPath]: the members in the order they joined.
type MemberList struct {
	Members []MemberInfo `json:"members"`
}

func bindMemberRoutes(se *core.ServeEvent) {
	se.Router.GET(MembersPath, handleListMembers).Bind(RequireMaintainer())
}

func handleListMembers(e *core.RequestEvent) error {
	members, err := ListMembers(e.App)
	if err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, MemberList{Members: members})
}

// ListMembers returns the members who are not soft-deleted, in the order they joined, as [MemberInfo].
func ListMembers(app core.App) ([]MemberInfo, error) {
	all, err := store.Query(app, store.Members).IncludeDeleted().OrderBy("joined_at ASC", "uuid ASC").All()
	if err != nil {
		return nil, err
	}
	refs := make(map[string]*MemberRef, len(all))
	for _, m := range all {
		refs[m.Id] = &MemberRef{UUID: m.GetString(store.FieldUUID), DisplayName: m.GetString("display_name")}
	}

	var counts []struct {
		Member string `db:"member"`
		Kind   string `db:"kind"`
		N      int64  `db:"n"`
	}
	if err := app.DB().Select("member", "kind", "COUNT(*) AS n").From(store.Credentials).
		GroupBy("member", "kind").All(&counts); err != nil {
		return nil, err
	}
	passkeys := map[string]int64{}
	password := map[string]bool{}
	for _, c := range counts {
		switch c.Kind {
		case CredentialPasskey:
			passkeys[c.Member] = c.N
		case CredentialPasswordTOTP:
			password[c.Member] = c.N > 0
		}
	}

	t := now().UTC()
	links, err := app.FindAllRecords(store.ResetLinks, dbx.HashExp{"used_at": "", "revoked_at": ""},
		dbx.NewExp("expires_at > {:now}", dbx.Params{"now": dbTime(t)}))
	if err != nil {
		return nil, err
	}
	pending := map[string]*PendingResetLink{}
	for _, l := range links {
		// Creating a link revokes the member's earlier ones, so there is at most one; keep the latest anyway.
		expires := l.GetDateTime("expires_at").Time().UTC()
		if p := pending[l.GetString("member")]; p == nil || expires.After(p.ExpiresAt) {
			pending[l.GetString("member")] = &PendingResetLink{UUID: l.GetString(store.FieldUUID), ExpiresAt: expires}
		}
	}

	out := make([]MemberInfo, 0, len(all))
	for _, m := range all {
		if store.IsDeleted(m) {
			continue
		}
		joined := m.GetDateTime("joined_at")
		if joined.IsZero() {
			joined = m.GetDateTime(store.FieldCreated)
		}
		info := MemberInfo{
			UUID:         m.GetString(store.FieldUUID),
			DisplayName:  m.GetString("display_name"),
			Role:         m.GetString("role"),
			JoinedAt:     joined.Time().UTC(),
			CanInvite:    m.GetBool("can_invite"),
			InvitedBy:    refs[m.GetString("invited_by")],
			LeftAt:       optionalTime(m, "left_at"),
			Passkeys:     passkeys[m.Id],
			PasswordTOTP: password[m.Id],
		}
		// A link of a member who can no longer sign in cannot be used (findValidResetLink).
		if CanSignIn(m) {
			info.PendingResetLink = pending[m.Id]
		}
		out = append(out, info)
	}
	return out, nil
}
