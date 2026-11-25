package tiles

// A reader for local PMTiles v3 archives, written from the specification:
// https://github.com/protomaps/PMTiles/blob/main/spec/v3/spec.md
//
// Every offset and length read from a file is checked against the file before it is used, and decompression is
// bounded, so a corrupt or truncated archive yields an error, never a panic, a hang or a large allocation.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
)

const (
	headerLen = 127

	// rootDirectoryBytes is how far into an archive the header and the root directory may reach.
	rootDirectoryBytes = 16384

	// maxDirectoryBytes bounds a directory after decompression, and maxMetadataBytes the JSON metadata before and
	// after it. A z10–z15 city extract needs a few kB of each.
	maxDirectoryBytes = 1 << 20
	maxMetadataBytes  = 1 << 20

	// maxLeafDepth is how many levels of leaf directories a lookup follows below the root.
	maxLeafDepth = 3
)

// compression is the PMTiles compression byte.
type compression uint8

const (
	compressionUnknown compression = 0
	compressionNone    compression = 1
	compressionGzip    compression = 2
	compressionBrotli  compression = 3
	compressionZstd    compression = 4
)

func (c compression) String() string {
	switch c {
	case compressionUnknown:
		return "unknown"
	case compressionNone:
		return "none"
	case compressionGzip:
		return "gzip"
	case compressionBrotli:
		return "brotli"
	case compressionZstd:
		return "zstd"
	}
	return fmt.Sprintf("compression %d", uint8(c))
}

// contentEncoding is the HTTP Content-Encoding of tiles stored with this compression; "" for none or unknown.
func (c compression) contentEncoding() string {
	switch c {
	case compressionGzip:
		return "gzip"
	case compressionBrotli:
		return "br"
	case compressionZstd:
		return "zstd"
	}
	return ""
}

// tileTypeMVT is the PMTiles tile type of Mapbox Vector Tiles, the only type served.
const tileTypeMVT = 1

// header is the fixed 127-byte PMTiles v3 header.
type header struct {
	rootOffset, rootLength         uint64
	metadataOffset, metadataLength uint64
	leafOffset, leafLength         uint64
	tileDataOffset, tileDataLength uint64
	internalCompression            compression
	tileCompression                compression
	tileType                       uint8
	minZoom, maxZoom               uint8
	minLonE7, minLatE7             int32
	maxLonE7, maxLatE7             int32
	centerZoom                     uint8
	centerLonE7, centerLatE7       int32
}

var errNotPMTiles = errors.New("not a PMTiles archive")

// parseHeader decodes and checks the header of an archive of the given size.
func parseHeader(b []byte, size uint64) (header, error) {
	var h header
	if len(b) < headerLen || string(b[:7]) != "PMTiles" {
		return h, errNotPMTiles
	}
	if b[7] != 3 {
		return h, fmt.Errorf("PMTiles spec version %d, want 3", b[7])
	}
	le := binary.LittleEndian
	u64 := func(at int) uint64 { return le.Uint64(b[at:]) }
	i32 := func(at int) int32 { return int32(le.Uint32(b[at:])) } //nolint:gosec // G115: two's complement by the spec
	h = header{
		rootOffset: u64(8), rootLength: u64(16),
		metadataOffset: u64(24), metadataLength: u64(32),
		leafOffset: u64(40), leafLength: u64(48),
		tileDataOffset: u64(56), tileDataLength: u64(64),
		// 72, 80, 88: tile counts; 96: clustered. Not needed for reading.
		internalCompression: compression(b[97]),
		tileCompression:     compression(b[98]),
		tileType:            b[99],
		minZoom:             b[100], maxZoom: b[101],
		minLonE7: i32(102), minLatE7: i32(106), maxLonE7: i32(110), maxLatE7: i32(114),
		centerZoom:  b[118],
		centerLonE7: i32(119), centerLatE7: i32(123),
	}
	switch {
	case h.internalCompression != compressionNone && h.internalCompression != compressionGzip:
		return h, fmt.Errorf("unsupported internal compression %s (only none and gzip)", h.internalCompression)
	case h.tileType != tileTypeMVT:
		return h, fmt.Errorf("tile type %d, want MVT (1)", h.tileType)
	case h.minZoom > h.maxZoom || h.maxZoom > maxZoom:
		return h, fmt.Errorf("zoom levels %d to %d", h.minZoom, h.maxZoom)
	case h.rootLength == 0 || !within(h.rootOffset, h.rootLength, min(size, rootDirectoryBytes)) ||
		h.rootOffset < headerLen:
		return h, errors.New("root directory outside the first 16 KiB")
	case !within(h.metadataOffset, h.metadataLength, size),
		!within(h.leafOffset, h.leafLength, size),
		!within(h.tileDataOffset, h.tileDataLength, size):
		return h, errors.New("truncated: a section ends beyond the end of the file")
	case h.metadataLength > maxMetadataBytes:
		return h, fmt.Errorf("metadata of %d bytes", h.metadataLength)
	}
	return h, nil
}

