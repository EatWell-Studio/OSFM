package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// The instance settings (G3, D5, D11, D12, B7). Every member reads the instance's name, cities and whether place
// search is on (the web app hides the search box without it); maintainers also read and change the report threshold,
// the invite cooling-off period and the search contact, and change the name and cities, with an audit entry
// settings.update. A POST changes only the fields it carries: the older name-and-cities form and the newer settings
// form of the Admin page each send their own.

// InstancePath is GET (members: [Instance]; maintainers: [MaintainerInstance]) and POST (maintainers:
// [InstanceUpdate]) of the instance settings (settings.InstancePath).
const InstancePath = "/api/osfm/instance"

// Error codes of the instance settings, besides the setup's auth.CodeInstanceNameInvalid and auth.CodeCitiesInvalid.
const (
	CodeReportThresholdInvalid = "report_threshold_invalid"
	CodeInviteCooldownInvalid  = "invite_cooldown_invalid"
	CodeGeocoderContactInvalid = "geocoder_contact_invalid"
)

// City is one city of the instance.
type City struct {
	// Slug names the city in URLs and is the name of its basemap archive.
	Slug string `json:"slug"`
	Name string `json:"name"`
	// BBox is minLon, minLat, maxLon, maxLat in WGS-84 degrees.
	BBox [4]float64 `json:"bbox"`
	// Center is lon, lat.
	Center [2]float64 `json:"center"`
}

// Instance is GET [InstancePath] for members.
type Instance struct {
	Name   string `json:"name"`
	Cities []City `json:"cities"`
	// GeocoderEnabled says whether place search is on.
	GeocoderEnabled bool `json:"geocoderEnabled"`
}

// MaintainerInstance is GET [InstancePath] for maintainers, and the response of POST: everything a member sees and
// the settings only maintainers change.
type MaintainerInstance struct {
	Instance `tstype:",extends"`
	// ReportThreshold is the number of open reports that hide a recommendation (at least 1).
	ReportThreshold int `json:"reportThreshold"`
	// InviteCooldownDays is the number of days after joining before a new member may invite (0 for none).
	InviteCooldownDays int `json:"inviteCooldownDays"`
	// GeocoderContact is sent to the search services with each request; may be empty.
	GeocoderContact string `json:"geocoderContact"`
}

// InstanceUpdate is the body of POST [InstancePath]. A field that is absent (or null) keeps its saved value.
type InstanceUpdate struct {
	Name               *string `json:"name,omitempty"`
	Cities             *[]City `json:"cities,omitempty"`
	ReportThreshold    *int    `json:"reportThreshold,omitempty"`
	InviteCooldownDays *int    `json:"inviteCooldownDays,omitempty"`
	GeocoderEnabled    *bool   `json:"geocoderEnabled,omitempty"`
	GeocoderContact    *string `json:"geocoderContact,omitempty"`
}

func registerSettings(se *core.ServeEvent, s *server) {
	se.Router.GET(InstancePath, handleInstance)
	se.Router.POST(InstancePath, handleUpdateInstance).Bind(MaintainersOnly(), s.WriteBudget())
}

func handleInstance(e *core.RequestEvent) error {
	inst, circle, err := loadInstance(e.App)
	if err != nil {
		return e.InternalServerError("", err)
	}
	if auth.IsMaintainer(auth.CurrentMember(e)) {
		return respond(e, http.StatusOK, maintainerInstance(inst, circle))
	}
	return respond(e, http.StatusOK, memberInstance(inst, circle))
}

func handleUpdateInstance(e *core.RequestEvent) error {
	var req InstanceUpdate
	if err := readJSON(e, &req, DefaultMaxBody); err != nil {
		return requestError(e, err)
	}
	inst, circle, err := UpdateInstance(e.App, auth.CurrentMember(e), req)
	switch {
	case errors.Is(err, settings.ErrInvalidName):
		return fail(e, http.StatusBadRequest, auth.CodeInstanceNameInvalid,
			"An instance name has 1 to 100 characters and no control characters.")
	case errors.Is(err, settings.ErrInvalidCities):
		return fail(e, http.StatusBadRequest, auth.CodeCitiesInvalid, err.Error())
	case errors.Is(err, settings.ErrInvalidReportThreshold):
		return fail(e, http.StatusBadRequest, CodeReportThresholdInvalid, err.Error())
	case errors.Is(err, settings.ErrInvalidInviteCooldown):
		return fail(e, http.StatusBadRequest, CodeInviteCooldownInvalid, err.Error())
	case errors.Is(err, settings.ErrInvalidGeocoderContact):
		return fail(e, http.StatusBadRequest, CodeGeocoderContactInvalid, err.Error())
	case err != nil:
		return e.InternalServerError("", err)
	}
	return respond(e, http.StatusOK, maintainerInstance(inst, circle))
}

