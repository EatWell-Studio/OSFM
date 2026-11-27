// Package boundary keeps PocketBase's own HTTP surface away from clients (AD2) and applies the PocketBase settings the
// boundary depends on.
//
// On the main listener, the one the reverse proxy forwards to, only these are served:
//
//   - /api/osfm/*: the OSFM API, for routes OSFM registers there;
//   - /api/health: the health check used by the container and the reverse proxy;
//   - every path outside /api/ and /_/: the web app, the basemap tiles and the like.
//
// Every other path under /api/ (PocketBase's record, collection, auth, realtime, file, batch, settings, log, backup
// and cron APIs, and unknown paths) and the admin UI under /_/ get a JSON 404, whatever the request carries. The
// Authorization header is removed from the requests that do get through: on the main listener the only credential is
// the session cookie (AD6), so a PocketBase token, a superuser's included, carries no weight there.
//
// With the admin UI enabled (OSFM_ADMIN_UI), the admin UI and all of PocketBase's APIs are served on a second,
// separate listener (OSFM_ADMIN_LISTEN, default 127.0.0.1:8091) that the reverse proxy does not know about; operators
// reach it through an SSH tunnel. Checking whether a request on the main listener comes from the local machine would
// not do: behind a reverse proxy on the same host, every request arrives from a loopback address.
package boundary

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// Paths of the main listener's boundary.
const (
	// OSFMPrefix starts every path of the OSFM API.
	OSFMPrefix = "/api/osfm/"
	// HealthPath is PocketBase's health check, the only PocketBase route the main listener serves.
	HealthPath = "/api/health"

	apiRoot   = "/api"
	adminRoot = "/_"
)

const (
	// MiddlewareID is the ID of the boundary middleware on the router.
	MiddlewareID = "osfmBoundary"
	// MiddlewarePriority places the middleware after PocketBase's activity logger and panic recovery, so that refused
	// requests are logged, and before it reads the auth token and applies its rate limits, so that refused requests
	// cost nothing and a token in the Authorization header is never loaded on the main listener.
	MiddlewarePriority = apis.DefaultLoadAuthTokenMiddlewarePriority - 5
)

// Options configures [Bind].
type Options struct {
	// AdminUI serves the admin UI and PocketBase's APIs on a separate listener at AdminListen.
	AdminUI bool
	// AdminListen is the TCP address of the admin listener.
	AdminListen string
	// TrustedProxyHeaders, when not empty, replace PocketBase's trusted proxy headers at every bootstrap (see
	// [ApplyTrustedProxy]).
	TrustedProxyHeaders []string
	// Logger receives the startup messages; nil means slog.Default().
	Logger *slog.Logger
}

// listenAdmin opens the admin listener; tests replace it to learn the address of a listener on port 0.
var listenAdmin = net.Listen

// adminConnKey marks the context of requests that arrived on the admin listener. Only the admin server sets it, so a
// client cannot.
type adminConnKey struct{}

// Bind installs the boundary on app:
//
//   - at every bootstrap, the request log settings (see [ApplyLogSettings]) and, when configured, the trusted proxy
//     headers (see [ApplyTrustedProxy]);
//   - when serving, the boundary middleware on the router; with the admin UI enabled, the admin listener; and
//     PocketBase's first-start installer link only when the admin UI is enabled, pointing at the admin listener.
//
// The `superuser` command is unaffected: it works on the database directly.
func Bind(app core.App, opts Options) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		if err := ApplyLogSettings(e.App); err != nil {
			return err
		}
		return ApplyTrustedProxy(e.App, opts.TrustedProxyHeaders)
	})

	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Id: MiddlewareID,
		Func: func(se *core.ServeEvent) error {
			se.Router.Bind(middleware())
			// Without this route, net/http answers /_ itself with a redirect to the admin UI's /_/, before any
			// middleware runs. The handler is reached only on the admin listener.
			if !se.Router.HasRoute(http.MethodGet, adminRoot) {
				se.Router.GET(adminRoot, func(e *core.RequestEvent) error {
					return e.Redirect(http.StatusTemporaryRedirect, adminRoot+"/")
				})
			}
			if !opts.AdminUI {
				// The installer would create a placeholder superuser and print a link into the admin UI, which is
				// not served. Superusers can still be created with `osfm superuser upsert EMAIL PASS`.
				se.InstallerFunc = nil
				if err := se.Next(); err != nil {
					return err
				}
				logServing(logger, se, "")
				return nil
			}
			return serveAdmin(se, opts.AdminListen, logger)
		},
	})
}

