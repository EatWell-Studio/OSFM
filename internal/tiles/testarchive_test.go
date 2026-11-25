package tiles

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"slices"
	"testing"
)

// Test archives are built here, in code: no .pmtiles file is committed.

type testTile struct {
	z       uint8
	x, y    uint32
	content []byte // before gzip, as a client sees it after decoding
}

// archiveSpec describes a test archive.
type archiveSpec struct {
	tiles            []testTile
	minZoom, maxZoom uint8
	// leafSize > 0 puts the entries into leaf directories of that many entries, with only pointers in the root.
	leafSize int
	// internal is the directory and metadata compression; zero means gzip.
	internal compression
	metadata map[string]any
}

type testEntry struct {
	entry
	content []byte
}

// buildArchive returns a PMTiles v3 archive. Tiles with the same content are stored once, and tiles with the same
// content at consecutive tile IDs share one run-length entry.
func buildArchive(t *testing.T, spec archiveSpec) []byte {
	t.Helper()
	internal := spec.internal
	if internal == compressionUnknown {
		internal = compressionGzip
	}

	tiles := slices.Clone(spec.tiles)
	slices.SortFunc(tiles, func(a, b testTile) int {
		ia, ib := tileID(a.z, a.x, a.y), tileID(b.z, b.x, b.y)
		switch {
		case ia < ib:
			return -1
		case ia > ib:
			return 1
		}
		return 0
	})
	var tileData []byte
	offsets := map[string]uint64{}
	var entries []entry
	var last *testEntry
	for _, tile := range tiles {
		id := tileID(tile.z, tile.x, tile.y)
		if last != nil && last.tileID+uint64(last.runLength) == id && bytes.Equal(last.content, tile.content) {
			last.runLength++
			entries[len(entries)-1] = last.entry
			continue
		}
		offset, ok := offsets[string(tile.content)]
		compressed := gzipBytes(t, tile.content)
		if !ok {
			offset = uint64(len(tileData))
			offsets[string(tile.content)] = offset
			tileData = append(tileData, compressed...)
		}
		last = &testEntry{entry{tileID: id, offset: offset, length: uint32(len(compressed)), runLength: 1}, tile.content} //nolint:gosec // G115: a few bytes
		entries = append(entries, last.entry)
	}

	var root, leaves []byte
	if spec.leafSize == 0 {
		root = compress(t, serializeDirectory(entries), internal)
	} else {
		var pointers []entry
		for chunk := range slices.Chunk(entries, spec.leafSize) {
			leaf := compress(t, serializeDirectory(chunk), internal)
			pointers = append(pointers, entry{tileID: chunk[0].tileID, offset: uint64(len(leaves)), length: uint32(len(leaf))}) //nolint:gosec // G115: a few bytes
			leaves = append(leaves, leaf...)
		}
		root = compress(t, serializeDirectory(pointers), internal)
	}

	metadata := spec.metadata
	if metadata == nil {
		metadata = map[string]any{
			"name":          "test",
			"attribution":   "© OpenStreetMap contributors",
			"vector_layers": []any{map[string]any{"id": "roads", "fields": map[string]any{}}},
		}
	}
	rawMetadata, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	metadataBytes := compress(t, rawMetadata, internal)

	h := make([]byte, headerLen)
	copy(h, "PMTiles")
	h[7] = 3
	le := binary.LittleEndian
	offset := uint64(headerLen)
	section := func(at int, length int) {
		le.PutUint64(h[at:], offset)
		le.PutUint64(h[at+8:], uint64(length)) //nolint:gosec // G115: a length
		offset += uint64(length)               //nolint:gosec // G115: a length
	}
	section(8, len(root))
	section(24, len(metadataBytes))
	section(40, len(leaves))
	section(56, len(tileData))
	le.PutUint64(h[72:], uint64(len(tiles)))
	le.PutUint64(h[80:], uint64(len(entries)))
	le.PutUint64(h[88:], uint64(len(offsets)))
	h[96] = 1 // clustered
	h[97] = byte(internal)
	h[98] = byte(compressionGzip)
	h[99] = tileTypeMVT
	h[100], h[101] = spec.minZoom, spec.maxZoom
	putE7 := func(at int, v int32) { le.PutUint32(h[at:], uint32(v)) } //nolint:gosec // G115: two's complement
	putE7(102, -1_000_000)
	putE7(106, -1_000_000)
	putE7(110, 1_000_000)
	putE7(114, 1_000_000)
	h[118] = spec.minZoom
	putE7(119, 0)
	putE7(123, 0)

	return slices.Concat(h, root, metadataBytes, leaves, tileData)
}

// serializeDirectory encodes entries as the spec says, with the "follows the previous entry" offset where it applies.
func serializeDirectory(entries []entry) []byte {
	var b []byte
	b = binary.AppendUvarint(b, uint64(len(entries)))
	var lastID uint64
	for _, e := range entries {
		b = binary.AppendUvarint(b, e.tileID-lastID)
		lastID = e.tileID
	}
	for _, e := range entries {
		b = binary.AppendUvarint(b, uint64(e.runLength))
	}
	for _, e := range entries {
		b = binary.AppendUvarint(b, uint64(e.length))
	}
	var previous *entry
	for _, e := range entries {
		if previous != nil && e.offset == previous.offset+uint64(previous.length) {
			b = binary.AppendUvarint(b, 0)
		} else {
			b = binary.AppendUvarint(b, e.offset+1)
		}
		previous = &e
	}
	return b
}

func compress(t *testing.T, b []byte, c compression) []byte {
	t.Helper()
	switch c {
	case compressionNone:
		return b
	case compressionGzip:
		return gzipBytes(t, b)
	}
	t.Fatalf("cannot compress with %s", c)
	return nil
}

func gzipBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := out.ReadFrom(zr); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
