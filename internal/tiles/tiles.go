package tiles

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/protomaps/go-pmtiles/pmtiles"
)

// RoutePrefix is the path prefix of every request the [Handler] answers; register it as RoutePrefix + "{path...}".
const RoutePrefix = "/tiles/"

const (
	// directoryCacheMB is the size of go-pmtiles' in-memory cache of archive directories, in MB (10^6 bytes). The
	// directories of a z10–z15 city extract take a few kB, so this holds many cities.
	directoryCacheMB = 4

	// maxZoom bounds the zoom levels a request may name, so that x and y fit the tile grid's integer types. The
	// archives themselves stop at z15; anything above their maximum is answered with 404 anyway.
	maxZoom = 30

	// rootDirectoryBytes is how far into an archive the header and the root directory may reach (PMTiles v3).
	rootDirectoryBytes = 16384

	// Tiles change only when an archive is extracted again; a day of freshness, then revalidation by ETag. They are
	// "private" because they will be served only to signed-in members (T007), so shared caches must not keep them.
	tileCacheControl = "private, max-age=86400"
	// The TileJSON follows the archive (zoom levels, bounds), so it is revalidated on every use.
	tileJSONCacheControl = "private, no-cache"
)

// cityPattern is what a city name may look like. It doubles as the guard against path traversal: the name becomes a
// file name in the tiles directory.
var cityPattern = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// Handler serves the PMTiles archives of one directory as z/x/y tiles and TileJSON; see the package documentation.
type Handler struct {
	dir    string
	server *pmtiles.Server
	logger *slog.Logger

	mu       sync.Mutex
	listedAt time.Time               // the directory's modification time when files was read
	readAt   time.Time               // when files was read
	files    map[string]string       // the directory's *.pmtiles files: path by file name
	archives map[string]archiveState // by city: the result of checkArchive for the file as it was last seen
}

type archiveState struct {
	modTime time.Time
	size    int64
	usable  bool
}

// NewHandler returns a handler for the archives <dir>/<city>.pmtiles. The directory need not exist yet; cities are
// looked up on each request, so archives can be added or replaced while the server runs (replace a file by renaming
// a complete one over it, as deploy/tiles.sh does).
//
// publicURL is the URL prefix of the tile URLs written into the TileJSON, without a trailing slash; empty means
// "/tiles", a path relative to the server's own origin. logger receives warnings about unusable archives and, at
// debug level, go-pmtiles' own log; nil discards both.
func NewHandler(dir, publicURL string, logger *slog.Logger) (*Handler, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if publicURL == "" {
		publicURL = strings.TrimSuffix(RoutePrefix, "/")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("tiles: directory %q: %w", dir, err)
	}
	// go-pmtiles logs every directory it fetches; that is debug output.
	libraryLog := slog.NewLogLogger(logger.Handler(), slog.LevelDebug)
	server, err := pmtiles.NewServerWithBucket(pmtiles.NewFileBucket(abs), "", libraryLog, directoryCacheMB, strings.TrimSuffix(publicURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("tiles: %w", err)
	}
	server.Start()
	return &Handler{dir: abs, server: server, logger: logger, archives: make(map[string]archiveState)}, nil
}

// ServeHTTP answers GET and HEAD requests for RoutePrefix + "<city>/<z>/<x>/<y>.mvt" and RoutePrefix + "<city>.json".
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
	if city, ok := strings.CutSuffix(rest, ".json"); ok && cityPattern.MatchString(city) {
		h.serve(w, r, city, "/"+city+".json", tileJSONCacheControl)
		return
	}
	if city, z, x, y, ok := parseTilePath(rest); ok {
		h.serve(w, r, city, fmt.Sprintf("/%s/%d/%d/%d.mvt", city, z, x, y), tileCacheControl)
		return
	}
	notFound(w)
}

