package api

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// Error is the body of every error response of this package: PocketBase's error shape (status, message, data) with a
// machine-readable code, the same as auth.AuthError. Clients choose their message by code; message is for people
// reading logs and is never shown as is.
type Error struct {
	Status  int            `json:"status"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
	Code    string         `json:"code"`
}

// Error codes shared by the features. A feature defines its own codes next to its handlers (such as
// "already_recommended"); codes are lowercase words joined by underscores and never change once released.
const (
	CodeInvalidRequest = "invalid_request" // 400: malformed JSON, a missing or invalid field (as auth's)
	CodeBodyTooLarge   = "body_too_large"  // 413: the body is larger than the route accepts
	CodeNotFound       = "not_found"       // 404: no such object, or one the caller may not see
	CodeForbidden      = "forbidden"       // 403: the caller may not do this
	CodeRateLimited    = "rate_limited"    // 429: over the budget; Retry-After says when to try again (as auth's)
)

// fail answers an [Error] with status and code, not cached.
func fail(e *core.RequestEvent, status int, code, message string) error {
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(status, Error{Status: status, Message: message, Data: map[string]any{}, Code: code})
}

// badRequest answers 400 [CodeInvalidRequest].
func badRequest(e *core.RequestEvent) error {
	return fail(e, http.StatusBadRequest, CodeInvalidRequest, "The request is not valid.")
}

// notFound answers 404 [CodeNotFound]. Use it for objects that do not exist and for objects the caller may not see
// alike, so that the answer does not tell them apart.
func notFound(e *core.RequestEvent) error {
	return fail(e, http.StatusNotFound, CodeNotFound, "Not found.")
}

// forbidden answers 403 [CodeForbidden].
func forbidden(e *core.RequestEvent) error {
	return fail(e, http.StatusForbidden, CodeForbidden, "You may not do this.")
}
