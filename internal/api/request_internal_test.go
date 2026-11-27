package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

func newEvent(method, body string) (*core.RequestEvent, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/api/osfm/test", strings.NewReader(body))
	return &core.RequestEvent{Event: router.Event{Response: rec, Request: req}}, rec
}

// TestErrorShorthands: each answers an Error with its status and code, not cached.
func TestErrorShorthands(t *testing.T) {
	for _, tc := range []struct {
		answer func(*core.RequestEvent) error
		status int
		code   string
	}{
		{badRequest, http.StatusBadRequest, CodeInvalidRequest},
		{notFound, http.StatusNotFound, CodeNotFound},
		{forbidden, http.StatusForbidden, CodeForbidden},
	} {
		e, rec := newEvent(http.MethodGet, "")
		if err := tc.answer(e); err != nil {
			t.Fatal(err)
		}
		var body Error
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if rec.Code != tc.status || body.Status != tc.status || body.Code != tc.code || body.Message == "" ||
			body.Data == nil || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%d %+v %v", rec.Code, body, rec.Header())
		}
	}
}

// TestReadJSON: the size limit, empty and malformed bodies.
func TestReadJSON(t *testing.T) {
	var v struct {
		Name string `json:"name"`
	}
	for body, want := range map[string]error{
		`{"name":"Test"}`: nil,
		``:                errEmptyBody,
		`{`:               errInvalidJSON,
		`{"name":"` + strings.Repeat("x", 64) + `"}`: errBodyTooLarge,
	} {
		e, _ := newEvent(http.MethodPost, body)
		if err := readJSON(e, &v, 32); !errors.Is(err, want) {
			t.Errorf("%q: %v, want %v", body, err, want)
		}
	}
	e, rec := newEvent(http.MethodPost, "")
	if err := requestError(e, errBodyTooLarge); err != nil || rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: %d", rec.Code)
	}
}

// TestPathUUID: only a UUIDv7 in canonical form names an object.
func TestPathUUID(t *testing.T) {
	for id, want := range map[string]bool{
		"0190a1b2-0000-7000-8000-000000000001": true,
		"0190A1B2-0000-7000-8000-000000000001": false,
		"0190a1b2-0000-4000-8000-000000000001": false, // version 4
		"abc123def456ghi":                      false, // a record ID
		"":                                     false,
	} {
		e, _ := newEvent(http.MethodGet, "")
		e.Request.SetPathValue("uuid", id)
		if got, ok := pathUUID(e, "uuid"); ok != want || got != id {
			t.Errorf("%q: %q %v", id, got, ok)
		}
	}
}

// TestRespondKeepsCachePolicy: respond does not cache unless the handler chose a policy.
func TestRespondKeepsCachePolicy(t *testing.T) {
	e, rec := newEvent(http.MethodGet, "")
	if err := respond(e, http.StatusOK, map[string]int{}); err != nil || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("default: %q", rec.Header().Get("Cache-Control"))
	}
	e, rec = newEvent(http.MethodGet, "")
	e.Response.Header().Set("Cache-Control", "private, no-cache")
	if err := respond(e, http.StatusOK, map[string]int{}); err != nil ||
		rec.Header().Get("Cache-Control") != "private, no-cache" {
		t.Errorf("kept: %q", rec.Header().Get("Cache-Control"))
	}
}