// serveAdmin opens the admin listener before the main server starts (so that a bad address stops the start, and the
// installer link can name it) and serves the main router's handler on it once that handler is built.
func serveAdmin(se *core.ServeEvent, addr string, logger *slog.Logger) error {
	ln, err := listenAdmin("tcp", addr)
	if err != nil {
		return fmt.Errorf("admin listener (OSFM_ADMIN_LISTEN=%s): %w", addr, err)
	}
	adminURL := "http://" + browsableAddr(ln.Addr().String())
	if se.InstallerFunc != nil {
		se.InstallerFunc = installer(adminURL, logger)
	}

	if err := se.Next(); err != nil {
		_ = ln.Close()
		return err
	}
	if se.Server == nil || se.Server.Handler == nil {
		// Not a real server start (a test that builds the router only).
		_ = ln.Close()
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	server := &http.Server{
		Handler:           se.Server.Handler,
		ReadHeaderTimeout: time.Minute,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		BaseContext: func(net.Listener) context.Context {
			return context.WithValue(ctx, adminConnKey{}, true)
		},
	}
	app := se.App
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("admin listener stopped", slog.Any("error", err))
		}
	}()
	app.OnTerminate().BindFunc(func(te *core.TerminateEvent) error {
		cancel()
		shutdownCtx, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		_ = server.Shutdown(shutdownCtx)
		return te.Next()
	})

	logServing(logger, se, adminURL)
	if !isLoopback(ln.Addr()) {
		logger.Warn("the admin listener is not bound to a loopback address; make sure only the local machine can reach "+
			"it (for a container, publish the port on 127.0.0.1 only)", slog.String("admin_listen", ln.Addr().String()))
	}
	return nil
}

func logServing(logger *slog.Logger, se *core.ServeEvent, adminURL string) {
	if se.Server == nil {
		return
	}
	if adminURL == "" {
		logger.Info("serving; the admin UI is off", slog.String("listen", se.Server.Addr))
		return
	}
	logger.Info("serving; the admin UI and PocketBase's APIs are on the admin listener only",
		slog.String("listen", se.Server.Addr), slog.String("admin_ui", adminURL+"/_/"))
}

// installer replaces PocketBase's installer: it prints the link into the admin UI's superuser installer on the admin
// listener, instead of the main one, and does not try to open a browser on the server.
func installer(adminURL string, logger *slog.Logger) func(core.App, *core.Record, string) error {
	return func(_ core.App, systemSuperuser *core.Record, _ string) error {
		token, err := systemSuperuser.NewStaticAuthToken(30 * time.Minute)
		if err != nil {
			return err
		}
		logger.Warn("no superuser yet: open this link within 30 minutes to create one, or run "+
			"`osfm superuser upsert EMAIL PASS`", slog.String("url", adminURL+"/_/#/pbinstall/"+token))
		return nil
	}
}

// middleware is the boundary middleware: see the package documentation.
func middleware() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id:       MiddlewareID,
		Priority: MiddlewarePriority,
		Func: func(e *core.RequestEvent) error {
			var err error
			switch {
			case OnAdminListener(e.Request):
				err = e.Next()
			case !publicAllowed(e.Request.URL.Path, e.Request.Pattern):
				err = e.NotFoundError("", nil)
			default:
				e.Request.Header.Del("Authorization")
				err = e.Next()
			}
			omitQueryFromLog(e)
			return err
		},
	}
}

// OnAdminListener reports whether r arrived on the admin listener. Only the admin server marks its requests, so a client
// cannot make a request on the main listener look like one.
func OnAdminListener(r *http.Request) bool {
	v, _ := r.Context().Value(adminConnKey{}).(bool)
	return v
}

// publicAllowed reports whether the main listener serves a request for path that the router matched to pattern (a
// net/http pattern such as "GET /api/health"). Both are checked, so that a path the router reads differently from
// URL.Path (escaped characters) cannot slip through.
func publicAllowed(path, pattern string) bool {
	route := pattern
	if i := strings.IndexByte(route, ' '); i >= 0 {
		route = route[i+1:]
	}
	switch {
	case under(path, adminRoot) || under(route, adminRoot):
		return false
	case !under(path, apiRoot) && !under(route, apiRoot):
		return true
	case strings.HasPrefix(path, OSFMPrefix) && strings.HasPrefix(route, OSFMPrefix):
		return true // a route OSFM registered; an unknown /api/osfm/ path matches the catch-all instead
	default:
		return path == HealthPath && route == HealthPath
	}
}

func under(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+"/")
}

// omitQueryFromLog drops the query string from the URL PocketBase's activity logger records once the handler has run,
// so that the request log never holds search terms, coordinates or tokens passed in a query (§3.9).
func omitQueryFromLog(e *core.RequestEvent) {
	if e.Request.URL.RawQuery == "" && !e.Request.URL.ForceQuery {
		return
	}
	u := *e.Request.URL
	u.RawQuery = ""
	u.ForceQuery = false
	r := e.Request.WithContext(e.Request.Context()) // a shallow copy
	r.URL = &u
	e.Request = r
}

// browsableAddr replaces an unspecified host (0.0.0.0, ::) by 127.0.0.1, for a link an operator can open.
func browsableAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func isLoopback(addr net.Addr) bool {
	tcp, ok := addr.(*net.TCPAddr)
	return ok && tcp.IP.IsLoopback()
}
