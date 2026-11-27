package store

import (
	"github.com/pocketbase/pocketbase/core"
)

// WriteAudit appends an audit_log entry: who (actor, nil for the system or the member's own action), what (action,
// such as "settings.update"), about which object (targetType, the collection name, and targetUUID, the uuid of the
// target; empty for the settings row) and details. It runs in the caller's transaction when app is a transaction's
// app, so that the entry is written if and only if the change it records is.
//
// Only maintainers can read the audit log (in the admin UI); it is where the members behind changes to the places
// layer are recorded (places carry no member fields). Details never hold passwords, codes, tokens, secrets, search
// terms, coordinates or recommendation text.
func WriteAudit(app core.App, actor *core.Record, action, targetType, targetUUID string, details map[string]any) error {
	c, err := app.FindCachedCollectionByNameOrId(AuditLog)
	if err != nil {
		return err
	}
	r := core.NewRecord(c)
	if actor != nil {
		r.Set("actor", actor.Id)
	}
	r.Set("action", action)
	r.Set("target_type", targetType)
	r.Set("target_uuid", targetUUID)
	r.Set("details", details)
	return app.Save(r)
}
