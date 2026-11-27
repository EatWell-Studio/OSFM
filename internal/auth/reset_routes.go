package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/boundary"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Paths of the reset links (reset.go).
const (
	// ResetLinksPath lists the reset links (GET) and creates one (POST, fresh); maintainers only.
	ResetLinksPath = boundary.OSFMPrefix + "reset-links"
	// ResetLinkRevokePath revokes a reset link (POST); maintainers only.
	ResetLinkRevokePath = boundary.OSFMPrefix + "reset-links/{uuid}/revoke"
	// ResetCheckPath tells whether a token can be used and whose account it resets (POST, public).
	ResetCheckPath = boundary.OSFMPrefix + "reset/check"
	// ResetPasswordStartPath and ResetPasswordConfirmPath reset to password and TOTP (POST, public).
	ResetPasswordStartPath   = boundary.OSFMPrefix + "reset/password/start"
	ResetPasswordConfirmPath = boundary.OSFMPrefix + "reset/password/confirm"
	// ResetPasskeyOptionsPath and ResetPasskeyPath reset to a passkey (POST, public).
	ResetPasskeyOptionsPath = boundary.OSFMPrefix + "reset/passkey/options"
	ResetPasskeyPath        = boundary.OSFMPrefix + "reset/passkey"
)

// Error codes of the reset routes.
const (
	CodeResetInvalid        = "reset_invalid"
	CodeMemberNotFound      = "member_not_found"
	CodeResetSelf           = "reset_self"
	CodeResetLinkUsed       = "reset_link_used"
	MethodResetLink         = "reset_link"
	maxResetLinkRequestBody = 1 << 10
)

// LimitResetEnrol: the last step of a reset writes the sign-in audit entry.
var LimitResetEnrol = Limit{signIn: MethodResetLink}

// ResetLinkInfo is a reset link as the maintainers' list shows it; usedAt and revokedAt are null while not set.
type ResetLinkInfo struct {
	UUID      string     `json:"uuid"`
	State     string     `json:"state"`
	Member    *MemberRef `json:"member"`
	CreatedBy *MemberRef `json:"createdBy"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	UsedAt    *time.Time `json:"usedAt"`
	RevokedAt *time.Time `json:"revokedAt"`
}

// ResetLinkList is the response of GET [ResetLinksPath], newest first.
type ResetLinkList struct {
	ResetLinks []ResetLinkInfo `json:"resetLinks"`
}

// CreateResetLinkRequest is the body of POST [ResetLinksPath]: the member's uuid, from the member list
// ([MembersPath]).
type CreateResetLinkRequest struct {
	Member string `json:"member"`
}

// CreatedResetLink is the response of POST [ResetLinksPath] (201): token is shown this once.
type CreatedResetLink struct {
	ResetLink ResetLinkInfo `json:"resetLink"`
	Token     string        `json:"token"`
}

// ResetLinkResponse is the response of POST [ResetLinkRevokePath].
type ResetLinkResponse struct {
	ResetLink ResetLinkInfo `json:"resetLink"`
}

// ResetTokenRequest is the body of POST [ResetCheckPath] and [ResetPasskeyOptionsPath].
type ResetTokenRequest struct {
	Token string `json:"token"`
}

// ResetCheckResponse is the response of POST [ResetCheckPath]: for a valid token, whose account it resets; for every
// invalid one the same {"valid": false}.
type ResetCheckResponse struct {
	Valid       bool   `json:"valid"`
	LoginName   string `json:"loginName,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// ResetPasswordStartRequest is the body of POST [ResetPasswordStartPath].
type ResetPasswordStartRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// ResetPasswordConfirmRequest is the body of POST [ResetPasswordConfirmPath].
type ResetPasswordConfirmRequest struct {
	Token     string `json:"token"`
	Enrolment string `json:"enrolment"`
	Code      string `json:"code"`
}

// ResetPasskeyRequest is the body of POST [ResetPasskeyPath].
type ResetPasskeyRequest struct {
	Token      string          `json:"token"`
	State      string          `json:"state"`
	Credential json.RawMessage `json:"credential"`
}

func bindResetRoutes(se *core.ServeEvent) {
	se.Router.GET(ResetLinksPath, handleListResetLinks).Bind(RequireMaintainer())
	se.Router.POST(ResetLinksPath, handleCreateResetLink).Bind(RequireMaintainer(), RequireFresh())
	se.Router.POST(ResetLinkRevokePath, handleRevokeResetLink).Bind(RequireMaintainer())
	Public(se.Router.POST(ResetCheckPath, handleResetCheck)).Bind(RateLimit(LimitPublic))
	Public(se.Router.POST(ResetPasswordStartPath, handleResetPasswordStart)).Bind(RateLimit(LimitPublic))
	Public(se.Router.POST(ResetPasswordConfirmPath, handleResetPasswordConfirm)).Bind(RateLimit(LimitResetEnrol))
	Public(se.Router.POST(ResetPasskeyOptionsPath, handleResetPasskeyOptions)).Bind(RateLimit(LimitPublic))
	Public(se.Router.POST(ResetPasskeyPath, handleResetPasskey)).Bind(RateLimit(LimitResetEnrol))
}

// resetError answers the errors of the reset routes; the others as the registration and passkey routes do.
func resetError(e *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, ErrInvalidResetLink):
		return authError(e, http.StatusForbidden, CodeResetInvalid, "The reset link is invalid or has expired.")
	case errors.Is(err, errResetMemberNotFound):
		return authError(e, http.StatusNotFound, CodeMemberNotFound, "No member who can sign in has this uuid.")
	case errors.Is(err, errResetSelf):
		return authError(e, http.StatusConflict, CodeResetSelf, "Manage your own credentials on your page.")
	case errors.Is(err, errResetLinkUsed):
		return authError(e, http.StatusConflict, CodeResetLinkUsed, "The reset link has been used.")
	default:
		return passkeyError(e, err)
	}
}

