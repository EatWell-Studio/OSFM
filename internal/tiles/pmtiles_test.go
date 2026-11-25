package tiles

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

func TestTileID(t *testing.T) {
	for _, tc := range []struct {
		z    uint8
		x, y uint32
		want uint64
	}{
		// From the PMTiles specification and its reference implementations.
		{0, 0, 0, 0},
		{1, 0, 0, 1},
		{1, 0, 1, 2},
		{1, 1, 1, 3},
		{1, 1, 0, 4},
		{2, 0, 0, 5},
		{12, 3423, 1763, 19078479},
	} {
		if got := tileID(tc.z, tc.x, tc.y); got != tc.want {
			t.Errorf("tileID(%d, %d, %d) = %d, want %d", tc.z, tc.x, tc.y, got, tc.want)
		}
	}
	// On each zoom level the IDs are a bijection onto the level's range.
	var first uint64
	for z := range uint8(7) {
		n := uint32(1) << z
		seen := make(map[uint64]bool)
		for x := range n {
			for y := range n {
				id := tileID(z, x, y)
				if id < first || id >= first+uint64(n)*uint64(n) || seen[id] {
					t.Fatalf("tileID(%d, %d, %d) = %d: outside the level or repeated", z, x, y, id)
				}
				seen[id] = true
			}
		}
		first += uint64(n) * uint64(n)
	}
}

// areaTiles returns every tile from z10 to z13 in a small area, with content that repeats across runs of tiles.
func areaTiles() []testTile {
	var tiles []testTile
	for z := uint8(10); z <= 13; z++ {
		scale := uint32(1) << (z - 10)
		for x := 512 * scale; x < 514*scale; x++ {
			for y := 510 * scale; y < 512*scale; y++ {
				// Plenty of identical neighbours (along the Hilbert curve too) for run-length entries.
				content := fmt.Sprintf("z%d sea", z)
				if (x+y)%5 == 0 {
					content = fmt.Sprintf("tile %d/%d/%d", z, x, y)
				}
				tiles = append(tiles, testTile{z, x, y, []byte(content)})
			}
		}
	}
	return tiles
}