// within reports whether the section [offset, offset+length) lies inside [0, limit).
func within(offset, length, limit uint64) bool {
	end := offset + length
	return end >= offset && end <= limit
}

// entry is a directory entry. A runLength of 0 makes it a pointer to a leaf directory at offset (relative to the leaf
// directory section); otherwise it stands for the runLength tiles from tileID on, all stored at offset (relative to
// the tile data section).
type entry struct {
	tileID    uint64
	offset    uint64
	length    uint32
	runLength uint32
}

// entryBytes is the memory one decoded entry takes, for the directory cache's budget.
const entryBytes = 24

// decompress returns b decompressed with c, refusing results larger than limit.
func decompress(b []byte, c compression, limit int64) ([]byte, error) {
	var r io.Reader
	switch c {
	case compressionNone:
		r = bytes.NewReader(b)
	case compressionGzip:
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		r = zr
	default:
		return nil, fmt.Errorf("unsupported internal compression %s", c)
	}
	out, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(out)) > limit {
		return nil, fmt.Errorf("more than %d bytes after decompression", limit)
	}
	return out, nil
}

// parseDirectory decodes a serialized directory (already decompressed).
func parseDirectory(b []byte) ([]entry, error) {
	r := bufio.NewReader(bytes.NewReader(b))
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("directory: %w", err)
	}
	// Each entry takes at least 4 bytes (one varint per field), which bounds the allocation by the input.
	if n == 0 || n > uint64(len(b))/4 {
		return nil, fmt.Errorf("directory: %d entries in %d bytes", n, len(b))
	}
	entries := make([]entry, n)
	var id uint64
	for i := range entries {
		delta, err := binary.ReadUvarint(r)
		if err != nil {
			return nil, fmt.Errorf("directory: tile ID: %w", err)
		}
		if i > 0 && delta == 0 || id+delta < id {
			return nil, errors.New("directory: tile IDs not increasing")
		}
		id += delta
		entries[i].tileID = id
	}
	for i := range entries {
		v, err := readUvarint32(r)
		if err != nil {
			return nil, fmt.Errorf("directory: run length: %w", err)
		}
		entries[i].runLength = v
	}
	for i := range entries {
		v, err := readUvarint32(r)
		if err != nil {
			return nil, fmt.Errorf("directory: length: %w", err)
		}
		if v == 0 {
			return nil, errors.New("directory: zero length")
		}
		entries[i].length = v
	}
	for i := range entries {
		v, err := binary.ReadUvarint(r)
		if err != nil {
			return nil, fmt.Errorf("directory: offset: %w", err)
		}
		switch {
		case v > 0:
			entries[i].offset = v - 1
		case i > 0: // 0: right after the previous entry
			entries[i].offset = entries[i-1].offset + uint64(entries[i-1].length)
		default:
			return nil, errors.New("directory: first offset missing")
		}
	}
	return entries, nil
}

func readUvarint32(r io.ByteReader) (uint32, error) {
	v, err := binary.ReadUvarint(r)
	if err != nil {
		return 0, err
	}
	if v > 1<<32-1 {
		return 0, errors.New("value out of range")
	}
	return uint32(v), nil
}

// findEntry returns the entry that covers tileID: the last entry whose tileID is not above it, if that is a leaf
// pointer or a run that reaches tileID.
func findEntry(entries []entry, tileID uint64) (entry, bool) {
	i := sort.Search(len(entries), func(i int) bool { return entries[i].tileID > tileID }) - 1
	if i < 0 {
		return entry{}, false
	}
	e := entries[i]
	if e.runLength == 0 || tileID-e.tileID < uint64(e.runLength) {
		return e, true
	}
	return entry{}, false
}

// tileID returns the PMTiles tile ID of z/x/y: the number of tiles on all lower zoom levels plus the tile's position
// on the Hilbert curve of its level. x and y must be below 2^z, z at most maxZoom.
func tileID(z uint8, x, y uint32) uint64 {
	var id uint64
	for level := range z {
		id += uint64(1) << (2 * level)
	}
	n := uint32(1) << z
	for s := n / 2; s > 0; s /= 2 {
		var rx, ry uint32
		if x&s != 0 {
			rx = 1
		}
		if y&s != 0 {
			ry = 1
		}
		id += uint64(s) * uint64(s) * uint64((3*rx)^ry)
		// Rotate the quadrant so that the curve continues in the right orientation.
		if ry == 0 {
			if rx == 1 {
				x = n - 1 - x
				y = n - 1 - y
			}
			x, y = y, x
		}
	}
	return id
}