func handleListResetLinks(e *core.RequestEvent) error {
	links, err := e.App.FindRecordsByFilter(store.ResetLinks, "", "-created,-uuid", 0, 0)
	if err != nil {
		return e.InternalServerError("", err)
	}
	infos, err := resetLinkInfos(e.App, links)
	if err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, ResetLinkList{ResetLinks: infos})
}

func handleCreateResetLink(e *core.RequestEvent) error {
	var req CreateResetLinkRequest
	if err := readJSONLimit(e, &req, maxResetLinkRequestBody); err != nil {
		return badRequest(e)
	}
	token, link, err := CreateResetLink(e.App, CurrentMember(e), req.Member)
	if err != nil {
		return resetError(e, err)
	}
	infos, err := resetLinkInfos(e.App, []*core.Record{link})
	if err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusCreated, CreatedResetLink{ResetLink: infos[0], Token: token})
}

func handleRevokeResetLink(e *core.RequestEvent) error {
	id := e.Request.PathValue("uuid")
	if !store.IsUUID(id) {
		return e.NotFoundError("", nil)
	}
	link, err := e.App.FindFirstRecordByData(store.ResetLinks, store.FieldUUID, id)
	if err != nil {
		return e.NotFoundError("", nil)
	}
	if err := RevokeResetLink(e.App, link, CurrentMember(e)); err != nil {
		return resetError(e, err)
	}
	infos, err := resetLinkInfos(e.App, []*core.Record{link})
	if err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, ResetLinkResponse{ResetLink: infos[0]})
}

// handleResetCheck answers {"valid"} and, for a valid token, the member's names. Every invalid token gets the same
// response and counts as a failed attempt.
func handleResetCheck(e *core.RequestEvent) error {
	var req ResetTokenRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	_, member, err := findValidResetLink(e.App, req.Token)
	if err != nil && !errors.Is(err, ErrInvalidResetLink) {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	if err != nil {
		attemptFailed(e)
		return e.JSON(http.StatusOK, ResetCheckResponse{})
	}
	return e.JSON(http.StatusOK, ResetCheckResponse{Valid: true, LoginName: member.GetString("login_name"),
		DisplayName: member.GetString("display_name")})
}

func handleResetPasswordStart(e *core.RequestEvent) error {
	var req ResetPasswordStartRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	start, err := startResetPassword(e.Request.Context(), e.App, req.Token, req.Password)
	if err != nil {
		return resetError(e, err)
	}
	return enrolmentStarted(e, start)
}

func handleResetPasswordConfirm(e *core.RequestEvent) error {
	var req ResetPasswordConfirmRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	member, codes, err := confirmResetPassword(e.App, req.Token, req.Enrolment, req.Code)
	if err != nil {
		if errors.Is(err, errCodeInvalid) || errors.Is(err, errEnrolmentInvalid) {
			return registrationError(e, err)
		}
		return resetError(e, err)
	}
	return enrolled(e, member, codes, false)
}

func handleResetPasskeyOptions(e *core.RequestEvent) error {
	var req ResetTokenRequest
	if !readAuthJSON(e, &req) {
		return badRequest(e)
	}
	options, state, err := beginResetPasskey(e.App, req.Token)
	if err != nil {
		return resetError(e, err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, PasskeyOptionsResponse{Options: options, State: state})
}

func handleResetPasskey(e *core.RequestEvent) error {
	var req ResetPasskeyRequest
	if err := readPasskeyJSON(e, &req); err != nil {
		return badRequest(e)
	}
	member, codes, err := finishResetPasskey(e.App, req.Token, req.State, req.Credential)
	if err != nil {
		return resetError(e, err)
	}
	return enrolled(e, member, codes, true)
}

// resetLinkInfos projects reset links for the maintainers' list, with the member and the creator by uuid and main
// name (soft-deleted members included).
func resetLinkInfos(app core.App, links []*core.Record) ([]ResetLinkInfo, error) {
	ids := map[string]bool{}
	for _, l := range links {
		ids[l.GetString("member")] = true
		ids[l.GetString("created_by")] = true
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
	out := make([]ResetLinkInfo, 0, len(links))
	for _, l := range links {
		out = append(out, ResetLinkInfo{
			UUID:      l.GetString(store.FieldUUID),
			State:     ResetLinkState(l, t),
			Member:    refs[l.GetString("member")],
			CreatedBy: refs[l.GetString("created_by")],
			CreatedAt: l.GetDateTime(store.FieldCreated).Time().UTC(),
			ExpiresAt: l.GetDateTime("expires_at").Time().UTC(),
			UsedAt:    optionalTime(l, "used_at"),
			RevokedAt: optionalTime(l, "revoked_at"),
		})
	}
	return out, nil
}
