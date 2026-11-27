package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/boundary"
	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Paths of the invite routes.
const (
	// InvitesPath lists the invites (GET: maintainers all, members their own) and creates one (POST: maintainers, and
	// members who may invite, domain.CanInvite).
	InvitesPath = boundary.OSFMPrefix + "invites"
	// InviteRevokePath revokes an invite (POST): maintainers any, members their own (others' are 404).
	InviteRevokePath = boundary.OSFMPrefix + "invites/{uuid}/revoke"
	// InviteCheckPath tells whether an invite code can be used (POST, public): the registration page asks it before
	// showing the form.
	InviteCheckPath = boundary.OSFMPrefix + "invites/check"
)

// maxInviteBody bounds the request bodies of the invite routes.
const maxInviteBody = 1 << 10

// Error codes of creating an invite.
const (
	// CodeInviteNotAllowed (403): the member may not invite now. The error's data says why, {"reason":
	// "cooldown"|"disabled"}, and when the cooling-off period ends, {"cooldownEndsAt": RFC 3339} (D12).
	CodeInviteNotAllowed = "invite_not_allowed"
	// CodeInviteLimitReached (409): the member has domain.MaxActiveInvites active invites already; the data has
	// {"limit"}.
	CodeInviteLimitReached = "invite_limit_reached"
)

// MemberRef names a member in a response: the uuid and the main name.
type MemberRef struct {
	UUID        string `json:"uuid"`
	DisplayName string `json:"displayName"`
}

// InviteInfo is an invite as the list shows it. Times are RFC 3339 in UTC; usedBy, usedAt and revokedAt are null
// while not set. The code is not part of it: only its hash is stored.
type InviteInfo struct {
	UUID      string     `json:"uuid"`
	State     string     `json:"state"` // InviteActive, InviteUsed, InviteExpired or InviteRevoked
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	CreatedBy *MemberRef `json:"createdBy"`
	UsedBy    *MemberRef `json:"usedBy"`
	UsedAt    *time.Time `json:"usedAt"`
	RevokedAt *time.Time `json:"revokedAt"`
}

// InviteList is the response of GET [InvitesPath], newest first: every invite for a maintainer, the member's own for a
// member.
type InviteList struct {
	Invites []InviteInfo `json:"invites"`
}

// CreateInviteRequest is the body of POST [InvitesPath]; an empty body means [DefaultInviteDays].
type CreateInviteRequest struct {
	Days int `json:"days"`
}

// CreatedInvite is the response of POST [InvitesPath]. code is shown this once: the link the web app builds from it
// cannot be shown again.
type CreatedInvite struct {
	Invite InviteInfo `json:"invite"`
	Code   string     `json:"code"`
}

// RevokedInvite is the response of POST [InviteRevokePath].
type RevokedInvite struct {
	Invite InviteInfo `json:"invite"`
}

// CheckInviteRequest is the body of POST [InviteCheckPath].
type CheckInviteRequest struct {
	Code string `json:"code"`
}

// CheckInviteResponse is the response of POST [InviteCheckPath]: only whether the code can be used, the same for
// every invalid code.
type CheckInviteResponse struct {
	Valid bool `json:"valid"`
}

// bindInviteRoutes registers the invite routes.
func bindInviteRoutes(se *core.ServeEvent) {
	// Members use these routes too (D12): the handlers decide what each member may see and do.
	se.Router.GET(InvitesPath, handleListInvites)
	se.Router.POST(InvitesPath, handleCreateInvite).Bind(MemberWriteBudget())
	se.Router.POST(InviteRevokePath, handleRevokeInvite).Bind(MemberWriteBudget())
	Public(se.Router.POST(InviteCheckPath, handleCheckInvite)).Bind(RateLimit(LimitPublic))
}

// errEmptyBody is returned by [readJSONLimit] for a request without a body.
var errEmptyBody = errors.New("auth: empty request body")

// readJSON decodes a small JSON body into v; an empty body leaves v as it is.
func readJSON(e *core.RequestEvent, v any) error {
	if err := readJSONLimit(e, v, maxInviteBody); err != nil && !errors.Is(err, errEmptyBody) {
		return err
	}
	return nil
}

// readJSONLimit decodes a JSON body of at most limit bytes into v; an empty body is [errEmptyBody].
func readJSONLimit(e *core.RequestEvent, v any, limit int64) error {
	body, err := io.ReadAll(http.MaxBytesReader(e.Response, e.Request.Body, limit))
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return errEmptyBody
	}
	return json.Unmarshal(body, v)
}

func handleListInvites(e *core.RequestEvent) error {
	// Newest first; the uuid (UUIDv7, increasing within a millisecond too) orders invites created in the same
	// millisecond, which the random record IDs did not.
	q := store.Query(e.App, store.Invites).OrderBy("created DESC", "uuid DESC")
	if m := CurrentMember(e); !IsMaintainer(m) {
		q = q.Where(dbx.HashExp{store.Invites + ".created_by": m.Id})
	}
	invites, err := q.All()
	if err != nil {
		return e.InternalServerError("", err)
	}
	infos, err := inviteInfos(e.App, invites)
	if err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, InviteList{Invites: infos})
}

