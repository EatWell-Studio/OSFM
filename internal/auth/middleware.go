package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/EatWell-Studio/OSFM/internal/boundary"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/tiles"
)

// Paths and headers of the session layer.
const (
	// MePath answers with the signed-in member (GET).
	MePath = boundary.OSFMPrefix + "me"
	// LogoutPath ends the current session (POST).
	LogoutPath = boundary.OSFMPrefix + "logout"

	// CSRFHeader must be sent, with the value "1", on every state-changing request to /api/osfm/* (AD6). A cross-site
	// form cannot add a header, and a cross-origin script cannot send one: the main listener answers preflight
	// requests without any CORS header (internal/webapp), so the browser's preflight fails.
	CSRFHeader = "X-OSFM"
)

// Middleware IDs and priorities. Lower priorities run first. The boundary middleware runs before all of them, so a
// request it refuses is never looked up.
const (
	// LoadSessionMiddlewareID reads the session cookie and, for a valid session, sets e.Auth to the member. It runs
	// before PocketBase's rate limiter, which then applies its rules for signed-in callers (audience "@auth") to
	// members; it still counts per client address. It also runs before PocketBase's own token loader, which leaves
	// e.Auth alone once it is set (and finds no Authorization header on the main listener anyway, see
	// internal/boundary).
	LoadSessionMiddlewareID       = "osfmLoadSession"
	LoadSessionMiddlewarePriority = boundary.MiddlewarePriority + 3

	// RequireSessionMiddlewareID answers 401 to a request for a protected path without a session. It runs after
	// PocketBase's rate limiter, so that refused requests count towards the limits too. [Public] removes it from a
	// route.
	RequireSessionMiddlewareID       = "osfmRequireSession"
	RequireSessionMiddlewarePriority = apis.DefaultRateLimitMiddlewarePriority + 1

	// CSRFMiddlewareID answers 403 to a state-changing request to /api/osfm/* without the [CSRFHeader]. It applies to
	// the public routes too: signing in must not be forgeable either.
	CSRFMiddlewareID       = "osfmCSRF"
	CSRFMiddlewarePriority = RequireSessionMiddlewarePriority + 1

	// RequireMaintainerMiddlewareID is the route middleware of [RequireMaintainer]. Its priority is the default (0),
	// so it runs after all of the above.
	RequireMaintainerMiddlewareID = "osfmRequireMaintainer"
)

// protectedPrefixes are the paths that need a session (E1): the OSFM API, except the routes marked [Public], and the
// basemap tiles, the city index and the TileJSON included. Everything else is public: the web app's files, PocketBase's
// health check and /.well-known/osfm.json (federation).
var protectedPrefixes = []string{boundary.OSFMPrefix, tiles.RoutePrefix}

// Request store keys of the current session and member.
const (
	sessionKey = "osfmSession"
	memberKey  = "osfmMember"
)

// Options configures [Bind].
type Options struct {
	// HostPrefix names the session cookie [HostCookieName] instead of [CookieName]. Set it when the instance is served
	// over HTTPS at a public host name (config.Config.PublicHTTPS).
	HostPrefix bool

	// PasskeyRPID is the WebAuthn relying party ID and PasskeyOrigins the origins passkey ceremonies are accepted
	// from (config.Config.PasskeyRP). When PasskeyRPID is empty, "localhost" with http://localhost:8090 is used.
	PasskeyRPID    string
	PasskeyOrigins []string

	// MasterKey returns the instance master key (config.Keys.Master): TOTP secrets are stored encrypted with it, and
	// the pending registrations and sign-in tickets are sealed with it. It is called when needed, not by Bind, so it
	// may return a key that is only loaded when the app bootstraps. Without it the password and TOTP routes answer
	// 500.
	MasterKey func() []byte

	// SetupCodeFile is where the setup code is written while the instance has no maintainer (setup.go), normally
	// <data>/secrets/[SetupCodeFileName]; empty writes no file. Logger receives the setup code and other startup
	// messages; nil means slog.Default().
	SetupCodeFile string
	Logger        *slog.Logger
	// Limits are the rate limits and lockouts of the public authentication routes ([RateLimit]); nil means
	// [DefaultLimits].
	Limits *Limits
}

