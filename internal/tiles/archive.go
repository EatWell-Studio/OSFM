package tiles

import (
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"
)

// maxTileBytes bounds a single tile read. Protomaps basemap tiles stay below 1 MB.
const maxTileBytes = 16 << 20

// archive is what the handler knows about one archive file, identified by its size and modification time: the header,
// the decoded root directory and the TileJSON, or why the file is not served.
type archive struct {
	size    int64
	modTime time.Time

	header   header
	root     []entry
	tileJSON []byte

	// err is set when the file failed its checks on loading or a lookup found it corrupt; it is then not served
	// until it changes.
	err error
}

// loadArchive reads and checks the header, the root directory and the metadata of the archive f of the given size.
// tileURL is the URL prefix of the archive's tiles, written into its TileJSON.
func loadArchive(f io.ReaderAt, size int64, tileURL string) (*archive, error) {
	if size < 0 {
		return nil, errors.New("negative file size")
	}
	limit := uint64(size)
	head, err := readSection(f, 0, min(limit, rootDirectoryBytes))
	if err != nil {
		return nil, err
	}
	h, err := parseHeader(head, limit)
	if err != nil {
		return nil, err
	}
	rootBytes, err := decompress(head[h.rootOffset:h.rootOffset+h.rootLength], h.internalCompression, maxDirectoryBytes)
	if err != nil {
		return nil, fmt.Errorf("root directory: %w", err)
	}
	root, err := parseDirectory(rootBytes)
	if err != nil {
		return nil, fmt.Errorf("root %w", err)
	}

	metadata := map[string]any{}
	if h.metadataLength > 0 {
		raw, err := readSection(f, h.metadataOffset, h.metadataLength)
		if err != nil {
			return nil, fmt.Errorf("metadata: %w", err)
		}
		raw, err = decompress(raw, h.internalCompression, maxMetadataBytes)
		if err != nil {
			return nil, fmt.Errorf("metadata: %w", err)
		}
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return nil, fmt.Errorf("metadata: %w", err)
		}
	}
	tileJSON, err := buildTileJSON(h, metadata, tileURL)
	if err != nil {
		return nil, err
	}
	return &archive{size: size, header: h, root: root, tileJSON: tileJSON}, nil
}

// cityEntry describes the archive in the city index, with the same bounds, center and zoom levels as its TileJSON.
func (a *archive) cityEntry(city string) cityEntry {
	h := a.header
	return cityEntry{
		ID:      city,
		Bounds:  [4]float64{e7Degrees(h.minLonE7), e7Degrees(h.minLatE7), e7Degrees(h.maxLonE7), e7Degrees(h.maxLatE7)},
		Center:  [3]float64{e7Degrees(h.centerLonE7), e7Degrees(h.centerLatE7), float64(h.centerZoom)},
		MinZoom: h.minZoom,
		MaxZoom: h.maxZoom,
	}
}

// e7Degrees converts the fixed-point degrees of the header (degrees × 10⁷) to degrees.
func e7Degrees(v int32) float64 {
	return float64(v) / 1e7
}

// buildTileJSON returns the TileJSON 3.0 document of an archive.
func buildTileJSON(h header, metadata map[string]any, tileURL string) ([]byte, error) {
	layers, ok := metadata["vector_layers"]
	if !ok {
		layers = []any{}
	}
	tj := map[string]any{
		"tilejson":      "3.0.0",
		"scheme":        "xyz",
		"tiles":         []string{tileURL + "/{z}/{x}/{y}.mvt"},
		"vector_layers": layers,
		"minzoom":       h.minZoom,
		"maxzoom":       h.maxZoom,
		"bounds": []float64{
			e7Degrees(h.minLonE7), e7Degrees(h.minLatE7), e7Degrees(h.maxLonE7), e7Degrees(h.maxLatE7),
		},
		"center": []any{e7Degrees(h.centerLonE7), e7Degrees(h.centerLatE7), h.centerZoom},
	}
	for _, key := range []string{"attribution", "description", "name", "version"} {
		if v, ok := metadata[key]; ok {
			tj[key] = v
		}
	}
	return json.Marshal(tj)
}

