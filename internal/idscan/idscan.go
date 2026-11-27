// Package idscan is test support for AD4: no response of the instance may contain a PocketBase internal ID. It seeds
// made-up records into every collection ([Seed]), lists the routes registered on a PocketBase router ([Routes]),
// sends requests and searches every response's headers and body for the seeded IDs ([Scan]).
//
// The scan in cmd/osfm boots the app the way `osfm serve` does and requires a probe for every route OSFM registers,
// so a new route cannot be left out of the scan unnoticed. Only tests import this package.
package idscan

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// Route is a route registered on a router.
type Route struct {
	Method string // empty for a route that matches every method
	Path   string // the full path pattern, group prefixes included
}

// String returns the route as a net/http pattern: "GET /tiles/{path...}", or only the path for any method.
func (r Route) String() string {
	if r.Method == "" {
		return r.Path
	}
	return r.Method + " " + r.Path
}

var (
	routeType = reflect.TypeFor[*router.Route[*core.RequestEvent]]()
	groupType = reflect.TypeFor[*router.RouterGroup[*core.RequestEvent]]()
)

// Routes returns every route registered on r, in registration order.
//
// PocketBase's router has no API to list its routes: the tree of groups and routes is in an unexported field, read
// here with reflection (read-only). If a PocketBase upgrade changes that structure, Routes fails the test rather than
// returning an incomplete list.
func Routes(t testing.TB, r *router.Router[*core.RequestEvent]) []Route {
	t.Helper()
	var out []Route
	if err := walk(reflect.ValueOf(r.RouterGroup), "", &out); err != nil {
		t.Fatalf("idscan: list the routes: %v", err)
	}
	return out
}

func walk(group reflect.Value, prefix string, out *[]Route) error {
	if group.Type() != groupType || group.IsNil() {
		return fmt.Errorf("unexpected group type %v", group.Type())
	}
	g := group.Elem()
	prefix += g.FieldByName("Prefix").String()
	children := g.FieldByName("children")
	if !children.IsValid() || children.Kind() != reflect.Slice {
		return fmt.Errorf("router.RouterGroup has no children slice: PocketBase's router has changed")
	}
	for i := range children.Len() {
		child := children.Index(i)
		if child.Kind() == reflect.Interface {
			child = child.Elem()
		}
		switch child.Type() {
		case routeType:
			route := child.Elem()
			*out = append(*out, Route{
				Method: route.FieldByName("Method").String(),
				Path:   prefix + route.FieldByName("Path").String(),
			})
		case groupType:
			if err := walk(child, prefix, out); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected router child of type %v", child.Type())
		}
	}
	return nil
}