// Bind installs the session layer on app:
//
//   - the middlewares (session cookie, default deny, CSRF header) and the routes GET [MePath] and POST [LogoutPath],
//     when serving;
//   - the invite routes ([bindInviteRoutes]) and the password and TOTP routes ([bindPasswordTOTPRoutes]);
//   - the passkey registration and sign-in routes ([bindPasskeyRoutes]) for the relying party in opts;
//   - the rate limits of the public authentication routes ([RateLimit], with opts.Limits);
//   - a hook that deletes a member's sessions and revokes their open invites as soon as they leave, are removed or
//     are soft-deleted;
//   - a daily job that deletes expired sessions;
//   - the setup wizard's routes ([bindSetupRoutes]), and the setup code when the instance starts unconfigured.
func Bind(app core.App, opts Options) {
	name := CookieName
	if opts.HostPrefix {
		name = HostCookieName
	}
	app.Store().Set(cookieNameKey, name)
	if opts.MasterKey != nil {
		app.Store().Set(masterKeyStoreKey, opts.MasterKey)
	}

	rpID, origins := opts.PasskeyRPID, opts.PasskeyOrigins
	if rpID == "" {
		rpID, origins = "localhost", []string{"http://localhost:8090"}
	}
	p, err := newPasskeys(rpID, origins)
	if err != nil {
		panic(fmt.Sprintf("auth: passkey setup: %v", err)) // only if the system's random source fails
	}
	app.Store().Set(passkeysKey, p)
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	app.Store().Set(setupCodeKey, &setupCode{file: opts.SetupCodeFile, logger: logger})
	limits := DefaultLimits()
	if opts.Limits != nil {
		limits = *opts.Limits
	}
	app.Store().Set(limiterKey, newLimiter(limits))
	if p.err != nil {
		app.Logger().Warn("passkeys are not available: the relying party configuration is not valid",
			"rp_id", rpID, "error", p.err)
	}

	app.OnRecordUpdate(store.Members).BindFunc(func(e *core.RecordEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		original := e.Record.Original()
		if !CanSignIn(original) || CanSignIn(e.Record) {
			return nil
		}
		reason := RevokeLeft
		if store.IsDeleted(e.Record) {
			reason = RevokeDeleted
		}
		if _, err := RevokeSessions(e.App, e.Record, nil, reason); err != nil {
			return err
		}
		return revokeOpenInvites(e.App, e.Record, reason)
	})

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		e.App.Cron().MustAdd("osfmDeleteExpiredSessions", "23 4 * * *", func() {
			if _, err := DeleteExpiredSessions(e.App); err != nil {
				e.App.Logger().Warn("could not delete expired sessions", "error", err)
			}
		})
		return nil
	})

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.Router.Bind(loadSession(), requireSession(), requireCSRFHeader())
		se.Router.GET(MePath, handleMe)
		se.Router.POST(LogoutPath, handleLogout)
		bindInviteRoutes(se)
		bindPasswordTOTPRoutes(se)
		bindPasskeyRoutes(se)
		bindSetupRoutes(se)
		if err := startSetup(se.App, logger); err != nil {
			return err
		}
		// The first sign-in attempt with an unknown login name would otherwise also pay for the dummy hash.
		go func() { _ = verifyDummy(context.Background(), "") }()
		return se.Next()
	})
}

// Public makes route reachable without a session and returns it: for the sign-in, registration and credential reset
// endpoints only (E1). The CSRF header is still required for its state-changing requests, and the session cookie is
// still read, so [CurrentMember] and [SignIn] work as on any other route.
func Public(route *router.Route[*core.RequestEvent]) *router.Route[*core.RequestEvent] {
	return route.Unbind(RequireSessionMiddlewareID)
}

// IsMaintainer reports whether member is a maintainer (D6) who may sign in.
func IsMaintainer(member *core.Record) bool {
	return CanSignIn(member) && member.GetString("role") == RoleMaintainer
}