func loadTestArchive(t *testing.T, b []byte) *archive {
	t.Helper()
	a, err := loadArchive(bytes.NewReader(b), int64(len(b)), "/tiles/test")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestReadTiles(t *testing.T) {
	tiles := areaTiles()
	for _, spec := range []archiveSpec{
		{tiles: tiles, minZoom: 10, maxZoom: 13},
		{tiles: tiles, minZoom: 10, maxZoom: 13, internal: compressionNone},
		{tiles: tiles, minZoom: 10, maxZoom: 13, leafSize: 3},
		{tiles: tiles, minZoom: 10, maxZoom: 13, leafSize: 1, internal: compressionNone},
	} {
		t.Run(fmt.Sprintf("leafSize=%d,internal=%s", spec.leafSize, spec.internal), func(t *testing.T) {
			b := buildArchive(t, spec)
			a := loadTestArchive(t, b)
			if spec.leafSize > 0 {
				for _, e := range a.root {
					if e.runLength != 0 {
						t.Fatalf("root entry %+v is not a leaf pointer", e)
					}
				}
			}
			runs := false
			cache := newDirCache(directoryCacheBytes)
			for _, tile := range tiles {
				got, err := a.readTile(bytes.NewReader(b), tile.z, tile.x, tile.y, cache)
				if err != nil {
					t.Fatalf("%d/%d/%d: %v", tile.z, tile.x, tile.y, err)
				}
				if !bytes.Equal(gunzip(t, got), tile.content) {
					t.Fatalf("%d/%d/%d: content %q, want %q", tile.z, tile.x, tile.y, gunzip(t, got), tile.content)
				}
			}
			for _, e := range a.root {
				runs = runs || e.runLength > 1
			}
			for _, el := range cache.items {
				for _, e := range el.Value.(*dirItem).entries {
					runs = runs || e.runLength > 1
				}
			}
			if !runs {
				t.Error("the archive has no run-length entries; the test does not cover them")
			}
			// Tiles in the grid but not in the archive.
			for _, tile := range []testTile{{10, 0, 0, nil}, {11, 1023, 1023, nil}, {13, 4200, 4100, nil}} {
				if _, err := a.readTile(bytes.NewReader(b), tile.z, tile.x, tile.y, cache); !errors.Is(err, errNoTile) {
					t.Errorf("%d/%d/%d: error %v, want errNoTile", tile.z, tile.x, tile.y, err)
				}
			}
		})
	}
}

func TestUnsupportedCompression(t *testing.T) {
	b := buildArchive(t, archiveSpec{tiles: testTiles, minZoom: testMinZoom, maxZoom: testMaxZoom})
	for _, c := range []compression{compressionUnknown, compressionBrotli, compressionZstd, 9} {
		bad := bytes.Clone(b)
		bad[97] = byte(c)
		_, err := loadArchive(bytes.NewReader(bad), int64(len(bad)), "")
		if err == nil || !strings.Contains(err.Error(), "unsupported internal compression "+c.String()) {
			t.Errorf("internal compression %s: error %v", c, err)
		}
	}
}

func TestBadHeaders(t *testing.T) {
	b := buildArchive(t, archiveSpec{tiles: testTiles, minZoom: testMinZoom, maxZoom: testMaxZoom})
	for name, mutate := range map[string]func([]byte){
		"magic":            func(b []byte) { b[0] = 'X' },
		"version 2":        func(b []byte) { b[7] = 2 },
		"tile type PNG":    func(b []byte) { b[99] = 2 },
		"min above max":    func(b []byte) { b[100] = 12 },
		"max zoom 31":      func(b []byte) { b[101] = 31 },
		"root beyond 16K":  func(b []byte) { b[8+1] = 0x40 },
		"root in header":   func(b []byte) { b[8] = 10 },
		"tile data length": func(b []byte) { b[64+7] = 0x7f },
		"leaf offset":      func(b []byte) { b[40+6] = 1 },
		"metadata length":  func(b []byte) { b[32+3] = 1 },
	} {
		bad := bytes.Clone(b)
		mutate(bad)
		if _, err := loadArchive(bytes.NewReader(bad), int64(len(bad)), ""); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// Every prefix of a valid archive, and every archive with one corrupted byte, is either refused or read without a
// panic; the reads may fail but must end.
func TestCorruptArchives(t *testing.T) {
	tiles := areaTiles()
	for _, leafSize := range []int{0, 2} {
		b := buildArchive(t, archiveSpec{tiles: tiles, minZoom: 10, maxZoom: 13, leafSize: leafSize})
		check := func(bad []byte) {
			t.Helper()
			a, err := loadArchive(bytes.NewReader(bad), int64(len(bad)), "")
			if err != nil {
				return
			}
			cache := newDirCache(directoryCacheBytes)
			for _, tile := range tiles {
				_, _ = a.readTile(bytes.NewReader(bad), tile.z, tile.x, tile.y, cache)
			}
		}
		for n := range len(b) {
			if _, err := loadArchive(bytes.NewReader(b[:n]), int64(n), ""); err == nil && n < headerLen {
				t.Fatalf("a %d-byte prefix loaded", n)
			}
			check(b[:n])
		}
		r := rand.New(rand.NewPCG(14, uint64(leafSize))) //nolint:gosec // G404: a reproducible sequence is the point
		for range 3000 {
			bad := bytes.Clone(b)
			bad[r.IntN(len(bad))] ^= byte(1 + r.IntN(255)) //nolint:gosec // G115: 1 to 255
			check(bad)
		}
	}
}

func TestTruncatedTileData(t *testing.T) {
	b := buildArchive(t, archiveSpec{tiles: testTiles, minZoom: testMinZoom, maxZoom: testMaxZoom})
	// The header still claims the full tile data section, but the reader sees a shorter file.
	short := b[:len(b)-5]
	if _, err := loadArchive(bytes.NewReader(short), int64(len(short)), ""); err == nil {
		t.Fatal("truncated archive loaded")
	}
}

func TestLeafLoop(t *testing.T) {
	// A root that points to a leaf that points to itself: the lookup must stop.
	leaf := serializeDirectory([]entry{{tileID: 0, offset: 0, length: 0, runLength: 0}})
	leaf = serializeDirectory([]entry{{tileID: 0, offset: 0, length: uint32(len(leaf)), runLength: 0}}) //nolint:gosec // G115: a few bytes
	b := buildArchive(t, archiveSpec{tiles: testTiles, minZoom: testMinZoom, maxZoom: testMaxZoom, internal: compressionNone})
	h, err := parseHeader(b, uint64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	a := &archive{header: h, root: []entry{{tileID: 0, offset: 0, length: uint32(len(leaf))}}} //nolint:gosec // G115: a few bytes
	a.header.leafOffset, a.header.leafLength = 0, uint64(len(leaf))
	_, err = a.readTile(bytes.NewReader(leaf), 10, 512, 511, newDirCache(directoryCacheBytes))
	if err == nil || !strings.Contains(err.Error(), "deeper") {
		t.Errorf("error %v, want one about nesting", err)
	}
}

func TestDecompressionBomb(t *testing.T) {
	bomb := gzipBytes(t, make([]byte, maxDirectoryBytes+1))
	if _, err := decompress(bomb, compressionGzip, maxDirectoryBytes); err == nil {
		t.Error("no error for a directory beyond the limit")
	}
	if _, err := parseDirectory([]byte{0xff, 0xff, 0xff, 0xff, 0x0f}); err == nil {
		t.Error("no error for an entry count beyond the input")
	}
}

func TestDirCacheBudget(t *testing.T) {
	c := newDirCache(10 * entryBytes)
	a := &archive{}
	for i := range 20 {
		c.put(dirKey{archive: a, offset: uint64(i)}, make([]entry, 3))
		if c.used > c.budget {
			t.Fatalf("used %d of %d", c.used, c.budget)
		}
	}
	if _, ok := c.get(dirKey{archive: a, offset: 19}); !ok {
		t.Error("the most recent directory was evicted")
	}
	if _, ok := c.get(dirKey{archive: a, offset: 0}); ok {
		t.Error("the oldest directory was kept")
	}
	c.put(dirKey{archive: a, offset: 99}, make([]entry, 11)) // over the whole budget: not cached
	if _, ok := c.get(dirKey{archive: a, offset: 99}); ok || c.used > c.budget {
		t.Error("a directory larger than the budget was cached")
	}
}