func handleCreateInvite(e *core.RequestEvent) error {
	req := CreateInviteRequest{Days: DefaultInviteDays}
	if err := readJSON(e, &req); err != nil {
		return e.BadRequestError("", nil)
	}
	if req.Days < MinInviteDays || req.Days > MaxInviteDays {
		return e.BadRequestError("days must be between 1 and 30.", nil)
	}
	code, invite, err := CreateInvite(e.App, CurrentMember(e), req.Days)
	var notAllowed *InviteNotAllowedError
	switch {
	case errors.As(err, &notAllowed):
		data := map[string]any{"reason": notAllowed.Right.Reason}
		if t := notAllowed.Right.CooldownEndsAt; !t.IsZero() {
			data["cooldownEndsAt"] = t.UTC().Format(time.RFC3339)
		}
		return authErrorData(e, http.StatusForbidden, CodeInviteNotAllowed, "You may not invite now.", data)
	case errors.Is(err, ErrInviteLimit):
		return authErrorData(e, http.StatusConflict, CodeInviteLimitReached, "You have too many active invites.",
			map[string]any{"limit": domain.MaxActiveInvites})
	case err != nil:
		return e.InternalServerError("", err)
	}
	infos, err := inviteInfos(e.App, []*core.Record{invite})
	if err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusCreated, CreatedInvite{Invite: infos[0], Code: code})
}

func handleRevokeInvite(e *core.RequestEvent) error {
	id := e.Request.PathValue("uuid")
	if !store.IsUUID(id) {
		return e.NotFoundError("", nil)
	}
	invite, err := store.FindByUUID(e.App, store.Invites, id)
	m := CurrentMember(e)
	// Another member's invite answers as a missing one.
	if err != nil || (!IsMaintainer(m) && invite.GetString("created_by") != m.Id) {
		return e.NotFoundError("", nil)
	}
	switch err := RevokeInvite(e.App, invite, m); {
	case errors.Is(err, ErrInviteUsed):
		return e.Error(http.StatusConflict, "The invite has been used.", nil)
	case err != nil:
		return e.InternalServerError("", err)
	}
	infos, err := inviteInfos(e.App, []*core.Record{invite})
	if err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, RevokedInvite{Invite: infos[0]})
}

// handleCheckInvite answers POST [InviteCheckPath]: 200 with [CheckInviteResponse], the same body for every invalid
// code. A body that is not JSON is a 400, which says nothing about any code.
func handleCheckInvite(e *core.RequestEvent) error {
	var req CheckInviteRequest
	if err := readJSON(e, &req); err != nil {
		return e.BadRequestError("", nil)
	}
	valid, err := CheckInvite(e.App, req.Code)
	if err != nil {
		return e.InternalServerError("", err)
	}
	if !valid {
		attemptFailed(e)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, CheckInviteResponse{Valid: valid})
}

// inviteInfos projects invites for the list, with the inviter and the new member by uuid and main name. Members are
// looked up soft-deleted ones included: the list keeps showing who invited and who joined.
func inviteInfos(app core.App, invites []*core.Record) ([]InviteInfo, error) {
	ids := map[string]bool{}
	for _, inv := range invites {
		for _, f := range []string{"created_by", "used_by"} {
			if id := inv.GetString(f); id != "" {
				ids[id] = true
			}
		}
	}
	refs := map[string]*MemberRef{}
	if len(ids) > 0 {
		list := make([]any, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		members, err := store.Query(app, store.Members).IncludeDeleted().Where(dbx.In(store.Members+".id", list...)).All()
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			refs[m.Id] = &MemberRef{UUID: m.GetString(store.FieldUUID), DisplayName: m.GetString("display_name")}
		}
	}

	t := now().UTC()
	out := make([]InviteInfo, 0, len(invites))
	for _, inv := range invites {
		out = append(out, InviteInfo{
			UUID:      inv.GetString(store.FieldUUID),
			State:     InviteState(inv, t),
			CreatedAt: inv.GetDateTime(store.FieldCreated).Time().UTC(),
			ExpiresAt: inv.GetDateTime("expires_at").Time().UTC(),
			CreatedBy: refs[inv.GetString("created_by")],
			UsedBy:    refs[inv.GetString("used_by")],
			UsedAt:    optionalTime(inv, "used_at"),
			RevokedAt: optionalTime(inv, "revoked_at"),
		})
	}
	return out, nil
}

func optionalTime(r *core.Record, field string) *time.Time {
	d := r.GetDateTime(field)
	if d.IsZero() {
		return nil
	}
	t := d.Time().UTC()
	return &t
}

// authErrorData is [authError] with data, for errors that tell the client more than their code.
func authErrorData(e *core.RequestEvent, status int, code, message string, data map[string]any) error {
	e.Set(authErrorCodeKey, code)
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(status, AuthError{Status: status, Message: message, Data: data, Code: code})
}