// readSection reads length bytes at offset. The caller has checked the section against the file's size.
func readSection(f io.ReaderAt, offset, length uint64) ([]byte, error) {
	if offset > math.MaxInt64 || length > maxTileBytes {
		return nil, fmt.Errorf("section of %d bytes at %d", length, offset)
	}
	b := make([]byte, length)
	if _, err := f.ReadAt(b, int64(offset)); err != nil {
		return nil, err
	}
	return b, nil
}

// errNoTile means that the archive does not hold the tile.
var errNoTile = errors.New("tile not in archive")

// readTile returns the bytes of tile z/x/y as stored, or errNoTile. Leaf directories come from and go to cache.
func (a *archive) readTile(f io.ReaderAt, z uint8, x, y uint32, cache *dirCache) ([]byte, error) {
	id := tileID(z, x, y)
	dir := a.root
	for depth := 0; ; depth++ {
		e, ok := findEntry(dir, id)
		if !ok {
			return nil, errNoTile
		}
		if e.runLength > 0 {
			if !within(e.offset, uint64(e.length), a.header.tileDataLength) {
				return nil, errors.New("tile outside the tile data section")
			}
			return readSection(f, a.header.tileDataOffset+e.offset, uint64(e.length))
		}
		if depth == maxLeafDepth {
			return nil, fmt.Errorf("leaf directories nested deeper than %d levels", maxLeafDepth)
		}
		var err error
		if dir, err = a.readLeaf(f, e, cache); err != nil {
			return nil, err
		}
	}
}

func (a *archive) readLeaf(f io.ReaderAt, e entry, cache *dirCache) ([]entry, error) {
	key := dirKey{archive: a, offset: e.offset, length: e.length}
	if entries, ok := cache.get(key); ok {
		return entries, nil
	}
	if !within(e.offset, uint64(e.length), a.header.leafLength) {
		return nil, errors.New("leaf directory outside the leaf directory section")
	}
	raw, err := readSection(f, a.header.leafOffset+e.offset, uint64(e.length))
	if err != nil {
		return nil, fmt.Errorf("leaf directory: %w", err)
	}
	if raw, err = decompress(raw, a.header.internalCompression, maxDirectoryBytes); err != nil {
		return nil, fmt.Errorf("leaf directory: %w", err)
	}
	entries, err := parseDirectory(raw)
	if err != nil {
		return nil, fmt.Errorf("leaf %w", err)
	}
	cache.put(key, entries)
	return entries, nil
}

// dirCache keeps decoded leaf directories, least recently used first out, within a budget of entries. It is safe
// for concurrent use.
type dirCache struct {
	mu     sync.Mutex
	budget int // in bytes of decoded entries
	used   int
	order  *list.List // of *dirItem, most recently used at the front
	items  map[dirKey]*list.Element
}

type dirKey struct {
	archive *archive // a changed file is a new *archive, so its old directories age out
	offset  uint64
	length  uint32
}

type dirItem struct {
	key     dirKey
	entries []entry
}

func newDirCache(budget int) *dirCache {
	return &dirCache{budget: budget, order: list.New(), items: make(map[dirKey]*list.Element)}
}

func (c *dirCache) get(key dirKey) ([]entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*dirItem).entries, true
}

func (c *dirCache) put(key dirKey, entries []entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cost := len(entries) * entryBytes
	if _, ok := c.items[key]; ok || cost > c.budget {
		return
	}
	for c.used+cost > c.budget {
		oldest := c.order.Back()
		item := c.order.Remove(oldest).(*dirItem)
		delete(c.items, item.key)
		c.used -= len(item.entries) * entryBytes
	}
	c.items[key] = c.order.PushFront(&dirItem{key: key, entries: entries})
	c.used += cost
}
