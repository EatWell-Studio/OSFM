// Package settings reads and writes the instance settings, the single row of instance_settings (architecture §3.5):
// the instance's name and its cities, which the first-start setup wizard sets (G3, internal/auth), and the circle
// settings maintainers change later ([Circle]). internal/api serves them, GET and POST /api/osfm/instance.
//
// A city is a display name, a slug, a bounding box and a center. The slug is also the name of the city's basemap
// archive, <data>/tiles/<slug>.pmtiles (internal/tiles), so a city can be entered before its archive exists: the map
// shows that it has no tiles yet until the archive is placed (deploy/tiles.sh <slug> <bbox>).
package settings

import (
	"errors"
	"fmt"
	"math"
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
	name, err := NormalizeName(in.Name)
	if err != nil {
		return Instance{}, err
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

// NormalizeName returns an instance name as stored, trimmed, or [ErrInvalidName] when it is empty, longer than
// [MaxNameLength] characters or contains control or format characters.
func NormalizeName(name string) (string, error) {
	name, ok := normalizeText(name, MaxNameLength)
	if !ok {
		return "", ErrInvalidName
	}
	return name, nil
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

// SaveName writes the instance name, which [NormalizeName] has checked, into the settings row. It runs in the
// caller's transaction when app is a transaction's app.
func SaveName(app core.App, name string) error {
	r, err := app.FindRecordById(store.InstanceSettings, store.SettingsID)
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	r.Set("name", name)
	if err := app.Save(r); err != nil {
		return fmt.Errorf("settings: save: %w", err)
	}
	return nil
}

// Circle holds the instance settings besides the name and the cities, which maintainers change on the Admin page
// (POST /api/osfm/instance, internal/api): the report threshold (D11), the invite cooling-off period for new members
// (D12) and place search (B7, M2-Q13).
type Circle struct {
	// ReportThreshold is the number of open reports that hide a recommendation until a maintainer reviews it.
	ReportThreshold int
	// InviteCooldownDays is the number of days after joining before a new member may invite; 0 for none.
	InviteCooldownDays int
	// GeocoderEnabled turns place search on; the search terms then go from this server to the search services.
	GeocoderEnabled bool
	// GeocoderContact is sent to the search services with each request (their usage policies ask for one); may be
	// empty.
	GeocoderContact string
}

// Limits of the circle settings.
const (
	MinReportThreshold      = 1
	MaxReportThreshold      = 100
	MaxInviteCooldownDays   = 3650
	MaxGeocoderContactChars = 200 // instance_settings.geocoder_contact, in characters
)

// Validation errors of [NormalizeCircle].
var (
	ErrInvalidReportThreshold = fmt.Errorf("settings: the report threshold is a whole number from %d to %d",
		MinReportThreshold, MaxReportThreshold)
	ErrInvalidInviteCooldown = fmt.Errorf("settings: the invite cooling-off period is a whole number of days from 0 to %d",
		MaxInviteCooldownDays)
	ErrInvalidGeocoderContact = fmt.Errorf("settings: the search contact has at most %d characters and no control characters",
		MaxGeocoderContactChars)
)

// NormalizeCircle checks the circle settings and returns them as stored: the contact trimmed. It returns
// [ErrInvalidReportThreshold], [ErrInvalidInviteCooldown] or [ErrInvalidGeocoderContact].
func NormalizeCircle(c Circle) (Circle, error) {
	if c.ReportThreshold < MinReportThreshold || c.ReportThreshold > MaxReportThreshold {
		return Circle{}, ErrInvalidReportThreshold
	}
	if c.InviteCooldownDays < 0 || c.InviteCooldownDays > MaxInviteCooldownDays {
		return Circle{}, ErrInvalidInviteCooldown
	}
	c.GeocoderContact = strings.TrimSpace(c.GeocoderContact)
	if c.GeocoderContact != "" {
		contact, ok := normalizeText(c.GeocoderContact, MaxGeocoderContactChars)
		if !ok {
			return Circle{}, ErrInvalidGeocoderContact
		}
		c.GeocoderContact = contact
	}
	return c, nil
}

// LoadCircle returns the circle settings as stored. Features read them when they need them (T034 the cooling-off
// period, T036 the report threshold, T039 place search), so that a change applies at once.
func LoadCircle(app core.App) (Circle, error) {
	r, err := app.FindRecordById(store.InstanceSettings, store.SettingsID)
	if err != nil {
		return Circle{}, fmt.Errorf("settings: %w", err)
	}
	return Circle{
		ReportThreshold:    max(MinReportThreshold, r.GetInt("report_threshold")),
		InviteCooldownDays: max(0, r.GetInt("invite_cooldown_days")),
		GeocoderEnabled:    r.GetBool("geocoder_enabled"),
		GeocoderContact:    strings.TrimSpace(r.GetString("geocoder_contact")),
	}, nil
}

// SaveCircle writes the circle settings, which [NormalizeCircle] has checked, into the settings row. It runs in the
// caller's transaction when app is a transaction's app.
func SaveCircle(app core.App, c Circle) error {
	r, err := app.FindRecordById(store.InstanceSettings, store.SettingsID)
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	r.Set("report_threshold", c.ReportThreshold)
	r.Set("invite_cooldown_days", c.InviteCooldownDays)
	r.Set("geocoder_enabled", c.GeocoderEnabled)
	r.Set("geocoder_contact", c.GeocoderContact)
	if err := app.Save(r); err != nil {
		return fmt.Errorf("settings: save: %w", err)
	}
	return nil
}
