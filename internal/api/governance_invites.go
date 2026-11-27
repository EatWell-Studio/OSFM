package api

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Member invites and the cooling-off period (D2, D6, D12; M2-Q11), T034's part of the governance feature. The invite
// routes themselves (list, create, revoke) are internal/auth's: members use them once domain.CanInvite lets them,
// see only their own invites there, and get 403 invite_not_allowed (with the reason and the end of the cooling-off
// period) or 409 invite_limit_reached otherwise. Here:
//
//   - GET [InviteStatusPath]: whether the signed-in member may invite now, and why not; the "me" page shows the days
//     left of the cooling-off period from it.
//   - POST [InvitePermissionPath] and [InviteEarlyPath] (maintainers): switch a member's invite permission off or on,
//     and end their cooling-off period now (members.invite_allowed_from). Each change writes an audit entry
//     (auth.AuditInvitePermission, auth.AuditInviteEarly) in its transaction; a request that changes nothing writes
//     none.
//
// The cooling-off period is computed from joined_at with the setting as it is when asked (settings.LoadCircle), so a
// change of the setting applies to everyone at once.

// Paths of the member invite routes.
const (
	InviteStatusPath     = "/api/osfm/me/invite-status"
	InvitePermissionPath = "/api/osfm/members/{uuid}/invite-permission"
	InviteEarlyPath      = "/api/osfm/members/{uuid}/invite-early"
)

// Why a member may not invite ([InviteStatus].Reason, and the reason of a 403 invite_not_allowed of the invite route):
// the same values as domain.InviteReasonCooldown and domain.InviteReasonDisabled.
const (
	InviteReasonCooldown = "cooldown"
	InviteReasonDisabled = "disabled"
)

// MaxActiveInvites is the number of active invites a member who is not a maintainer may have at a time
// (domain.MaxActiveInvites).
const MaxActiveInvites = 3

// InviteStatus is GET [InviteStatusPath]: whether the signed-in member may create an invite now.
type InviteStatus struct {
	// Allowed is true when the member may invite now (maintainers always). A member at MaxActiveInvites active
	// invites is still Allowed, but must wait for one to be used, revoked or expire.
	Allowed bool `json:"allowed"`
	// Reason is empty when Allowed, otherwise InviteReasonCooldown or InviteReasonDisabled.
	Reason string `json:"reason"`
	// CooldownEndsAt is when the cooling-off period ends or ended; null for maintainers.
	CooldownEndsAt *time.Time `json:"cooldownEndsAt" tstype:"string | null,required"`
	// ActiveInvites is the number of the member's invites that are neither used, revoked nor expired.
	ActiveInvites int `json:"activeInvites"`
	// MaxActiveInvites is the limit of ActiveInvites; null for maintainers, who have none.
	MaxActiveInvites *int `json:"maxActiveInvites" tstype:"number | null,required"`
}

// InvitePermissionRequest is the body of POST [InvitePermissionPath].
type InvitePermissionRequest struct {
	// CanInvite switches the member's invite permission on (true) or off (false); required.
	CanInvite *bool `json:"canInvite"`
}

// MemberInviteRights is the response of POST [InvitePermissionPath] and [InviteEarlyPath]: the member's invite
// permission and cooling-off period after the change.
type MemberInviteRights struct {
	UUID string `json:"uuid"`
	// CanInvite is the member's invite permission.
	CanInvite bool `json:"canInvite"`
	// Allowed and Reason are as in [InviteStatus].
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
	// CooldownEndsAt is when the cooling-off period ends or ended; null for maintainers.
	CooldownEndsAt *time.Time `json:"cooldownEndsAt" tstype:"string | null,required"`
}

// registerInvites registers the routes of this file; registerGovernance calls it.
func registerInvites(se *core.ServeEvent, s *server) {
	se.Router.GET(InviteStatusPath, handleInviteStatus)
	se.Router.POST(InvitePermissionPath, handleInvitePermission).Bind(MaintainersOnly(), s.WriteBudget())
	se.Router.POST(InviteEarlyPath, handleInviteEarly).Bind(MaintainersOnly(), s.WriteBudget())
}

