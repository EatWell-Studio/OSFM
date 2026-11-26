package idscan_test

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/EatWell-Studio/OSFM/internal/idscan"
	"github.com/EatWell-Studio/OSFM/internal/store"
	_ "github.com/EatWell-Studio/OSFM/migrations" // the collections
)

const id = "abc123def456ghi" // the shape of a PocketBase record ID

func TestFind(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"id":"` + id + `","type":"auth"}`))
	jwt := "eyJhbGciOiJIUzI1NiJ9." + payload + ".c2lnbmF0dXJl"
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(`{"place":"` + id + `"}`))
	_ = zw.Close()

	for _, tc := range []struct {
		name   string
		header http.Header
		body   []byte
		found  bool
	}{
		{"clean", http.Header{"Content-Type": {"application/json"}}, []byte(`{"uuid":"0190a3c4-5b6d-7e8f-9a0b-1c2d3e4f5a6b"}`), false},
		{"body", nil, []byte(`{"id":"` + id + `"}`), true},
		{"header value", http.Header{"Location": {"/places/" + id}}, nil, true},
		{"header name", http.Header{"X-" + id: {"1"}}, nil, true},
		{"token in a cookie", http.Header{"Set-Cookie": {"osfm_session=" + jwt + "; HttpOnly"}}, nil, true},
		{"token in the body", nil, []byte(`{"token":"` + jwt + `"}`), true},
		{"gzip", http.Header{"Content-Encoding": {"gzip"}}, gz.Bytes(), true},
		{"base64url value", nil, []byte(`{"user":{"id":"` + base64.RawURLEncoding.EncodeToString([]byte("x"+id)) + `"}}`), true},
		{"base64 value", nil, []byte(`{"key":"` + base64.StdEncoding.EncodeToString([]byte("\xff\xfe"+id+"!")) + `"}`), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := idscan.Find(tc.header, tc.body, []string{id})
			if (len(got) > 0) != tc.found {
				t.Errorf("Find = %q, want found %v", got, tc.found)
			}
		})
	}
}

// TestScanReportsLeaks checks the scan against a handler that leaks an ID, with a test double for testing.TB.
func TestScanReportsLeaks(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/leak" {
			_, _ = w.Write([]byte(id))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	rec := &recorder{TB: t}
	n := idscan.Scan(rec, h, []idscan.Request{
		{Path: "/clean", Status: http.StatusNoContent},
		{Path: "/leak"},
		{Path: "/clean", Status: http.StatusOK},
	}, []string{id})
	if n != 3 {
		t.Errorf("checked %d responses, want 3", n)
	}
	if len(rec.errors) != 2 || !strings.Contains(rec.errors[0], "GET /leak") ||
		!strings.Contains(rec.errors[1], "status 204, want 200") {
		t.Errorf("reported %q", rec.errors)
	}
}

type recorder struct {
	testing.TB
	errors []string
}

func (r *recorder) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *recorder) Helper() {}

func TestExpand(t *testing.T) {
	got := idscan.Expand("/api/collections/{collection}/records/{id}", map[string][]string{"collection": {"a", "b"}},
		"x")
	if want := []string{"/api/collections/a/records/x", "/api/collections/b/records/x"}; !slices.Equal(got, want) {
		t.Errorf("Expand = %q, want %q", got, want)
	}
	if got := idscan.Expand("/tiles/{path...}", nil, "x"); !slices.Equal(got, []string{"/tiles/x"}) {
		t.Errorf("Expand = %q", got)
	}
	if got := idscan.Expand("/api/health", nil, "x"); !slices.Equal(got, []string{"/api/health"}) {
		t.Errorf("Expand = %q", got)
	}
}

func TestRoutes(t *testing.T) {
	app, err := tests.NewTestApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	r.GET("/test/{path...}", func(*core.RequestEvent) error { return nil })
	r.Group("/outer").Group("/inner").POST("/x", func(*core.RequestEvent) error { return nil })

	var got []string
	for _, route := range idscan.Routes(t, r) {
		got = append(got, route.String())
	}
	for _, want := range []string{"GET /api/health", "GET /api/collections/{collection}/records", "GET /test/{path...}",
		"POST /outer/inner/x"} {
		if !slices.Contains(got, want) {
			t.Errorf("route %q not listed in %q", want, got)
		}
	}
}

func TestSeedCoversEveryCollection(t *testing.T) {
	app, err := tests.NewTestApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	store.BindHooks(app)
	s := idscan.Seed(t, app)
	for _, name := range []string{store.Members, store.Places, store.Recommendations} {
		var live, deleted int
		for _, r := range s.Records[name] {
			if store.IsDeleted(r) {
				deleted++
			} else {
				live++
			}
		}
		if live == 0 || deleted == 0 {
			t.Errorf("%s: %d live and %d deleted records, want some of both", name, live, deleted)
		}
	}
	if !slices.Contains(s.IDs, s.Superuser.Id) || !slices.Contains(s.IDs, store.SettingsID) {
		t.Error("the superuser or the settings row is missing from the IDs")
	}
}