// parseTilePath splits "<city>/<z>/<x>/<y>.mvt" and accepts it only when the city name is valid and the tile exists in
// the grid of its zoom level. go-pmtiles does not check the latter: it would map an x or y beyond the grid onto
// another tile.
func parseTilePath(path string) (city string, z, x, y uint32, ok bool) {
	parts := strings.Split(path, "/")
	if len(parts) != 4 || !cityPattern.MatchString(parts[0]) {
		return "", 0, 0, 0, false
	}
	last, ok := strings.CutSuffix(parts[3], ".mvt")
	if !ok {
		return "", 0, 0, 0, false
	}
	z, okZ := parseCoordinate(parts[1])
	x, okX := parseCoordinate(parts[2])
	y, okY := parseCoordinate(last)
	if !okZ || !okX || !okY || z > maxZoom || x >= 1<<z || y >= 1<<z {
		return "", 0, 0, 0, false
	}
	return parts[0], z, x, y, true
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

// serve answers from go-pmtiles for an archive that passed checkArchive. A tile the archive does not hold (go-pmtiles
// answers 204) is a 404 like every other miss.
func (h *Handler) serve(w http.ResponseWriter, r *http.Request, city, path, cacheControl string) {
	if !h.usable(city) {
		notFound(w)
		return
	}
	status, headers, body := h.server.Get(r.Context(), path)
	switch {
	case status == http.StatusOK:
	case status == 499: // the client went away
		return
	case status >= http.StatusInternalServerError:
		h.logger.Warn("tiles: archive could not be read", "city", city, "status", status, "detail", string(body))
		http.Error(w, "Tile archive could not be read", http.StatusInternalServerError)
		return
	default:
		notFound(w)
		return
	}
	for name, value := range headers { // Content-Type, Content-Encoding for tiles, ETag
		w.Header().Set(name, value)
	}
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// ServeContent answers If-None-Match with 304 and HEAD without a body.
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}

func notFound(w http.ResponseWriter) {
	http.Error(w, "Not found", http.StatusNotFound)
}

// usable reports whether the tiles directory holds <city>.pmtiles and the file passes checkArchive. The check is
// repeated only when the file's size or modification time changes.
func (h *Handler) usable(city string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	path, ok := h.archivePath(city + ".pmtiles")
	var info os.FileInfo
	var err error
	if ok {
		info, err = os.Stat(path)
	}
	if !ok || err != nil || !info.Mode().IsRegular() {
		delete(h.archives, city)
		return false
	}
	if state, ok := h.archives[city]; ok && state.size == info.Size() && state.modTime.Equal(info.ModTime()) {
		return state.usable
	}
	err = checkArchive(path, info.Size())
	if err != nil {
		h.logger.Warn("tiles: archive not served", "file", info.Name(), "error", err)
	}
	h.archives[city] = archiveState{modTime: info.ModTime(), size: info.Size(), usable: err == nil}
	return err == nil
}

// archivePath looks name up in the listing of the tiles directory, read again whenever the directory changes (or, for a
// name not found, at most once a second, for file systems with a coarse modification time). The path to a file is
// thus never built from the request, only taken from the listing. h.mu must be held.
func (h *Handler) archivePath(name string) (string, bool) {
	info, err := os.Stat(h.dir)
	if err != nil {
		h.files = nil
		return "", false
	}
	_, listed := h.files[name]
	if h.files == nil || !info.ModTime().Equal(h.listedAt) || !listed && time.Since(h.readAt) > time.Second {
		entries, err := os.ReadDir(h.dir)
		if err != nil {
			h.files = nil
			return "", false
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
	path, ok := h.files[name]
	return path, ok
}

// checkArchive verifies what go-pmtiles takes for granted when it reads an archive's header and root directory: a
// file that is too short, not PMTiles v3 or truncated would otherwise crash the process or leave requests hanging in
// its directory cache.
func checkArchive(path string, size int64) (err error) {
	if size < 0 {
		return errors.New("negative file size")
	}
	limit := uint64(size)
	file, err := os.Open(path) //nolint:gosec // G304: path comes from the listing of the tiles directory
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	head := make([]byte, min(size, rootDirectoryBytes))
	if _, err := io.ReadFull(file, head); err != nil {
		return err
	}
	if len(head) < pmtiles.HeaderV3LenBytes {
		return errors.New("too short for a PMTiles header")
	}
	header, err := pmtiles.DeserializeHeader(head)
	if err != nil {
		return err
	}
	switch {
	case header.SpecVersion != 3:
		return fmt.Errorf("PMTiles spec version %d, want 3", header.SpecVersion)
	case header.TileType != pmtiles.Mvt:
		return fmt.Errorf("tile type %d, want MVT", header.TileType)
	case header.InternalCompression != pmtiles.NoCompression && header.InternalCompression != pmtiles.Gzip:
		return fmt.Errorf("unsupported directory compression %d", header.InternalCompression)
	case !within(header.RootOffset, header.RootLength, uint64(len(head))):
		return errors.New("root directory outside the first 16 KiB")
	case !within(header.MetadataOffset, header.MetadataLength, limit),
		!within(header.LeafDirectoryOffset, header.LeafDirectoryLength, limit),
		!within(header.TileDataOffset, header.TileDataLength, limit):
		return errors.New("truncated: a section ends beyond the end of the file")
	}

	// DeserializeEntries panics on a corrupt directory.
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("unreadable root directory: %v", p)
		}
	}()
	root := head[header.RootOffset : header.RootOffset+header.RootLength]
	if entries := pmtiles.DeserializeEntries(bytes.NewBuffer(root), header.InternalCompression); len(entries) == 0 {
		return errors.New("empty root directory")
	}
	return nil
}

// within reports whether the section [offset, offset+length) lies inside [0, limit).
func within(offset, length, limit uint64) bool {
	end := offset + length
	return end >= offset && end <= limit
}
