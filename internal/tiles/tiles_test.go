package tiles

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

// A made-up archive: two tiles at z10 and z11 (the zoom range of the archive) around 0°, 0°.
const (
	testCity          = "test-town"
	testMinZoom uint8 = 10
	testMaxZoom uint8 = 11
)

var testTiles = []testTile{
	{10, 512, 511, []byte("tile 10/512/511")},
	{11, 1024, 1023, []byte("tile 11/1024/1023")},
}

// writeTestArchive writes testArchive to dir/name.
func writeTestArchive(t *testing.T, dir, name string) {
	t.Helper()
	writeFile(t, dir, name, testArchive(t))
}

func writeFile(t *testing.T, dir, name string, content []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// testArchive returns a minimal valid archive holding testTiles.
func testArchive(t *testing.T) []byte {
	t.Helper()
	return buildArchive(t, archiveSpec{tiles: testTiles, minZoom: testMinZoom, maxZoom: testMaxZoom})
}

func newTestHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	writeTestArchive(t, dir, testCity+".pmtiles")
	h, err := NewHandler(dir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return h, dir
}

func get(h http.Handler, target string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTile(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, tile := range testTiles {
		target := "/tiles/" + testCity + "/" + itoa(uint32(tile.z)) + "/" + itoa(tile.x) + "/" + itoa(tile.y) + ".mvt"
		rec := get(h, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d, body %q", target, rec.Code, rec.Body.String())
		}
		for name, want := range map[string]string{
			"Content-Type":     "application/x-protobuf",
			"Content-Encoding": "gzip",
			"Cache-Control":    tileCacheControl,
		} {
			if got := rec.Header().Get(name); got != want {
				t.Errorf("GET %s: %s = %q, want %q", target, name, got, want)
			}
		}
		if got := gunzip(t, rec.Body.Bytes()); !bytes.Equal(got, tile.content) {
			t.Errorf("GET %s: content %q, want %q", target, got, tile.content)
		}

		etag := rec.Header().Get("ETag")
		if etag == "" {
			t.Fatalf("GET %s: no ETag", target)
		}
		if rec := get(h, target, "If-None-Match", etag); rec.Code != http.StatusNotModified {
			t.Errorf("GET %s with If-None-Match: status %d, want 304", target, rec.Code)
		}
	}
}

func TestTileJSON(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := get(h, "/tiles/"+testCity+".json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != tileJSONCacheControl {
		t.Errorf("Cache-Control = %q", got)
	}
	var tj struct {
		TileJSON    string    `json:"tilejson"`
		Tiles       []string  `json:"tiles"`
		MinZoom     int       `json:"minzoom"`
		MaxZoom     int       `json:"maxzoom"`
		Bounds      []float64 `json:"bounds"`
		Attribution string    `json:"attribution"`
		Layers      []any     `json:"vector_layers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tj); err != nil {
		t.Fatal(err)
	}
	if len(tj.Tiles) != 1 || tj.Tiles[0] != "/tiles/"+testCity+"/{z}/{x}/{y}.mvt" {
		t.Errorf("tiles = %q", tj.Tiles)
	}
	if tj.TileJSON == "" || tj.MinZoom != int(testMinZoom) || tj.MaxZoom != int(testMaxZoom) || len(tj.Bounds) != 4 ||
		tj.Attribution == "" || len(tj.Layers) != 1 {
		t.Errorf("TileJSON = %+v", tj)
	}
}

func TestTileJSONPublicURL(t *testing.T) {
	dir := t.TempDir()
	writeTestArchive(t, dir, testCity+".pmtiles")
	h, err := NewHandler(dir, "https://food.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := get(h, "/tiles/"+testCity+".json")
	var tj struct {
		Tiles []string `json:"tiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tj); err != nil {
		t.Fatal(err)
	}
	if len(tj.Tiles) != 1 || tj.Tiles[0] != "https://food.example/tiles/"+testCity+"/{z}/{x}/{y}.mvt" {
		t.Errorf("tiles = %q", tj.Tiles)
	}
}

func TestNotFound(t *testing.T) {
	h, dir := newTestHandler(t)
	// Valid archives under invalid names: they must be refused for their name, not for being absent.
	for _, name := range []string{"A.pmtiles", "x.y.pmtiles", "x_y.pmtiles"} {
		writeTestArchive(t, dir, name)
	}
	// Archives that are not usable.
	writeFile(t, dir, "short.pmtiles", []byte("PMTiles"))
	writeFile(t, dir, "garbage.pmtiles", bytes.Repeat([]byte{0xff}, 4096))
	valid := testArchive(t)
	writeFile(t, dir, "truncated.pmtiles", valid[:len(valid)-10])

	for _, target := range []string{
		// unknown city
		"/tiles/elsewhere/10/512/511.mvt",
		"/tiles/elsewhere.json",
		// invalid city names
		"/tiles/../x/10/512/511.mvt",
		"/tiles/..%2Fx/10/512/511.mvt",
		"/tiles/A/10/512/511.mvt",
		"/tiles/A.json",
		"/tiles/x.y/10/512/511.mvt",
		"/tiles/x.y.json",
		"/tiles/x_y.json",
		"/tiles/.json",
		"/tiles/" + testCity + "/.json",
		// unusable archives
		"/tiles/short.json",
		"/tiles/garbage/10/512/511.mvt",
		"/tiles/truncated/10/512/511.mvt",
		// outside the archive's zoom levels
		"/tiles/" + testCity + "/9/256/255.mvt",
		"/tiles/" + testCity + "/12/2048/2047.mvt",
		"/tiles/" + testCity + "/99/0/0.mvt",
		"/tiles/" + testCity + "/300/0/0.mvt",
		// outside the tile grid, or not in the archive
		"/tiles/" + testCity + "/10/1024/511.mvt",
		"/tiles/" + testCity + "/10/512/1024.mvt",
		"/tiles/" + testCity + "/10/1536/511.mvt", // 512 + 1024: would alias 10/512/511 without the grid check
		"/tiles/" + testCity + "/10/0/0.mvt",
		// malformed tile paths
		"/tiles/" + testCity + "/10/512/0511.mvt",
		"/tiles/" + testCity + "/10/+512/511.mvt",
		"/tiles/" + testCity + "/10/-1/511.mvt",
		"/tiles/" + testCity + "/10/512/511.png",
		"/tiles/" + testCity + "/10/512/511",
		"/tiles/" + testCity + "/10/512.mvt",
		"/tiles/" + testCity + "/metadata",
		"/tiles/" + testCity + "/10/512/511.mvt/x",
		"/tiles//",
		"/tiles/index",
		"/other/" + testCity + ".json",
	} {
		if rec := get(h, target); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", target, rec.Code)
		}
	}
}

func TestIndex(t *testing.T) {
	h, dir := newTestHandler(t)
	writeTestArchive(t, dir, "another-town.pmtiles")
	// Left out: archives under invalid names (and so not served), unusable archives and other files.
	writeTestArchive(t, dir, "A.pmtiles")
	writeTestArchive(t, dir, ".hidden.pmtiles")
	writeFile(t, dir, "garbage.pmtiles", bytes.Repeat([]byte{0xff}, 4096))
	writeFile(t, dir, "notes.txt", []byte("not an archive"))

	rec := get(h, "/tiles/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
	}
	for name, want := range map[string]string{"Content-Type": "application/json", "Cache-Control": tileJSONCacheControl} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	var index struct {
		Cities []struct {
			ID      string    `json:"id"`
			Bounds  []float64 `json:"bounds"`
			Center  []float64 `json:"center"`
			MinZoom int       `json:"minzoom"`
			MaxZoom int       `json:"maxzoom"`
		} `json:"cities"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Cities) != 2 || index.Cities[0].ID != "another-town" || index.Cities[1].ID != testCity {
		t.Fatalf("cities = %+v, want another-town and %s", index.Cities, testCity)
	}

	// Each entry agrees with the city's TileJSON.
	var tj struct {
		Bounds  []float64 `json:"bounds"`
		Center  []float64 `json:"center"`
		MinZoom int       `json:"minzoom"`
		MaxZoom int       `json:"maxzoom"`
	}
	if err := json.Unmarshal(get(h, "/tiles/"+testCity+".json").Body.Bytes(), &tj); err != nil {
		t.Fatal(err)
	}
	c := index.Cities[1]
	if !slices.Equal(c.Bounds, tj.Bounds) || !slices.Equal(c.Center, tj.Center) || c.MinZoom != tj.MinZoom ||
		c.MaxZoom != tj.MaxZoom || c.MinZoom != int(testMinZoom) || c.MaxZoom != int(testMaxZoom) {
		t.Errorf("index entry %+v does not match the TileJSON %+v", c, tj)
	}

	// A city added or removed while the server runs shows in the next index.
	if err := os.Remove(filepath.Join(dir, "another-town.pmtiles")); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(get(h, "/tiles/").Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Cities) != 1 || index.Cities[0].ID != testCity {
		t.Errorf("after removing an archive: cities = %+v", index.Cities)
	}
}

func TestIndexEmpty(t *testing.T) {
	for name, dir := range map[string]string{"empty": t.TempDir(), "missing": filepath.Join(t.TempDir(), "absent")} {
		h, err := NewHandler(dir, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		rec := get(h, "/tiles/")
		if rec.Code != http.StatusOK || rec.Body.String() != `{"cities":[]}` {
			t.Errorf("%s directory: status %d, body %q", name, rec.Code, rec.Body.String())
		}
	}
}

func TestArchiveReplaced(t *testing.T) {
	h, dir := newTestHandler(t)
	target := "/tiles/" + testCity + ".json"
	if rec := get(h, target); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	writeFile(t, dir, testCity+".pmtiles", []byte("not an archive any more"))
	if rec := get(h, target); rec.Code != http.StatusNotFound {
		t.Errorf("after replacing the archive with garbage: status %d, want 404", rec.Code)
	}
	if err := os.Remove(filepath.Join(dir, testCity+".pmtiles")); err != nil {
		t.Fatal(err)
	}
	if rec := get(h, target); rec.Code != http.StatusNotFound {
		t.Errorf("after removing the archive: status %d, want 404", rec.Code)
	}
}

func TestArchiveAdded(t *testing.T) {
	h, dir := newTestHandler(t)
	target := "/tiles/new-town.json"
	if rec := get(h, target); rec.Code != http.StatusNotFound {
		t.Fatalf("before adding the archive: status %d, want 404", rec.Code)
	}
	writeTestArchive(t, dir, "new-town.pmtiles")
	if rec := get(h, target); rec.Code != http.StatusOK {
		t.Errorf("after adding the archive: status %d, want 200", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h, _ := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/tiles/"+testCity+".json", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status %d, want 405", rec.Code)
	}
}

func itoa(n uint32) string {
	return strconv.FormatUint(uint64(n), 10)
}