// RequireMaintainer returns a route middleware that lets only maintainers through: 401 without a session (in case the
// route was marked [Public] by mistake), 403 for a member who is not a maintainer. Bind it to every maintainer route:
// route.Bind(auth.RequireMaintainer()).
func RequireMaintainer() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: RequireMaintainerMiddlewareID,
		Func: func(e *core.RequestEvent) error {
			m := CurrentMember(e)
			if m == nil {
				return e.UnauthorizedError("", nil)
			}
			if !IsMaintainer(m) {
				return e.ForbiddenError("Only maintainers may do this.", nil)
			}
			return e.Next()
		},
	}
}

// CurrentMember returns the member of the request's session, or nil without one. Use it rather than e.Auth: on the
// admin listener e.Auth can also be a superuser.
func CurrentMember(e *core.RequestEvent) *core.Record {
	m, _ := e.Get(memberKey).(*core.Record)
	return m
}

func currentSession(e *core.RequestEvent) *core.Record {
	s, _ := e.Get(sessionKey).(*core.Record)
	return s
}

// SignIn starts a session for member, sets the session cookie on the response and makes member the request's
// member. A session the request already had is ended first. It fails with [ErrCannotSignIn] for a member who may
// not sign in. Call it only once every factor has been verified (D4).
func SignIn(e *core.RequestEvent, member *core.Record) error {
	token, session, err := CreateSession(e.App, member)
	if err != nil {
		return err
	}
	if old := currentSession(e); old != nil {
		deleteSession(e.App, old)
	}
	e.SetCookie(sessionCookie(SessionCookieName(e.App), token, session.GetDateTime("expires_at").Time()))
	setSession(e, session, member)
	return nil
}

// SignOut ends the request's session, if it has one, and clears the session cookie.
func SignOut(e *core.RequestEvent) error {
	if s := currentSession(e); s != nil {
		if err := e.App.Delete(s); err != nil {
			return err
		}
	}
	e.SetCookie(clearedCookie(SessionCookieName(e.App)))
	e.Set(sessionKey, nil)
	e.Set(memberKey, nil)
	e.Auth = nil
	return nil
}

func setSession(e *core.RequestEvent, session, member *core.Record) {
	e.Set(sessionKey, session)
	e.Set(memberKey, member)
	e.Auth = member
}

func loadSession() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       LoadSessionMiddlewareID,
		Priority: LoadSessionMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			if !under(e.Request, protectedPrefixes...) {
				return e.Next()
			}
			cookie, err := e.Request.Cookie(SessionCookieName(e.App))
			if err != nil {
				return e.Next() // no cookie
			}
			session, member, err := lookup(e.App, cookie.Value)
			switch {
			case err == nil:
				setSession(e, session, member)
			case errors.Is(err, errNoSession):
				// Expired, revoked or made up: the browser can forget it.
				e.SetCookie(clearedCookie(SessionCookieName(e.App)))
			default:
				return e.InternalServerError("", err)
			}
			return e.Next()
		},
	}
}

func requireSession() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       RequireSessionMiddlewareID,
		Priority: RequireSessionMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			if under(e.Request, protectedPrefixes...) && currentSession(e) == nil {
				return e.UnauthorizedError("", nil)
			}
			return e.Next()
		},
	}
}

func requireCSRFHeader() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       CSRFMiddlewareID,
		Priority: CSRFMiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			if under(e.Request, boundary.OSFMPrefix) && !safeMethod(e.Request.Method) &&
				e.Request.Header.Get(CSRFHeader) != "1" {
				return e.ForbiddenError("The "+CSRFHeader+" header is missing.", nil)
			}
			return e.Next()
		},
	}
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// under reports whether the request's path, or the path of the route pattern it matched, starts with one of
// prefixes. Both are checked, as the boundary does, so that a path the router reads differently from URL.Path cannot
// slip past.
func under(r *http.Request, prefixes ...string) bool {
	route := r.Pattern
	if i := strings.IndexByte(route, ' '); i >= 0 {
		route = route[i+1:]
	}
	for _, p := range prefixes {
		if strings.HasPrefix(r.URL.Path, p) || strings.HasPrefix(route, p) {
			return true
		}
	}
	return false
}