var placeholder = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)(\.\.\.)?\}`)

// Expand returns the paths obtained by replacing every placeholder of a route path ("{collection}", "{path...}") with
// each of its values in values, or with fallback when values has none for it.
func Expand(path string, values map[string][]string, fallback string) []string {
	loc := placeholder.FindStringSubmatchIndex(path)
	if loc == nil {
		return []string{path}
	}
	name := path[loc[2]:loc[3]]
	choices := values[name]
	if len(choices) == 0 {
		choices = []string{fallback}
	}
	var out []string
	for _, v := range choices {
		out = append(out, Expand(path[:loc[0]]+v+path[loc[1]:], values, fallback)...)
	}
	return out
}

// Request is one request of a scan.
type Request struct {
	Method string // GET when empty
	Path   string // path and query
	Body   string // sent as JSON when not empty
	Header http.Header
	// Status, when not zero, is the status the response must have: it shows that the request reached the handler it
	// is meant for rather than, say, a catch-all route.
	Status int
}

func (r Request) String() string {
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	return method + " " + r.Path
}

// Timeout bounds each request of a scan; a streaming endpoint (PocketBase's realtime API) answers until it expires.
const Timeout = time.Second

// clients numbers the client addresses of the scan's requests.
var clients atomic.Uint64

// ClientAddr returns a new client address (host and port) from the IPv6 documentation range, a different /64 network
// for every call. The scan sends each request from its own address, so that the sign-in rate limits, which count per
// client, do not answer its many requests with 429 instead of reaching the handlers.
func ClientAddr() string {
	n := clients.Add(1)
	return fmt.Sprintf("[2001:db8:%x:%x::1]:40000", n>>16&0xffff, n&0xffff)
}

// Scan sends each request to h and fails the test for every response that contains one of ids: in a header name or
// value (cookies, redirects, ETags included), in the body (gzip-decoded when the response says so), or in the decoded
// header or payload of a JSON Web Token found in either. It returns the number of responses it checked.
func Scan(t testing.TB, h http.Handler, reqs []Request, ids []string) int {
	t.Helper()
	n := 0
	for _, r := range reqs {
		method := r.Method
		if method == "" {
			method = http.MethodGet
		}
		ctx, cancel := context.WithTimeout(context.Background(), Timeout)
		req := httptest.NewRequestWithContext(ctx, method, r.Path, strings.NewReader(r.Body))
		req.RemoteAddr = ClientAddr()
		if r.Body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range r.Header {
			req.Header[k] = v
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		cancel()
		n++

		if r.Status != 0 && rec.Code != r.Status {
			t.Errorf("idscan: %s: status %d, want %d", r, rec.Code, r.Status)
		}
		for _, leak := range Find(rec.Header(), rec.Body.Bytes(), ids) {
			t.Errorf("idscan: %s (status %d): internal ID %s", r, rec.Code, leak)
		}
	}
	return n
}

// Find returns a description of every occurrence of one of ids in a response's header and body.
func Find(header http.Header, body []byte, ids []string) []string {
	var found []string
	search := func(where string, data []byte) {
		texts := map[string][]byte{"": data}
		for i, jwt := range jwtPattern.FindAll(data, -1) {
			for j, part := range bytes.Split(jwt, []byte(".")) {
				if j > 1 {
					break // the signature
				}
				if decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(string(part), "=")); err == nil {
					texts[fmt.Sprintf(" (JSON Web Token %d, part %d)", i+1, j+1)] = decoded
				}
			}
		}
		// Binary values in JSON (WebAuthn options carry the user handle and credential IDs, the sealed ceremony state)
		// are base64url or base64: an ID encoded in one would not show as such.
		for i, token := range base64Pattern.FindAll(data, -1) {
			trimmed := strings.TrimRight(string(token), "=")
			for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.RawStdEncoding} {
				if decoded, err := enc.DecodeString(trimmed); err == nil {
					texts[fmt.Sprintf(" (base64 value %d)", i+1)] = decoded
					break
				}
			}
		}
		for _, id := range ids {
			for label, text := range texts {
				if id != "" && bytes.Contains(text, []byte(id)) {
					found = append(found, fmt.Sprintf("%q in %s%s", id, where, label))
				}
			}
		}
	}

	for name, values := range header {
		search("the header name "+name, []byte(name))
		for _, v := range values {
			search("the header "+name, []byte(v))
		}
	}
	search("the body", body)
	if strings.EqualFold(header.Get("Content-Encoding"), "gzip") {
		if zr, err := gzip.NewReader(bytes.NewReader(body)); err == nil {
			if decoded, err := io.ReadAll(io.LimitReader(zr, 64<<20)); err == nil {
				search("the gzip-decoded body", decoded)
			}
		}
	}
	return found
}

// base64Pattern matches a run of base64 or base64url characters long enough to hold an encoded ID (15 characters
// encode to 20).
var base64Pattern = regexp.MustCompile(`[A-Za-z0-9_+/-]{20,}={0,2}`)

// jwtPattern matches a JSON Web Token in compact form: base64url header (always starting with "eyJ", that is `{"`),
// payload and signature, separated by dots. PocketBase's auth tokens are such tokens and carry the record ID.
var jwtPattern = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+=*\.[A-Za-z0-9_-]+=*\.[A-Za-z0-9_-]*`)
