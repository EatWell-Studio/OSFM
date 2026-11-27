package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// DefaultMaxBody is the request body limit of a route that has no reason for another one: enough for a recommendation
// with its longest text and ten sources.
const DefaultMaxBody = 64 << 10

// Errors of [readJSON].
var (
	errBodyTooLarge = errors.New("api: request body too large")
	errEmptyBody    = errors.New("api: empty request body")
	errInvalidJSON  = errors.New("api: request body is not valid JSON")
)

// readJSON decodes the JSON body of the request, of at most limit bytes, into v. It returns errBodyTooLarge,
// errEmptyBody or errInvalidJSON; answer them with [requestError]:
//
//	var req CreatePlaceRequest
//	if err := readJSON(e, &req, DefaultMaxBody); err != nil {
//		return requestError(e, err)
//	}
func readJSON(e *core.RequestEvent, v any, limit int64) error {
	body, err := io.ReadAll(http.MaxBytesReader(e.Response, e.Request.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return errBodyTooLarge
		}
		return errInvalidJSON
	}
	if len(body) == 0 {
		return errEmptyBody
	}
	if err := json.Unmarshal(body, v); err != nil {
		return errInvalidJSON
	}
	return nil
}

// requestError answers an error of [readJSON]: 413 [CodeBodyTooLarge] or 400 [CodeInvalidRequest].
func requestError(e *core.RequestEvent, err error) error {
	if errors.Is(err, errBodyTooLarge) {
		return fail(e, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, "The request body is too large.")
	}
	return badRequest(e)
}

// pathUUID returns the path parameter name ({uuid} in the route pattern) when it is a uuid. Anything else cannot name
// an object (AD4): answer it with [notFound], as for a uuid that names nothing.
//
//	id, ok := pathUUID(e, "uuid")
//	if !ok {
//		return notFound(e)
//	}
func pathUUID(e *core.RequestEvent, name string) (string, bool) {
	id := e.Request.PathValue(name)
	return id, store.IsUUID(id)
}

// respond answers v as JSON with status. Unless the handler has set a Cache-Control header already (the map data,
// T027), the response is not cached: member and maintainer data must not stay in caches of shared devices or proxies.
func respond(e *core.RequestEvent, status int, v any) error {
	noStore(e)
	return e.JSON(status, v)
}

// noStore sets Cache-Control: no-store unless a Cache-Control header is set already.
func noStore(e *core.RequestEvent) {
	if e.Response.Header().Get("Cache-Control") == "" {
		e.Response.Header().Set("Cache-Control", "no-store")
	}
}

// MaintainersOnlyMiddlewareID is the route middleware of [MaintainersOnly].
const MaintainersOnlyMiddlewareID = "osfmAPIMaintainersOnly"

// MaintainersOnly returns the route middleware of the maintainer routes: 401 without a session (in case the route was
// marked public by mistake), 403 [CodeForbidden] for a member who is not a maintainer (D6). Add such a route to the
// maintainer list of its probes file (cmd/osfm/probes_<feature>_test.go).
func MaintainersOnly() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: MaintainersOnlyMiddlewareID,
		Func: func(e *core.RequestEvent) error {
			m := auth.CurrentMember(e)
			if m == nil {
				return e.UnauthorizedError("", nil)
			}
			if !auth.IsMaintainer(m) {
				return fail(e, http.StatusForbidden, CodeForbidden, "Only maintainers may do this.")
			}
			return e.Next()
		},
	}
}