func handleInviteStatus(e *core.RequestEvent) error {
	m := auth.CurrentMember(e)
	if m == nil {
		return e.UnauthorizedError("", nil)
	}
	right, err := auth.InviteRightOf(e.App, m)
	if err != nil {
		return e.InternalServerError("", err)
	}
	active, err := auth.CountActiveInvites(e.App, m)
	if err != nil {
		return e.InternalServerError("", err)
	}
	status := InviteStatus{Allowed: right.Allowed, Reason: right.Reason, CooldownEndsAt: optionalUTC(right.CooldownEndsAt),
		ActiveInvites: active}
	if !auth.IsMaintainer(m) {
		limit := domain.MaxActiveInvites
		status.MaxActiveInvites = &limit
	}
	return respond(e, http.StatusOK, status)
}

func handleInvitePermission(e *core.RequestEvent) error {
	id, ok := pathUUID(e, "uuid")
	if !ok {
		return notFound(e)
	}
	var req InvitePermissionRequest
	if err := readJSON(e, &req, DefaultMaxBody); err != nil {
		return requestError(e, err)
	}
	if req.CanInvite == nil {
		return badRequest(e)
	}
	want := *req.CanInvite
	return changeInviteRights(e, id, func(m *core.Record, _ int) (string, map[string]any) {
		if m.GetBool("can_invite") == want {
			return "", nil
		}
		m.Set("can_invite", want)
		return auth.AuditInvitePermission, map[string]any{"canInvite": want}
	})
}

func handleInviteEarly(e *core.RequestEvent) error {
	id, ok := pathUUID(e, "uuid")
	if !ok {
		return notFound(e)
	}
	return changeInviteRights(e, id, func(m *core.Record, days int) (string, map[string]any) {
		end := domain.CooldownEnd(auth.InviterOf(m), days)
		t := time.Now().UTC()
		if auth.IsMaintainer(m) || !t.Before(end) {
			return "", nil // nothing to open: a maintainer, or a cooling-off period that is over
		}
		m.Set("invite_allowed_from", t)
		return auth.AuditInviteEarly, map[string]any{"cooldownEndsAt": end.Format(time.RFC3339)}
	})
}

// changeInviteRights applies change to the member with uuid id, who must be in the circle (404 otherwise), on behalf
// of the maintainer of the request, in one transaction: change sets fields of the member and returns the audit action
// and details of the change, or an empty action when there is nothing to change (then nothing is saved). It answers
// with the member's [MemberInviteRights] afterwards.
func changeInviteRights(e *core.RequestEvent, id string,
	change func(m *core.Record, cooldownDays int) (action string, details map[string]any)) error {
	actor := auth.CurrentMember(e)
	var member *core.Record
	var circle settings.Circle
	err := e.App.RunInTransaction(func(tx core.App) error {
		m, err := store.FindByUUID(tx, store.Members, id)
		if err != nil {
			return err
		}
		if !auth.CanSignIn(m) {
			return sql.ErrNoRows // left or removed: not a member to manage any more
		}
		if circle, err = settings.LoadCircle(tx); err != nil {
			return err
		}
		member = m
		action, details := change(m, circle.InviteCooldownDays)
		if action == "" {
			return nil
		}
		if err := tx.Save(m); err != nil {
			return err
		}
		return store.WriteAudit(tx, actor, action, store.Members, id, details)
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return notFound(e)
	case err != nil:
		return e.InternalServerError("", err)
	}
	right := domain.CanInvite(auth.InviterOf(member), circle.InviteCooldownDays, time.Now().UTC())
	return respond(e, http.StatusOK, MemberInviteRights{UUID: id, CanInvite: member.GetBool("can_invite"),
		Allowed: right.Allowed, Reason: right.Reason, CooldownEndsAt: optionalUTC(right.CooldownEndsAt)})
}

// optionalUTC returns t in UTC, or nil for the zero time.
func optionalUTC(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	t = t.UTC()
	return &t
}
