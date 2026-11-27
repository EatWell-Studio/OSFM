package migrations

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Credential management and reset links (D5):
//
//   - credentials.last_used_at: the UTC day a credential last proved a sign-in or a re-verification, for the members'
//     list of their own credentials. Only the day, as with sessions.last_seen_at.
//   - sessions.verified_at: when the member last proved a factor on this session (the sign-in, or a re-verification).
//     Sensitive changes to a member's credentials need it to be recent.
//   - reset_links.uuid and reset_links.revoked_at: maintainers list and revoke pending reset links by uuid.
func init() {
	m.Register(upCredentialManagement, downCredentialManagement)
}

func upCredentialManagement(app core.App) error {
	creds, err := app.FindCollectionByNameOrId(store.Credentials)
	if err != nil {
		return err
	}
	creds.Fields.Add(&core.DateField{Name: "last_used_at"})
	if err := app.Save(creds); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}

	sessions, err := app.FindCollectionByNameOrId(store.Sessions)
	if err != nil {
		return err
	}
	sessions.Fields.Add(&core.DateField{Name: "verified_at"})
	if err := app.Save(sessions); err != nil {
		return fmt.Errorf("sessions: %w", err)
	}

	links, err := app.FindCollectionByNameOrId(store.ResetLinks)
	if err != nil {
		return err
	}
	links.Fields.Add(
		&core.TextField{Name: store.FieldUUID, Required: true, Min: 36, Max: 36, Pattern: store.UUIDPattern},
		&core.DateField{Name: "revoked_at"},
	)
	// First without the unique index: rows written before this migration have no uuid yet.
	if err := app.Save(links); err != nil {
		return fmt.Errorf("reset_links: %w", err)
	}
	if err := backfillUUIDs(app, store.ResetLinks); err != nil {
		return fmt.Errorf("reset_links: %w", err)
	}
	links.AddIndex("idx_"+links.Name+"_uuid", true, "`"+store.FieldUUID+"`", "")
	if err := app.Save(links); err != nil {
		return fmt.Errorf("reset_links: %w", err)
	}
	return nil
}

func downCredentialManagement(app core.App) error {
	links, err := app.FindCollectionByNameOrId(store.ResetLinks)
	if err != nil {
		return err
	}
	links.RemoveIndex("idx_" + links.Name + "_uuid")
	links.Fields.RemoveByName(store.FieldUUID)
	links.Fields.RemoveByName("revoked_at")
	if err := app.Save(links); err != nil {
		return fmt.Errorf("reset_links: %w", err)
	}

	sessions, err := app.FindCollectionByNameOrId(store.Sessions)
	if err != nil {
		return err
	}
	sessions.Fields.RemoveByName("verified_at")
	if err := app.Save(sessions); err != nil {
		return fmt.Errorf("sessions: %w", err)
	}

	creds, err := app.FindCollectionByNameOrId(store.Credentials)
	if err != nil {
		return err
	}
	creds.Fields.RemoveByName("last_used_at")
	if err := app.Save(creds); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	return nil
}
