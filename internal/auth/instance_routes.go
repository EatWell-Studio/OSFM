package auth

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/settings"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Changing the instance settings after the setup (G3, D5): maintainers change the instance's name and cities with
// POST /api/osfm/instance, the same rules as the setup wizard's last step ([settings.Normalize]), and an audit entry
// [AuditSettingsUpdate]. Members read them with GET of the same path (internal/settings). A new name is only shown:
// the issuer of the authenticator app entries made before stays what it was.

// InstanceSettingsPath changes the instance's name and cities (POST; maintainers only).
const InstanceSettingsPath = settings.InstancePath

func bindInstanceRoutes(se *core.ServeEvent) {
	se.Router.POST(InstanceSettingsPath, handleUpdateInstance).Bind(RequireMaintainer())
}

// UpdateInstance checks the settings and saves them on behalf of actor, with the audit entry. It returns them as
// saved.
func UpdateInstance(app core.App, actor *core.Record, in settings.Instance) (settings.Instance, error) {
	inst, err := settings.Normalize(in)
	if err != nil {
		return settings.Instance{}, err
	}
	err = app.RunInTransaction(func(tx core.App) error {
		if err := settings.Save(tx, inst); err != nil {
			return err
		}
		slugs := make([]string, 0, len(inst.Cities))
		for _, c := range inst.Cities {
			slugs = append(slugs, c.Slug)
		}
		return writeAudit(tx, actor, AuditSettingsUpdate, store.InstanceSettings, "",
			map[string]any{"name": inst.Name, "cities": slugs})
	})
	if err != nil {
		return settings.Instance{}, fmt.Errorf("auth: update the instance settings: %w", err)
	}
	return inst, nil
}

func handleUpdateInstance(e *core.RequestEvent) error {
	var req settings.Instance
	if err := readJSONLimit(e, &req, maxSetupCompleteBody); err != nil {
		return badRequest(e)
	}
	inst, err := UpdateInstance(e.App, CurrentMember(e), req)
	switch {
	case errors.Is(err, settings.ErrInvalidName), errors.Is(err, settings.ErrInvalidCities):
		return setupError(e, err)
	case err != nil:
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, inst)
}
