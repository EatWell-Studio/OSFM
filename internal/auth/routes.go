package auth

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Me is the response of GET /api/osfm/me: the signed-in member as the web app needs them. displayName is the main
// name; the pseudonym is not part of it.
type Me struct {
	UUID        string `json:"uuid"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"` // RoleMember or RoleMaintainer
}

// meOf returns member as [Me]: the shape of GET /api/osfm/me and of the sign-in and registration responses.
func meOf(member *core.Record) Me {
	return Me{
		UUID:        member.GetString(store.FieldUUID),
		DisplayName: member.GetString("display_name"),
		Role:        member.GetString("role"),
	}
}

// handleMe answers GET /api/osfm/me: 200 with [Me]; without a session the middleware has answered 401 already.
func handleMe(e *core.RequestEvent) error {
	m := CurrentMember(e)
	if m == nil {
		return e.UnauthorizedError("", nil)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, meOf(m))
}

// handleLogout answers POST /api/osfm/logout: ends the current session and clears the cookie, 204. The member's
// other sessions (other devices) are not affected.
func handleLogout(e *core.RequestEvent) error {
	if m := CurrentMember(e); m != nil {
		auditApp(e.App, m, AuditSignOut, m.GetString(store.FieldUUID), nil)
	}
	if err := SignOut(e); err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.NoContent(http.StatusNoContent)
}
