// Package settings reads and writes the instance settings, the single row of instance_settings (architecture §3.5):
// the instance's name and its cities, which the first-start setup wizard sets (G3, internal/auth), and it serves them
// to members, GET /api/osfm/instance, for the top bar and the map's city switcher.
//
// A city is a display name, a slug, a bounding box and a center. The slug is also the name of the city's basemap
// archive, <data>/tiles/<slug>.pmtiles (internal/tiles), so a city can be entered before its archive exists: the map
// shows that it has no tiles yet until the archive is placed (deploy/tiles.sh <slug> <bbox>).
package settings

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/boundary"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/tiles"
)

// InstancePath answers with the instance's name and cities (GET). Like every path under /api/osfm/, it needs a
// session (internal/auth): the cities say where the circle is (E1).
const InstancePath = boundary.OSFMPrefix + "instance"

// Limits of the settings.
const (
	MaxNameLength     = 100 // instance_settings.name, in characters
	MaxCityNameLength = 64  // in characters
	MaxCities         = 32
)

// City is one city of the instance, as instance_settings.cities stores it and InstancePath returns it.
type City struct {
	// Slug names the city in URLs and is the name of its basemap archive (<slug>.pmtiles): 1 to 64 lowercase
	// letters, digits and '-' ([tiles.ValidCityName]).
	Slug string `json:"slug"`
	// Name is the display name.
	Name string `json:"name"`
	// BBox is minLon, minLat, maxLon, maxLat in WGS-84 degrees.
	BBox [4]float64 `json:"bbox"`
	// Center is lon, lat, inside BBox.
	Center [2]float64 `json:"center"`
}

// Instance is the response of GET [InstancePath], and what the setup wizard sets.
type Instance struct {
	Name   string `json:"name"`
	Cities []City `json:"cities"`
}

// Validation errors of [Normalize].
var (
	ErrInvalidName = fmt.Errorf("settings: an instance name has 1 to %d characters and no control characters",
		MaxNameLength)
	ErrInvalidCities = errors.New("settings: invalid cities")
)

// Normalize checks the settings and returns them as they are stored: names trimmed. The name must not be empty, and
// there must be 1 to [MaxCities] cities with distinct, valid slugs, names of 1 to [MaxCityNameLength] characters, a
// bounding box with min < max within the WGS-84 range, and a center inside it. It returns [ErrInvalidName] or an
// error wrapping [ErrInvalidCities].
func Normalize(in Instance) (Instance, error) {
	name, ok := normalizeText(in.Name, MaxNameLength)
	if !ok {
		return Instance{}, ErrInvalidName
	}
	if len(in.Cities) == 0 || len(in.Cities) > MaxCities {
		return Instance{}, fmt.Errorf("%w: 1 to %d cities", ErrInvalidCities, MaxCities)
	}
	out := Instance{Name: name, Cities: make([]City, 0, len(in.Cities))}
	seen := map[string]bool{}
	for i, c := range in.Cities {
		c, err := normalizeCity(c)
		if err != nil {
			return Instance{}, fmt.Errorf("%w: city %d: %w", ErrInvalidCities, i+1, err)
		}
		if seen[c.Slug] {
			return Instance{}, fmt.Errorf("%w: city %d: the slug %q is used twice", ErrInvalidCities, i+1, c.Slug)
		}
		seen[c.Slug] = true
		out.Cities = append(out.Cities, c)
	}
	return out, nil
}

func normalizeCity(c City) (City, error) {
	if !tiles.ValidCityName(c.Slug) {
		return City{}, errors.New("invalid slug")
	}
	name, ok := normalizeText(c.Name, MaxCityNameLength)
	if !ok {
		return City{}, errors.New("invalid name")
	}
	c.Name = name
	minLon, minLat, maxLon, maxLat := c.BBox[0], c.BBox[1], c.BBox[2], c.BBox[3]
	for _, v := range append(c.BBox[:], c.Center[:]...) {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return City{}, errors.New("not a number")
		}
	}
	if minLon < -180 || maxLon > 180 || minLat < -90 || maxLat > 90 || minLon >= maxLon || minLat >= maxLat {
		return City{}, errors.New("invalid bounding box")
	}
	if c.Center[0] < minLon || c.Center[0] > maxLon || c.Center[1] < minLat || c.Center[1] > maxLat {
		return City{}, errors.New("the center is outside the bounding box")
	}
	return c, nil
}

// normalizeText trims s; ok is false when it is empty, longer than maxLen characters or contains control or format
// characters.
func normalizeText(s string, maxLen int) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxLen {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", false
		}
	}
	return s, true
}

// Load returns the instance settings. Cities that are not valid (the row can be edited outside OSFM, in the admin
// UI) are left out.
func Load(app core.App) (Instance, error) {
	r, err := app.FindRecordById(store.InstanceSettings, store.SettingsID)
	if err != nil {
		return Instance{}, fmt.Errorf("settings: %w", err)
	}
	inst := Instance{Name: strings.TrimSpace(r.GetString("name")), Cities: []City{}}
	var stored []City
	if err := r.UnmarshalJSONField("cities", &stored); err != nil {
		app.Logger().Warn("instance_settings.cities is not a list of cities", "error", err)
		return inst, nil
	}
	seen := map[string]bool{}
	for _, c := range stored {
		if c, err := normalizeCity(c); err == nil && !seen[c.Slug] {
			seen[c.Slug] = true
			inst.Cities = append(inst.Cities, c)
		}
	}
	return inst, nil
}

// Save writes the name and the cities of inst, which [Normalize] has checked, into the settings row. It runs in the
// caller's transaction when app is a transaction's app.
func Save(app core.App, inst Instance) error {
	r, err := app.FindRecordById(store.InstanceSettings, store.SettingsID)
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	r.Set("name", inst.Name)
	r.Set("cities", inst.Cities)
	if err := app.Save(r); err != nil {
		return fmt.Errorf("settings: save: %w", err)
	}
	return nil
}

// Bind registers GET [InstancePath] when app serves.
func Bind(app core.App) {
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.Router.GET(InstancePath, handleInstance)
		return se.Next()
	})
}

func handleInstance(e *core.RequestEvent) error {
	inst, err := Load(e.App)
	if err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, inst)
}
