package migrations

import (
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Passkeys (D3, D4): what a WebAuthn credential record needs besides the credential ID, public key and signature
// counter the credentials collection has had from the start, and the members' WebAuthn user handle.
//
//   - members.passkey_user_handle: the user handle (user.id) every passkey of the member is created with, base64url.
//     It is random, not the record ID or the uuid (AD4), and empty until the member has a passkey.
//   - credentials.uuid: names a credential outside the server, for the members' list of their own passkeys (T012).
//   - credentials.aaguid, transports, backup_eligible, backup_state, user_verified, attestation_type,
//     attestation_format: the authenticator model, how the browser can reach it, and the flags the WebAuthn sign-in
//     checks against (the backup-eligible flag of a credential never changes).
func init() {
	m.Register(upPasskeys, downPasskeys)
}

// passkeyCredentialFields are the fields this migration adds to credentials, besides the uuid.
var passkeyCredentialFields = []string{
	"aaguid", "transports", "backup_eligible", "backup_state", "user_verified", "attestation_type", "attestation_format",
}

func upPasskeys(app core.App) error {
	members, err := app.FindCollectionByNameOrId(store.Members)
	if err != nil {
		return err
	}
	// 32 random bytes are 43 base64url characters; WebAuthn allows up to 64 bytes (86 characters).
	members.Fields.Add(&core.TextField{Name: "passkey_user_handle", Max: 86, Pattern: `^[A-Za-z0-9_-]*$`, Hidden: true})
	uniqueIndex(members, "passkey_user_handle", "`passkey_user_handle`", "`passkey_user_handle` != ''")
	if err := app.Save(members); err != nil {
		return fmt.Errorf("members: %w", err)
	}

	c, err := app.FindCollectionByNameOrId(store.Credentials)
	if err != nil {
		return err
	}
	c.Fields.Add(
		&core.TextField{Name: store.FieldUUID, Required: true, Min: 36, Max: 36, Pattern: store.UUIDPattern},
		// The authenticator model's AAGUID as a UUID string; all zeros when the authenticator does not tell (attestation
		// "none" may zero it).
		&core.TextField{Name: "aaguid", Max: 36},
		&core.JSONField{Name: "transports", MaxSize: 1024}, // ["internal", "hybrid", …], as the browser reported them
		&core.BoolField{Name: "backup_eligible"},
		&core.BoolField{Name: "backup_state"},
		&core.BoolField{Name: "user_verified"},
		text("attestation_type", 32),
		text("attestation_format", 32),
	)
	// First without the unique index: rows written before this migration have no uuid yet.
	if err := app.Save(c); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	if err := backfillUUIDs(app, store.Credentials); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	c.AddIndex("idx_"+c.Name+"_uuid", true, "`"+store.FieldUUID+"`", "")
	if err := app.Save(c); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	return nil
}

// backfillUUIDs gives every row of the collection that has no uuid a new one.
func backfillUUIDs(app core.App, collection string) error {
	var ids []string
	if err := app.DB().Select("id").From(collection).Where(dbx.HashExp{store.FieldUUID: ""}).Column(&ids); err != nil {
		return err
	}
	for _, id := range ids {
		u, err := store.NewUUID()
		if err != nil {
			return err
		}
		if _, err := app.DB().Update(collection, dbx.Params{store.FieldUUID: u}, dbx.HashExp{"id": id}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func downPasskeys(app core.App) error {
	c, err := app.FindCollectionByNameOrId(store.Credentials)
	if err != nil {
		return err
	}
	c.RemoveIndex("idx_" + c.Name + "_uuid")
	c.Fields.RemoveByName(store.FieldUUID)
	for _, name := range passkeyCredentialFields {
		c.Fields.RemoveByName(name)
	}
	if err := app.Save(c); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}

	members, err := app.FindCollectionByNameOrId(store.Members)
	if err != nil {
		return err
	}
	members.RemoveIndex("idx_members_passkey_user_handle")
	members.Fields.RemoveByName("passkey_user_handle")
	if err := app.Save(members); err != nil {
		return fmt.Errorf("members: %w", err)
	}
	return nil
}