// UpdateInstance applies req to the saved settings on behalf of actor, a maintainer: the fields req carries replace
// the saved ones, the others are kept. It checks the result as the setup does ([settings.Normalize],
// [settings.NormalizeCircle]) and saves it with the audit entry auth.AuditSettingsUpdate in one transaction. It
// returns the settings as saved.
func UpdateInstance(app core.App, actor *core.Record, req InstanceUpdate) (settings.Instance, settings.Circle, error) {
	inst, circle, err := loadInstance(app)
	if err != nil {
		return settings.Instance{}, settings.Circle{}, err
	}
	if req.Name != nil {
		inst.Name = *req.Name
	}
	if req.Cities != nil {
		inst.Cities = make([]settings.City, 0, len(*req.Cities))
		for _, c := range *req.Cities {
			inst.Cities = append(inst.Cities, settings.City{Slug: c.Slug, Name: c.Name, BBox: c.BBox, Center: c.Center})
		}
	}
	if req.ReportThreshold != nil {
		circle.ReportThreshold = *req.ReportThreshold
	}
	if req.InviteCooldownDays != nil {
		circle.InviteCooldownDays = *req.InviteCooldownDays
	}
	if req.GeocoderEnabled != nil {
		circle.GeocoderEnabled = *req.GeocoderEnabled
	}
	if req.GeocoderContact != nil {
		circle.GeocoderContact = *req.GeocoderContact
	}
	if inst, err = settings.Normalize(inst); err != nil {
		return settings.Instance{}, settings.Circle{}, err
	}
	if circle, err = settings.NormalizeCircle(circle); err != nil {
		return settings.Instance{}, settings.Circle{}, err
	}
	err = app.RunInTransaction(func(tx core.App) error {
		if err := settings.Save(tx, inst); err != nil {
			return err
		}
		if err := settings.SaveCircle(tx, circle); err != nil {
			return err
		}
		slugs := make([]string, 0, len(inst.Cities))
		for _, c := range inst.Cities {
			slugs = append(slugs, c.Slug)
		}
		// The contact may be a maintainer's address: the audit entry says only whether there is one.
		return store.WriteAudit(tx, actor, auth.AuditSettingsUpdate, store.InstanceSettings, "", map[string]any{
			"name": inst.Name, "cities": slugs, "reportThreshold": circle.ReportThreshold,
			"inviteCooldownDays": circle.InviteCooldownDays, "geocoderEnabled": circle.GeocoderEnabled,
			"geocoderContactSet": circle.GeocoderContact != "",
		})
	})
	if err != nil {
		return settings.Instance{}, settings.Circle{}, fmt.Errorf("api: update the instance settings: %w", err)
	}
	return inst, circle, nil
}

func loadInstance(app core.App) (settings.Instance, settings.Circle, error) {
	inst, err := settings.Load(app)
	if err != nil {
		return settings.Instance{}, settings.Circle{}, err
	}
	circle, err := settings.LoadCircle(app)
	if err != nil {
		return settings.Instance{}, settings.Circle{}, err
	}
	return inst, circle, nil
}

func memberInstance(inst settings.Instance, circle settings.Circle) Instance {
	cities := make([]City, 0, len(inst.Cities))
	for _, c := range inst.Cities {
		cities = append(cities, City{Slug: c.Slug, Name: c.Name, BBox: c.BBox, Center: c.Center})
	}
	return Instance{Name: inst.Name, Cities: cities, GeocoderEnabled: circle.GeocoderEnabled}
}

func maintainerInstance(inst settings.Instance, circle settings.Circle) MaintainerInstance {
	return MaintainerInstance{Instance: memberInstance(inst, circle), ReportThreshold: circle.ReportThreshold,
		InviteCooldownDays: circle.InviteCooldownDays, GeocoderContact: circle.GeocoderContact}
}
