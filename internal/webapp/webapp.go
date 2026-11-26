// Package webapp serves the web app on the main listener and sets the HTTP security baseline there (§4.7):
//
//   - security headers on every response of the main listener, API and errors included: a strict content security
//     policy, no referrer, no MIME sniffing, no framing, a permissions policy that allows only geolocation (for "show
//     my location"), cross-origin isolation of windows and resources, and HSTS when the instance is served over HTTPS
//     at a public host name ([Headers]); they replace PocketBase's own defaults there;
//   - no CORS on the main listener: the app is same-origin, so no other origin may read its responses or send it
//     preflighted requests; PocketBase's CORS middleware (serve --origins) still applies on the admin listener;
//   - the built single-page app with explicit content types and caching: hashed files under /assets/ are immutable,
//     everything else is revalidated (no-cache, with an ETag); paths that look like files and do not exist get 404,
//     and only client-side routes fall back to index.html ([Handler]).
//
// The admin listener (internal/boundary) is left alone: PocketBase's admin UI sets its own content security policy,
// which needs inline styles and the OpenStreetMap tile server among others.
package webapp

import (
	"io/fs"
	"net/http"
	"slices"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"

	"github.com/EatWell-Studio/OSFM/internal/boundary"
)

// Options configures [Bind].
type Options struct {
	// FS holds the built web app; index.html at its root.
	FS fs.FS
	// HSTS adds Strict-Transport-Security to the main listener's responses. Set it when the instance is served over
	// HTTPS at a public host name (config.Config.PublicHTTPS).
	HSTS bool
}

// Middleware IDs and priorities.
const (
	// HeadersMiddlewareID sets the security headers. It runs before every other middleware, PocketBase's own included,
	// so that every response carries the headers: refusals by the boundary and the session layer, rate limiting and
	// errors.
	HeadersMiddlewareID       = "osfmSecurityHeaders"
	HeadersMiddlewarePriority = apis.DefaultCorsMiddlewarePriority - 1
)

// Bind installs on app, when serving: the security headers middleware, the restriction of PocketBase's CORS and
// security headers middlewares to the admin listener, and the web app on the catch-all route GET /{path...} (unless a
// route is already registered there). It fails the start when the web app cannot be read.
func Bind(app core.App, opts Options) {
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.Router.Bind(headersMiddleware(Headers(opts.HSTS)))
		adminOnly(se, apis.DefaultCorsMiddlewareId, apis.DefaultSecurityHeadersMiddlewareId)
		if !se.Router.HasRoute(http.MethodGet, "/{path...}") {
			h, err := Handler(opts.FS)
			if err != nil {
				return err
			}
			// The files stay out of the request log, as with PocketBase's own static handler; 404s are still logged.
			se.Router.GET("/{path...}", h).Bind(apis.SkipSuccessActivityLog())
		}
		return se.Next()
	})
}

func headersMiddleware(headers http.Header) *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       HeadersMiddlewareID,
		Priority: HeadersMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			if !boundary.OnAdminListener(e.Request) {
				h := e.Response.Header()
				for name, values := range headers {
					h[name] = values
				}
			}
			return e.Next()
		},
	}
}

// adminOnly makes PocketBase's own middlewares with the given IDs, which apis.Serve and apis.NewRouter bind before
// the OnServe hooks run, skip the main listener; they keep working as before on the admin listener.
//
//   - CORS (apis.DefaultCorsMiddlewareId): PocketBase answers any origin with Access-Control-Allow-Origin: * (its
//     default for serve --origins) and answers preflight requests. The app never makes a cross-origin request to its
//     own server, and the session cookie is the only credential there, so the main listener sends no CORS headers at
//     all: browsers then keep other origins from reading its responses and refuse their preflighted requests (the
//     X-OSFM header, JSON bodies).
//   - Security headers (apis.DefaultSecurityHeadersMiddlewareId): PocketBase sets X-Frame-Options: SAMEORIGIN, which
//     would contradict frame-ancestors 'none', and X-XSS-Protection: 1; mode=block, which current browsers ignore and
//     older ones could be abused with; [Headers] replaces them on the main listener.
func adminOnly(se *core.ServeEvent, ids ...string) {
	for i, m := range se.Router.Middlewares {
		if m == nil || !slices.Contains(ids, m.Id) {
			continue
		}
		next := m.Func
		se.Router.Middlewares[i] = &hook.Handler[*core.RequestEvent]{
			Id:       m.Id,
			Priority: m.Priority,
			Func: func(e *core.RequestEvent) error {
				if boundary.OnAdminListener(e.Request) {
					return next(e)
				}
				return e.Next()
			},
		}
	}
}
