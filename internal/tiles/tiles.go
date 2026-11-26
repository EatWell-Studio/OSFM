package tiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RoutePrefix is the path prefix of every request the [Handler] answers; register it as RoutePrefix + "{path...}".
const RoutePrefix = "/tiles/"

const (
	// directoryCacheBytes is the budget of the cache of decoded leaf directories. The leaf directories of a z10–z15
	// city extract take a few kB, so this holds many cities.
	directoryCacheBytes = 4 << 20

	// maxZoom bounds the zoom levels a request or an archive may name, so that tile IDs and the tile grid fit their
	// integer types. The archives themselves stop at z15.
	maxZoom = 30

	// Tiles change only when an archive is extracted again; a day of freshness, then revalidation by ETag. They are
	// "private" because they will be served only to signed-in members (T007), so shared caches must not keep them.
	tileCacheControl = "private, max-age=86400"
	// The TileJSON and the city index follow the archives (zoom levels, bounds), so they are revalidated on every use.
	tileJSONCacheControl = "private, no-cache"

	// tileContentType is the media type of Mapbox Vector Tiles that MapLibre and most tile servers use.
	tileContentType = "application/x-protobuf"
)

// cityPattern is what a city name may look like.
var cityPattern = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// Handler serves the PMTiles archives of one directory as z/x/y tiles and TileJSON; see the package documentation.
type Handler struct {
	dir       string
	publicURL string
	logger    *slog.Logger
	dirs      *dirCache

	mu       sync.Mutex
	listedAt time.Time           // the directory's modification time when files was read
	readAt   time.Time           // when files was read
	files    map[string]string   // the directory's *.pmtiles files: path by file name
	archives map[string]*archive // by city: the file as it was last loaded
}

// NewHandler returns a handler for the archives <dir>/<city>.pmtiles. The directory need not exist yet; cities are
// looked up on each request, so archives can be added or replaced while the server runs (replace a file by renaming
// a complete one over it, as deploy/tiles.sh does).
//
// publicURL is the absolute URL the instance is reached at, without a trailing slash (OSFM_PUBLIC_URL); the TileJSON
// gives tile URLs below it. Empty means root-relative tile URLs ("/tiles/..."). logger receives one warning for each
// archive file that is not served, and why; nil discards them.
func NewHandler(dir, publicURL string, logger *slog.Logger) (*Handler, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("tiles: directory %q: %w", dir, err)
	}
	return &Handler{
		dir:       abs,
		publicURL: strings.TrimSuffix(publicURL, "/"),
		logger:    logger,
		dirs:      newDirCache(directoryCacheBytes),
		archives:  make(map[string]*archive),
	}, nil
}

// ServeHTTP answers GET and HEAD requests for RoutePrefix + "<city>/<z>/<x>/<y>.mvt", RoutePrefix + "<city>.json" and
// RoutePrefix itself, the index of the cities.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, RoutePrefix)
	if !ok {
		notFound(w)
		return
	}
	if rest == "" {
		h.serveIndex(w, r)
		return
	}
	if city, ok := strings.CutSuffix(rest, ".json"); ok && cityPattern.MatchString(city) {
		h.serveTileJSON(w, r, city)
		return
	}
	if city, z, x, y, ok := parseTilePath(rest); ok {
		h.serveTile(w, r, city, z, x, y)
		return
	}
	notFound(w)
}

// parseTilePath splits "<city>/<z>/<x>/<y>.mvt" and accepts it only when the city name is valid and the tile exists in
// the grid of its zoom level; a tile ID is defined only for those.
func parseTilePath(path string) (city string, z uint8, x, y uint32, ok bool) {
	parts := strings.Split(path, "/")
	if len(parts) != 4 || !cityPattern.MatchString(parts[0]) {
		return "", 0, 0, 0, false
	}
	last, ok := strings.CutSuffix(parts[3], ".mvt")
	if !ok {
		return "", 0, 0, 0, false
	}
	zoom, okZ := parseCoordinate(parts[1])
	x, okX := parseCoordinate(parts[2])
	y, okY := parseCoordinate(last)
	if !okZ || !okX || !okY || zoom > maxZoom || x >= 1<<zoom || y >= 1<<zoom {
		return "", 0, 0, 0, false
	}
	return parts[0], uint8(zoom), x, y, true
}

// parseCoordinate accepts a decimal number in its canonical form only (no sign, no leading zeros), so that each tile
// has exactly one URL and caches do not hold copies under several.
func parseCoordinate(s string) (uint32, bool) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || strconv.FormatUint(n, 10) != s {
		return 0, false
	}
	return uint32(n), true
}

func (h *Handler) serveTileJSON(w http.ResponseWriter, r *http.Request, city string) {
	f, a, ok := h.open(city)
	if !ok {
		notFound(w)
		return
	}
	_ = f.Close()
	writeBody(w, r, a.tileJSON, "application/json", "", tileJSONCacheControl)
}

// cityIndex is the body of the index: every city with an archive that is served, by name. The bounds, center and zoom
// levels are those of the city's TileJSON, from the archive header.
type cityIndex struct {
	Cities []cityEntry `json:"cities"`
}

type cityEntry struct {
	ID      string     `json:"id"`
	Bounds  [4]float64 `json:"bounds"` // minLon, minLat, maxLon, maxLat
	Center  [3]float64 `json:"center"` // lon, lat, zoom
	MinZoom uint8      `json:"minzoom"`
	MaxZoom uint8      `json:"maxzoom"`
}

