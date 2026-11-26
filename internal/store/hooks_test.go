package store_test

import (
	"errors"
	"regexp"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/EatWell-Studio/OSFM/internal/store"
	_ "github.com/EatWell-Studio/OSFM/migrations" // the collections
)

var uuidV7 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func newApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	store.BindHooks(app)
	return app
}

func newPlace(t *testing.T, app core.App, lat, lon float64) *core.Record {
	t.Helper()
	c, err := app.FindCollectionByNameOrId(store.Places)
	if err != nil {
		t.Fatal(err)
	}
	r := core.NewRecord(c)
	r.Set("name", "Test Noodles")
	r.Set("lat", lat)
	r.Set("lon", lon)
	r.Set("precision", "exact")
	r.Set("category", "restaurant")
	r.Set("status", "open")
	return r
}

func TestNewUUID(t *testing.T) {
	a, err := store.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	if !uuidV7.MatchString(a) || !uuidV7.MatchString(b) || a == b {
		t.Errorf("NewUUID: %q, %q", a, b)
	}
	if a > b {
		t.Errorf("UUIDv7 not increasing: %q then %q", a, b)
	}
}

func TestUUIDIsGeneratedAndKept(t *testing.T) {
	app := newApp(t)
	place := newPlace(t, app, 31.2, 121.5)
	if err := app.Save(place); err != nil {
		t.Fatal(err)
	}
	id := place.GetString(store.FieldUUID)
	if !uuidV7.MatchString(id) {
		t.Fatalf("generated uuid %q", id)
	}

	// An update keeps it; changing it is rejected.
	place.Set("name", "Test Noodles 2")
	if err := app.Save(place); err != nil {
		t.Fatal(err)
	}
	other, err := store.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	place.Set(store.FieldUUID, other)
	if err := app.Save(place); !errors.Is(err, store.ErrUUIDChanged) {
		t.Errorf("changing the uuid: %v, want ErrUUIDChanged", err)
	}
	stored, err := app.FindRecordById(store.Places, place.Id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.GetString(store.FieldUUID) != id {
		t.Errorf("stored uuid %q, want %q", stored.GetString(store.FieldUUID), id)
	}
}

func TestUUIDMustBeV7(t *testing.T) {
	app := newApp(t)
	place := newPlace(t, app, 1, 1)
	place.Set(store.FieldUUID, "123e4567-e89b-42d3-a456-426614174000") // a version 4 UUID
	if err := app.Save(place); err == nil {
		t.Error("a place with a UUIDv4 was saved")
	}
}

func TestCoordinatesAreRounded(t *testing.T) {
	app := newApp(t)
	place := newPlace(t, app, 31.2304567891, -121.4737012345)
	if err := app.Save(place); err != nil {
		t.Fatal(err)
	}
	stored, err := app.FindRecordById(store.Places, place.Id)
	if err != nil {
		t.Fatal(err)
	}
	if lat, lon := stored.GetFloat("lat"), stored.GetFloat("lon"); lat != 31.23046 || lon != -121.4737 {
		t.Errorf("stored %v, %v; want 31.23046, -121.4737", lat, lon)
	}

	stored.Set("lat", 0.000004)
	stored.Set("lon", 179.999996)
	if err := app.Save(stored); err != nil {
		t.Fatal(err)
	}
	updated, err := app.FindRecordById(store.Places, place.Id)
	if err != nil {
		t.Fatal(err)
	}
	if lat, lon := updated.GetFloat("lat"), updated.GetFloat("lon"); lat != 0 || lon != 180 {
		t.Errorf("after update %v, %v; want 0, 180", lat, lon)
	}
}

func TestRoundCoord(t *testing.T) {
	for in, want := range map[float64]float64{
		52.520008: 52.52001, -0.000005: -0.00001, 13.404954: 13.40495, 90: 90, 1.234566: 1.23457,
	} {
		if got := store.RoundCoord(in); got != want {
			t.Errorf("RoundCoord(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestAppendOnly(t *testing.T) {
	app := newApp(t)
	c, err := app.FindCollectionByNameOrId(store.AuditLog)
	if err != nil {
		t.Fatal(err)
	}
	entry := core.NewRecord(c)
	entry.Set("action", "test.action")
	if err := app.Save(entry); err != nil {
		t.Fatal(err)
	}
	entry.Set("action", "test.changed")
	if err := app.Save(entry); !errors.Is(err, store.ErrAppendOnly) {
		t.Errorf("updating an audit log entry: %v, want ErrAppendOnly", err)
	}
}
