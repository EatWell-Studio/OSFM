package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/config"
	"github.com/EatWell-Studio/OSFM/internal/pbtest"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/testapp"
)

// Unit tests of the export writers. The contents of the layers, as the four identities see them through the routes,
// are tested in cmd/osfm (layer_export_test.go).

func TestMain(m *testing.M) {
	code := m.Run()
	pbtest.RemoveTemplate()
	os.Exit(code)
}

func start(t *testing.T, unconfigured bool) *testapp.App {
	t.Helper()
	return testapp.Start(t, testapp.Options{Unconfigured: unconfigured, Bind: func(app core.App, _ *config.Config) {
		store.BindHooks(app)
	}})
}

// TestBatches: a layer longer than a batch is written whole, each record once, in uuid order, as valid JSON.
func TestBatches(t *testing.T) {
	a := start(t, false)
	old := batchSize
	batchSize = 2
	t.Cleanup(func() { batchSize = old })
	want := []string{a.Place.GetString(store.FieldUUID), a.PseudonymPlace.GetString(store.FieldUUID)}
	for range 5 {
		p := a.SeedPlace(testapp.PlaceSeed{})
		a.SeedRecommendation(testapp.RecommendationSeed{Place: p, Author: a.Member})
		want = append(want, p.GetString(store.FieldUUID))
	}
	slices.Sort(want)

	var buf bytes.Buffer
	if err := WritePlaces(&buf, a, Options{}); err != nil {
		t.Fatal(err)
	}
	var places PlacesCollection
	if err := json.Unmarshal(buf.Bytes(), &places); err != nil {
		t.Fatalf("%v: %s", err, buf.String())
	}
	got := []string{}
	for _, f := range places.Features {
		got = append(got, f.ID)
	}
	if !slices.Equal(got, want) {
		t.Errorf("places %v, want %v", got, want)
	}

	buf.Reset()
	if err := WriteRecommendations(&buf, a, Options{}); err != nil {
		t.Fatal(err)
	}
	var recs RecommendationsCollection
	if err := json.Unmarshal(buf.Bytes(), &recs); err != nil {
		t.Fatalf("%v: %s", err, buf.String())
	}
	if len(recs.Features) != 7 { // the author's two shared ones and the five new ones
		t.Errorf("%d recommendations", len(recs.Features))
	}
	if !slices.IsSortedFunc(recs.Features, func(x, y Feature[RecommendationProperties]) int {
		return strings.Compare(x.ID, y.ID)
	}) {
		t.Error("recommendations not in uuid order")
	}
}

// TestEmptyLayer: an instance without recommendations exports an empty list of features (not null), with the header.
func TestEmptyLayer(t *testing.T) {
	a := start(t, true)
	now := time.Date(2026, 1, 2, 3, 4, 5, 600, time.FixedZone("test", 3600))
	for _, write := range []func(*bytes.Buffer) error{
		func(b *bytes.Buffer) error { return WritePlaces(b, a, Options{Now: now, City: "test-city"}) },
		func(b *bytes.Buffer) error { return WriteRecommendations(b, a, Options{Now: now}) },
	} {
		var buf bytes.Buffer
		if err := write(&buf); err != nil {
			t.Fatal(err)
		}
		var v struct {
			Collection
			Features json.RawMessage `json:"features"`
		}
		if err := json.Unmarshal(buf.Bytes(), &v); err != nil {
			t.Fatalf("%v: %s", err, buf.String())
		}
		if string(bytes.TrimSpace(v.Features)) != "[]" || v.Type != "FeatureCollection" ||
			!v.Exported.Equal(time.Date(2026, 1, 2, 2, 4, 5, 0, time.UTC)) {
			t.Errorf("%s", buf.String())
		}
	}
}

type failingWriter struct{ after int }

func (w *failingWriter) Write(b []byte) (int, error) {
	if w.after <= 0 {
		return 0, errors.New("test: write failed")
	}
	w.after--
	return len(b), nil
}

// TestWriteError: a failing writer's error is returned.
func TestWriteError(t *testing.T) {
	a := start(t, false)
	for after := range 4 {
		if err := WritePlaces(&failingWriter{after: after}, a, Options{}); err == nil {
			t.Errorf("after %d writes: no error", after)
		}
		if err := WriteRecommendations(&failingWriter{after: after}, a, Options{}); err == nil {
			t.Errorf("after %d writes: no error", after)
		}
	}
}
