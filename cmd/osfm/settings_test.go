package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"

	"github.com/EatWell-Studio/OSFM/internal/api"
	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// TestInstanceSettings checks GET and POST /api/osfm/instance as the Admin page uses them (the settings form of
// web/src/admin/circleSettingsApi.ts and the name-and-cities form of web/src/admin/instance.ts): members read the
// name, the cities and whether search is on; maintainers read and change everything, a POST keeps the fields it does
// not carry, values are checked, and each change writes a settings.update audit entry without the contact.
func TestInstanceSettings(t *testing.T) {
	a := testapp.Start(t, testapp.Options{Bind: bind})

	// The JSON as the web app reads it.
	fields := func(r *testapp.Response) map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if r.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s: Cache-Control %q", r.Method, r.Path, r.Header().Get("Cache-Control"))
		}
		return m
	}
	circleFields := []string{"reportThreshold", "inviteCooldownDays", "geocoderContact"}

	a.As(testapp.Anonymous).Get(api.InstancePath).Expect(http.StatusUnauthorized)
	for _, id := range []testapp.Identity{testapp.Member, testapp.Author} {
		m := fields(a.As(id).Get(api.InstancePath).Expect(http.StatusOK))
		if m["name"] != testapp.InstanceName || m["geocoderEnabled"] != true {
			t.Errorf("%s: %v", id, m)
		}
		for _, f := range circleFields {
			if _, ok := m[f]; ok {
				t.Errorf("%s gets %s", id, f)
			}
		}
	}
	m := fields(a.As(testapp.Maintainer).Get(api.InstancePath).Expect(http.StatusOK))
	if m["name"] != testapp.InstanceName || m["reportThreshold"] != 3.0 || m["inviteCooldownDays"] != 30.0 ||
		m["geocoderEnabled"] != true || m["geocoderContact"] != "" {
		t.Errorf("maintainer: %v", m)
	}
	if cities, _ := m["cities"].([]any); len(cities) != 1 {
		t.Errorf("cities: %v", m["cities"])
	}

	// The settings form sends the name and cities read just before, and its four fields.
	inst := testapp.Decode[api.MaintainerInstance](a.As(testapp.Maintainer).Get(api.InstancePath))
	form := map[string]any{"name": inst.Name, "cities": inst.Cities, "reportThreshold": 2, "inviteCooldownDays": 0,
		"geocoderEnabled": false, "geocoderContact": "  test-contact@example.test  "}
	m = fields(a.As(testapp.Maintainer).Post(api.InstancePath, form).Expect(http.StatusOK))
	if m["name"] != testapp.InstanceName || m["reportThreshold"] != 2.0 || m["inviteCooldownDays"] != 0.0 ||
		m["geocoderEnabled"] != false || m["geocoderContact"] != "test-contact@example.test" {
		t.Errorf("after the settings form: %v", m)
	}
	if circle, err := settings.LoadCircle(a); err != nil || circle != (settings.Circle{ReportThreshold: 2,
		GeocoderContact: "test-contact@example.test"}) {
		t.Errorf("saved: %+v, %v", circle, err)
	}
	if m := fields(a.As(testapp.Member).Get(api.InstancePath)); m["geocoderEnabled"] != false {
		t.Errorf("members still see search on: %v", m)
	}

	// The name-and-cities form sends only those: the other settings stay.
	city := api.City{Slug: "testville", Name: "Test Ville", BBox: [4]float64{7, 0.3, 7.2, 0.4},
		Center: [2]float64{7.1, 0.35}}
	got := testapp.Decode[api.MaintainerInstance](a.As(testapp.Maintainer).Post(api.InstancePath,
		map[string]any{"name": "Test Circle Renamed", "cities": []api.City{city}}).Expect(http.StatusOK))
	if got.Name != "Test Circle Renamed" || len(got.Cities) != 1 || got.Cities[0].Slug != "testville" ||
		got.ReportThreshold != 2 || got.InviteCooldownDays != 0 || got.GeocoderEnabled ||
		got.GeocoderContact != "test-contact@example.test" {
		t.Errorf("after the name form: %+v", got)
	}
	// A single field, and null as absent.
	got = testapp.Decode[api.MaintainerInstance](a.As(testapp.Maintainer).Post(api.InstancePath,
		map[string]any{"inviteCooldownDays": 7, "name": nil}).Expect(http.StatusOK))
	if got.Name != "Test Circle Renamed" || got.InviteCooldownDays != 7 || got.ReportThreshold != 2 {
		t.Errorf("after one field: %+v", got)
	}

	// Refused values change nothing.
	for _, tc := range []struct {
		body map[string]any
		code string
	}{
		{map[string]any{"reportThreshold": 0}, api.CodeReportThresholdInvalid},
		{map[string]any{"reportThreshold": 101}, api.CodeReportThresholdInvalid},
		{map[string]any{"inviteCooldownDays": -1}, api.CodeInviteCooldownInvalid},
		{map[string]any{"inviteCooldownDays": 3651}, api.CodeInviteCooldownInvalid},
		{map[string]any{"geocoderContact": strings.Repeat("x", 201)}, api.CodeGeocoderContactInvalid},
		{map[string]any{"geocoderContact": "test\u0007bell"}, api.CodeGeocoderContactInvalid},
		{map[string]any{"name": " "}, auth.CodeInstanceNameInvalid},
		{map[string]any{"cities": []any{}}, auth.CodeCitiesInvalid},
		{map[string]any{"reportThreshold": 2.5}, api.CodeInvalidRequest},
		{map[string]any{"geocoderEnabled": "yes"}, api.CodeInvalidRequest},
	} {
		r := a.As(testapp.Maintainer).Post(api.InstancePath, tc.body).Expect(http.StatusBadRequest)
		if e := testapp.Decode[api.Error](r); e.Code != tc.code || e.Status != http.StatusBadRequest {
			t.Errorf("%v: %+v, want code %s", tc.body, e, tc.code)
		}
	}
	tooLarge := map[string]any{"geocoderContact": strings.Repeat("x", api.DefaultMaxBody)}
	if e := testapp.Decode[api.Error](a.As(testapp.Maintainer).Post(api.InstancePath, tooLarge).
		Expect(http.StatusRequestEntityTooLarge)); e.Code != api.CodeBodyTooLarge {
		t.Errorf("too large: %+v", e)
	}
	if after := testapp.Decode[api.MaintainerInstance](a.As(testapp.Maintainer).Get(api.InstancePath)); after.Name !=
		got.Name || after.ReportThreshold != 2 || after.InviteCooldownDays != 7 {
		t.Errorf("a refused change was saved: %+v", after)
	}

	// Members cannot change anything; nobody without the X-OSFM header.
	for _, id := range []testapp.Identity{testapp.Member, testapp.Author} {
		r := a.As(id).Post(api.InstancePath, map[string]any{"reportThreshold": 9}).Expect(http.StatusForbidden)
		if e := testapp.Decode[api.Error](r); e.Code != api.CodeForbidden {
			t.Errorf("%s: %+v", id, e)
		}
	}
	a.As(testapp.Anonymous).Post(api.InstancePath, map[string]any{"reportThreshold": 9}).
		Expect(http.StatusUnauthorized)
	req, _ := http.NewRequest(http.MethodPost, api.InstancePath, strings.NewReader(`{"reportThreshold":9}`))
	a.As(testapp.Maintainer).Send(req).Expect(http.StatusForbidden)

	// One audit entry per saved change, by the maintainer, with what changed but not the contact.
	audits, err := a.FindAllRecords(store.AuditLog, dbx.HashExp{"action": auth.AuditSettingsUpdate})
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) != 3 {
		t.Fatalf("%d settings.update entries, want 3", len(audits))
	}
	for _, r := range audits {
		if r.GetString("actor") != a.Maintainer.Id || strings.Contains(r.GetString("details"), "test-contact") {
			t.Errorf("audit entry: actor %s, details %s", r.GetString("actor"), r.GetString("details"))
		}
	}
	var details map[string]any
	if err := audits[0].UnmarshalJSONField("details", &details); err != nil {
		t.Fatal(err)
	}
	if details["reportThreshold"] != 2.0 || details["geocoderEnabled"] != false ||
		details["geocoderContactSet"] != true {
		t.Errorf("details: %v", details)
	}
	a.CheckNoInternalIDs()
}
