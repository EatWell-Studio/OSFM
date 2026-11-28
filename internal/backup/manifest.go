package backup

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Schema is the format of the backup zip and its manifest ("osfm:schema"). An incompatible change of the layout gets
// a new number; a restore refuses formats it does not know.
const Schema = "osfm-backup/1"

// Names of the entries of the zip.
const (
	DataDBName      = "data.db"      // PocketBase's main database
	AuxDBName       = "auxiliary.db" // PocketBase's logs database
	OSFMDir         = "osfm/"        // everything that is not pb_data
	ManifestName    = OSFMDir + "manifest.json"
	SecretsDir      = OSFMDir + "secrets/"
	PlacesName      = OSFMDir + "places.geojson"
	RecommendsName  = OSFMDir + "recommendations.geojson"
	maxManifestSize = 1 << 20
)

// Manifest describes a backup: osfm/manifest.json.
type Manifest struct {
	// Schema is [Schema].
	Schema string `json:"osfm:schema"`
	// CreatedAt is when the database was copied (UTC).
	CreatedAt time.Time `json:"createdAt"`
	// SoftwareVersion is the version of the OSFM program that wrote the backup (internal/version).
	SoftwareVersion string `json:"softwareVersion"`
	// Migration is the newest OSFM migration applied to the database in the backup (a migration file name, such as
	// "1795603400_member_invites.go"); "" for a database without any. A restore with a program that does not know it
	// is refused: the backup comes from a newer version.
	Migration string `json:"migration"`
	// Files lists every other entry of the zip.
	Files []File `json:"files"`
}

// File is an entry of the zip with the size and SHA-256 (hex) of its content.
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Name rules of the backups in pb_data/backups: OSFM's own, and the ones taken before migrations (migrations package).
var nameRE = regexp.MustCompile(`^(osfm|premigrate)_([0-9]{14})\.zip$`)

// Kinds of backup files ([Info].Kind).
const (
	KindOSFM       = "osfm"       // made by POST /api/osfm/backups or `osfm backup`
	KindPremigrate = "premigrate" // made before migrations (pb_data only, no keys)
)

// nameTimeLayout is the UTC time in backup names, as in the names PocketBase gives its backups.
const nameTimeLayout = "20060102150405"

// Name returns the file name of a backup made at t: osfm_<UTC time>.zip.
func Name(t time.Time) string {
	return KindOSFM + "_" + t.UTC().Format(nameTimeLayout) + ".zip"
}

// parseName returns the kind and time of a backup file name, ok false for any other name.
func parseName(name string) (kind string, t time.Time, ok bool) {
	m := nameRE.FindStringSubmatch(name)
	if m == nil {
		return "", time.Time{}, false
	}
	t, err := time.Parse(nameTimeLayout, m[2])
	if err != nil {
		return "", time.Time{}, false
	}
	return m[1], t.UTC(), true
}

// ValidName reports whether name is the name of a backup file this package lists.
func ValidName(name string) bool {
	_, _, ok := parseName(name)
	return ok
}

// ErrNewerData: the database has migrations this program does not know; it was migrated by a newer OSFM.
var ErrNewerData = errors.New("the database was migrated by a newer version of OSFM than this one")

// appliedMigrations returns the newest applied migration of db that is not one of PocketBase's system migrations, and
// the applied migrations this program does not know.
func appliedMigrations(db dbx.Builder) (newest string, unknown []string, err error) {
	var tables []string
	if err := db.NewQuery("SELECT name FROM sqlite_master WHERE type = 'table' AND name = {:t}").
		Bind(dbx.Params{"t": core.DefaultMigrationsTable}).Column(&tables); err != nil {
		return "", nil, err
	}
	if len(tables) == 0 {
		return "", nil, nil
	}
	var applied []string
	if err := db.Select("file").From(core.DefaultMigrationsTable).Column(&applied); err != nil {
		return "", nil, err
	}
	known := map[string]bool{}
	system := map[string]bool{}
	for _, item := range core.SystemMigrations.Items() {
		known[item.File], system[item.File] = true, true
	}
	for _, item := range core.AppMigrations.Items() {
		known[item.File] = true
	}
	for _, file := range applied {
		if !known[file] {
			unknown = append(unknown, file)
		}
		if !system[file] && file > newest {
			newest = file
		}
	}
	slices.Sort(unknown)
	return newest, unknown, nil
}

// safeEntryName reports whether a zip entry name is a plain relative path that stays inside the directory it is
// extracted to.
func safeEntryName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, "\x00") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