// serveIndex lists the cities whose archives are served, sorted by name; archives under invalid names and those that
// fail their checks are left out, as their tiles are not served either.
func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	index := cityIndex{Cities: []cityEntry{}}
	for _, city := range h.cities() {
		f, a, ok := h.open(city)
		if !ok {
			continue
		}
		_ = f.Close()
		index.Cities = append(index.Cities, a.cityEntry(city))
	}
	body, err := json.Marshal(index)
	if err != nil {
		// Unreachable: the index holds only strings and numbers.
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	writeBody(w, r, body, "application/json", "", tileJSONCacheControl)
}

// serveTile answers with the tile as stored in the archive; a tile outside the archive's zoom levels or not in it is a
// 404 like every other miss.
func (h *Handler) serveTile(w http.ResponseWriter, r *http.Request, city string, z uint8, x, y uint32) {
	f, a, ok := h.open(city)
	if !ok {
		notFound(w)
		return
	}
	defer func() { _ = f.Close() }()
	if z < a.header.minZoom || z > a.header.maxZoom {
		notFound(w)
		return
	}
	tile, err := a.readTile(f, z, x, y, h.dirs)
	if err != nil {
		if !errors.Is(err, errNoTile) {
			h.markBroken(city, a, err)
		}
		notFound(w)
		return
	}
	writeBody(w, r, tile, tileContentType, a.header.tileCompression.contentEncoding(), tileCacheControl)
}

func writeBody(w http.ResponseWriter, r *http.Request, body []byte, contentType, contentEncoding, cacheControl string) {
	hash := fnv.New64a()
	_, _ = hash.Write(body)
	header := w.Header()
	header.Set("Content-Type", contentType)
	if contentEncoding != "" {
		header.Set("Content-Encoding", contentEncoding)
	}
	header.Set("ETag", fmt.Sprintf(`"%016x"`, hash.Sum64()))
	header.Set("Cache-Control", cacheControl)
	header.Set("X-Content-Type-Options", "nosniff")
	// ServeContent answers If-None-Match with 304 and HEAD without a body.
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}

func notFound(w http.ResponseWriter) {
	http.Error(w, "Not found", http.StatusNotFound)
}

// open opens the archive of city and returns it with what is known about it, loading that again when the file has
// changed. ok is false when there is no such archive or it is not served; the caller closes f otherwise. All reads
// for one request go through the same open file, so a file replaced meanwhile cannot mix two archives.
func (h *Handler) open(city string) (f *os.File, a *archive, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	path, ok := h.archivePath(city + ".pmtiles")
	if !ok {
		delete(h.archives, city)
		return nil, nil, false
	}
	f, err := os.Open(path) //nolint:gosec // G304: path comes from the listing of the tiles directory
	if err != nil {
		delete(h.archives, city)
		return nil, nil, false
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		delete(h.archives, city)
		return nil, nil, false
	}
	a = h.archives[city]
	if a == nil || a.size != info.Size() || !a.modTime.Equal(info.ModTime()) {
		a, err = loadArchive(f, info.Size(), h.publicURL+RoutePrefix+city)
		if err != nil {
			h.logger.Warn("tiles: archive not served", "file", info.Name(), "error", err)
			a = &archive{size: info.Size(), err: err}
		}
		a.modTime = info.ModTime()
		h.archives[city] = a
	}
	if a.err != nil {
		_ = f.Close()
		return nil, nil, false
	}
	return f, a, true
}

// markBroken stops serving an archive in which a lookup failed, until the file changes, and logs why once.
func (h *Handler) markBroken(city string, a *archive, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if a.err == nil {
		a.err = err
		h.logger.Warn("tiles: archive not served", "file", city+".pmtiles", "error", err)
	}
}

// archivePath looks name up in the listing of the tiles directory. The path to a file is thus never built from the
// request, only taken from the listing. h.mu must be held.
func (h *Handler) archivePath(name string) (string, bool) {
	_, listed := h.files[name]
	h.refreshListing(!listed)
	path, ok := h.files[name]
	return path, ok
}

// cities returns the names of the cities whose archives are in the tiles directory, sorted; their archives may still
// fail their checks.
func (h *Handler) cities() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.refreshListing(true)
	var cities []string
	for name := range h.files {
		if city := strings.TrimSuffix(name, ".pmtiles"); cityPattern.MatchString(city) {
			cities = append(cities, city)
		}
	}
	slices.Sort(cities)
	return cities
}

// refreshListing reads the listing of the tiles directory again whenever the directory changes or, when stale is set
// (a name was not found), at most once a second, for file systems with a coarse modification time. h.mu must be held.
func (h *Handler) refreshListing(stale bool) {
	info, err := os.Stat(h.dir)
	if err != nil {
		h.files = nil
		return
	}
	if h.files != nil && info.ModTime().Equal(h.listedAt) && (!stale || time.Since(h.readAt) <= time.Second) {
		return
	}
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		h.files = nil
		return
	}
	h.files = make(map[string]string)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".pmtiles") {
			h.files[entry.Name()] = filepath.Join(h.dir, entry.Name())
		}
	}
	h.listedAt = info.ModTime()
	h.readAt = time.Now()
}
