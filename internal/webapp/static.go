package webapp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Cache-Control values of the web app's files.
const (
	// CacheImmutable is for the files Vite names after their content (under /assets/): a new build gives a new name,
	// so a cached copy never has to be checked again.
	CacheImmutable = "public, max-age=31536000, immutable"
	// CacheRevalidate is for every other file, index.html (and so every client-side route), the service worker,
	// its registration script and the manifest among them: the browser may keep a copy but checks it with the
	// server (ETag, 304) before each use, so that a new version of the app takes effect on the next load.
	CacheRevalidate = "no-cache"
)

const indexFile = "index.html"

// assetsDir is Vite's output directory for the files it names after their content.
const assetsDir = "assets"

// hashedName matches the file names Vite gives under assets/: a name, a dash, an 8-character content hash and an
// extension (index-Bps8T2MF.js, maplibre-gl-worker-Boc9pDRP.mjs).
var hashedName = regexp.MustCompile(`^[^/]+-[A-Za-z0-9_-]{8}\.[a-z0-9]+$`)

// contentTypes are the content types of the web app's file types. They are set explicitly rather than taken from the
// system's MIME table, which differs between systems (a distroless image has none) and lacks some of these:
// browsers enforce them (X-Content-Type-Options: nosniff) for scripts, module workers and styles.
var contentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".webmanifest": "application/manifest+json",
	".map":         "application/json",
	".png":         "image/png",
	".svg":         "image/svg+xml",
	".ico":         "image/x-icon",
	".webp":        "image/webp",
	".woff2":       "font/woff2",
	".pbf":         "application/x-protobuf",
	".txt":         "text/plain; charset=utf-8",
	".md":          "text/markdown; charset=utf-8",
}

// file is a regular file of the web app.
type file struct {
	etag        string
	contentType string
	cache       string
}

// Handler returns the route handler of the web app in fsys, for a route with the {path...} wildcard. It answers:
//
//   - a file of fsys with its content type, an ETag (304 on a match) and its caching ([CacheImmutable] for the hashed
//     files under assets/, [CacheRevalidate] for the rest);
//   - 404 for a path that names a directory, lies under a directory of fsys (assets/, map/), or ends in a segment
//     with a dot (a file name: favicon.ico, a missing glyph range), when no such file exists: a missing script,
//     style or glyph range must not get the app's HTML (a script would fail with a misleading error; a 404 is what
//     MapLibre expects for a glyph range it cannot have);
//   - index.html for every other path, the app's client-side routes (/, /list, /me, /login, /admin and unknown
//     ones, which the app itself sends to the sign-in page). Client-side routes therefore must not end in a segment
//     with a dot, as with Vite's development server, and must not lie under a directory of fsys.
//
// fsys is read once, here; it must not change afterwards.
func Handler(fsys fs.FS) (func(*core.RequestEvent) error, error) {
	files := map[string]file{}
	dirs := map[string]bool{}
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir():
			if name != "." {
				dirs[name] = true
			}
			return nil
		case !d.Type().IsRegular():
			return nil
		}
		f, err := describe(fsys, name)
		if err != nil {
			return err
		}
		files[name] = f
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("webapp: %w", err)
	}
	if _, ok := files[indexFile]; !ok {
		return nil, errors.New("webapp: no " + indexFile)
	}

	return func(e *core.RequestEvent) error {
		name := strings.TrimPrefix(path.Clean("/"+e.Request.PathValue(apis.StaticWildcardParam)), "/")
		if name == "" {
			name = indexFile
		}
		f, ok := files[name]
		if !ok {
			if dirs[name] || underDir(name, dirs) || strings.Contains(path.Base(name), ".") {
				return e.NotFoundError("", nil)
			}
			name, f = indexFile, files[indexFile]
		}
		return serve(e, fsys, name, f)
	}, nil
}

// describe reads a file of the web app once, for its ETag, and decides its headers.
func describe(fsys fs.FS, name string) (file, error) {
	r, err := fsys.Open(name)
	if err != nil {
		return file{}, err
	}
	defer func() { _ = r.Close() }()
	sum := sha256.New()
	if _, err := io.Copy(sum, r); err != nil {
		return file{}, err
	}

	ext := strings.ToLower(path.Ext(name))
	ct, ok := contentTypes[ext]
	if !ok {
		ct = mime.TypeByExtension(ext) // empty: http.ServeContent sniffs it
	}
	cache := CacheRevalidate
	if dir, base := path.Split(name); dir == assetsDir+"/" && hashedName.MatchString(base) {
		cache = CacheImmutable
	}
	return file{
		etag:        `"` + hex.EncodeToString(sum.Sum(nil)[:16]) + `"`,
		contentType: ct,
		cache:       cache,
	}, nil
}

// underDir reports whether name lies in one of dirs (directories of the web app, other than its root).
func underDir(name string, dirs map[string]bool) bool {
	for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
		if dirs[dir] {
			return true
		}
	}
	return false
}

func serve(e *core.RequestEvent, fsys fs.FS, name string, f file) error {
	r, err := fsys.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	rs, ok := r.(io.ReadSeeker)
	if !ok {
		return fmt.Errorf("webapp: %s cannot seek", name)
	}
	h := e.Response.Header()
	h.Set("Cache-Control", f.cache)
	h.Set("ETag", f.etag)
	if f.contentType != "" {
		h.Set("Content-Type", f.contentType)
	}
	// The embedded files have no modification time; the ETag is the validator.
	http.ServeContent(e.Response, e.Request, name, time.Time{}, rs)
	return nil
}
