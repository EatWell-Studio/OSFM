package migrations

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// pbUsers is the auth collection PocketBase's own system migrations create; this migration turns it into members.
const pbUsers = "users"

// loginNamePattern: lowercase letters, digits, "_" and "-"; the length (3–32) is checked by the field's Min and Max.
const loginNamePattern = `^[a-z0-9_-]+$`

// The M1 collections: members (the auth collection) and what authentication, invitations and administration need.
func init() {
	m.Register(func(app core.App) error {
		if err := upMembers(app); err != nil {
			return fmt.Errorf("members: %w", err)
		}
		membersID, err := collectionID(app, store.Members)
		if err != nil {
			return err
		}
		for _, up := range []func(core.App, string) error{
			upCredentials, upRecoveryCodes, upInvites, upResetLinks, upAuditLog,
		} {
			if err := up(app, membersID); err != nil {
				return err
			}
		}
		return upInstanceSettings(app)
	}, func(app core.App) error {
		for _, name := range []string{
			store.InstanceSettings, store.AuditLog, store.ResetLinks, store.Invites, store.RecoveryCodes, store.Credentials,
		} {
			if err := deleteIfExists(app, name); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		if err := downMembers(app); err != nil {
			return fmt.Errorf("members: %w", err)
		}
		return nil
	})
}

// memberFields are the fields OSFM adds to the auth collection, besides the system ones (id, password, tokenKey, email,
// emailVisibility, verified) and created/updated, which PocketBase's users collection already has.
var memberFields = []string{
	store.FieldUUID, "login_name", "display_name", "pseudonym", "external_links", "role", "can_invite",
	"invite_allowed_from", "invited_by", "joined_at", "left_at", store.FieldDeletedAt,
}

// upMembers renames PocketBase's default users collection to members and gives it the member fields. Renaming, rather
// than creating a second auth collection, keeps one auth collection with PocketBase's defaults and keeps any record a
// superuser may have created in users; such records get a uuid, a login name and a display name derived from what
// they have.
func upMembers(app core.App) error {
	c, err := app.FindCollectionByNameOrId(store.Members)
	if errors.Is(err, sql.ErrNoRows) {
		c, err = app.FindCollectionByNameOrId(pbUsers)
		if errors.Is(err, sql.ErrNoRows) {
			c, err = core.NewAuthCollection(store.Members), nil
		}
	}
	if err != nil {
		return err
	}
	if !c.IsAuth() {
		return fmt.Errorf("collection %s is not an auth collection", c.Name)
	}

	c.Name = store.Members
	superusersOnly(c)
	// OSFM does not collect e-mail addresses. PocketBase requires an auth collection to keep its system email field,
	// but not to require a value; its unique index only covers non-empty values.
	if email, ok := c.Fields.GetByName(core.FieldNameEmail).(*core.EmailField); ok {
		email.Required = false
	}
	c.Fields.Add(
		&core.TextField{Name: store.FieldUUID, Required: true, Min: 36, Max: 36, Pattern: store.UUIDPattern},
		&core.TextField{Name: "login_name", Required: true, Min: 3, Max: 32, Pattern: loginNamePattern},
		requiredText("display_name", 64),
		text("pseudonym", 64),
		&core.JSONField{Name: "external_links"},
		selectOne("role", true, "member", "maintainer"),
		&core.BoolField{Name: "can_invite"},
		&core.DateField{Name: "invite_allowed_from"},
		relation("invited_by", c.Id, false, false),
		&core.DateField{Name: "joined_at"},
		&core.DateField{Name: "left_at"},
	)
	addDeletedAt(c)
	addTimestamps(c)
	// First without the unique indexes: existing records have no uuid or login name yet.
	if err := app.Save(c); err != nil {
		return err
	}
	if err := backfillMembers(app, c); err != nil {
		return err
	}

	c.Fields.RemoveByName("name")
	c.Fields.RemoveByName("avatar")
	c.AddIndex("idx_members_uuid", true, "`uuid`", "")
	c.AddIndex("idx_members_login_name", true, "`login_name`", "")
	return app.Save(c)
}

// backfillMembers fills the required member fields of records that existed before this migration.
func backfillMembers(app core.App, c *core.Collection) error {
	records, err := app.FindAllRecords(c)
	if err != nil {
		return err
	}
	for _, r := range records {
		if r.GetString(store.FieldUUID) == "" {
			id, err := store.NewUUID()
			if err != nil {
				return err
			}
			r.Set(store.FieldUUID, id)
		}
		if r.GetString("login_name") == "" {
			r.Set("login_name", "member-"+r.Id)
		}
		if r.GetString("display_name") == "" {
			name := []rune(r.GetString("name"))
			if len(name) == 0 {
				name = []rune(r.GetString("login_name"))
			}
			r.Set("display_name", string(name[:min(len(name), 64)]))
		}
		if r.GetString("role") == "" {
			r.Set("role", "member")
		}
		if r.GetDateTime("joined_at").IsZero() {
			r.Set("joined_at", r.GetDateTime(store.FieldCreated))
		}
		if err := app.SaveNoValidate(r); err != nil {
			return fmt.Errorf("record %s: %w", r.Id, err)
		}
	}
	return nil
}

// downMembers turns members back into PocketBase's default users collection.
func downMembers(app core.App) error {
	c, err := app.FindCollectionByNameOrId(store.Members)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	c.Name = pbUsers
	for _, name := range memberFields {
		c.Fields.RemoveByName(name)
	}
	c.RemoveIndex("idx_members_uuid")
	c.RemoveIndex("idx_members_login_name")
	if email, ok := c.Fields.GetByName(core.FieldNameEmail).(*core.EmailField); ok {
		email.Required = true
	}
	// As PocketBase's system migration creates it.
	c.Fields.Add(
		&core.TextField{Name: "name", Max: 255},
		&core.FileField{
			Name: "avatar", MaxSelect: 1,
			MimeTypes: []string{"image/jpeg", "image/png", "image/svg+xml", "image/gif", "image/webp"},
		},
	)
	owner := "id = @request.auth.id"
	c.ListRule = types.Pointer(owner)
	c.ViewRule = types.Pointer(owner)
	c.CreateRule = types.Pointer("")
	c.UpdateRule = types.Pointer(owner)
	c.DeleteRule = types.Pointer(owner)
	c.OAuth2.MappedFields.Name = "name"
	c.OAuth2.MappedFields.AvatarURL = "avatar"
	return app.Save(c)
}

// credentials: a member's passkeys, or their password hash and TOTP secret (D3). Hashing and the TOTP secret's
// encryption with the instance master key are done by the authentication code.
func upCredentials(app core.App, membersID string) error {
	c, err := findOrNew(app, store.Credentials, core.CollectionTypeBase)
	if err != nil {
		return err
	}
	superusersOnly(c)
	c.Fields.Add(
		relation("member", membersID, true, true),
		selectOne("kind", true, "passkey", "password_totp"),
		// passkey: WebAuthn credential ID and COSE public key, base64url-encoded
		text("credential_id", 2048),
		text("public_key", 4096),
		integer("sign_count", ptr(0)),
		text("device_name", 100),
		// password_totp
		&core.TextField{Name: "password_hash", Max: 512, Hidden: true},
		&core.TextField{Name: "totp_secret_enc", Max: 512, Hidden: true}, // AES-GCM ciphertext
		integer("totp_last_step", ptr(0)),                                // last accepted time step, to reject reuse
	)
	addTimestamps(c)
	index(c, "member", "`member`")
	uniqueIndex(c, "credential_id", "`credential_id`", "`credential_id` != ''")
	return app.Save(c)
}

// recovery_codes: one-time codes, stored only as hashes (N5).
func upRecoveryCodes(app core.App, membersID string) error {
	c, err := findOrNew(app, store.RecoveryCodes, core.CollectionTypeBase)
	if err != nil {
		return err
	}
	superusersOnly(c)
	c.Fields.Add(
		relation("member", membersID, true, true),
		requiredText("code_hash", 255),
		&core.DateField{Name: "used_at"},
	)
	addTimestamps(c)
	index(c, "member", "`member`")
	uniqueIndex(c, "code_hash", "`code_hash`", "")
	return app.Save(c)
}

// invites: invitation codes, stored only as hashes (D12, N5).
func upInvites(app core.App, membersID string) error {
	c, err := findOrNew(app, store.Invites, core.CollectionTypeBase)
	if err != nil {
		return err
	}
	superusersOnly(c)
	addUUID(c)
	c.Fields.Add(
		requiredText("code_hash", 255),
		relation("created_by", membersID, true, false),
		&core.DateField{Name: "expires_at", Required: true},
		relation("used_by", membersID, false, false),
		&core.DateField{Name: "used_at"},
		&core.DateField{Name: "revoked_at"},
	)
	addTimestamps(c)
	uniqueIndex(c, "code_hash", "`code_hash`", "")
	index(c, "created_by", "`created_by`") // the inviter's cooldown and list of invitations
	return app.Save(c)
}

// reset_links: one-time credential reset links a maintainer creates (D5), stored only as hashes.
func upResetLinks(app core.App, membersID string) error {
	c, err := findOrNew(app, store.ResetLinks, core.CollectionTypeBase)
	if err != nil {
		return err
	}
	superusersOnly(c)
	c.Fields.Add(
		requiredText("code_hash", 255),
		relation("member", membersID, true, true),
		relation("created_by", membersID, true, false),
		&core.DateField{Name: "expires_at", Required: true},
		&core.DateField{Name: "used_at"},
	)
	addTimestamps(c)
	uniqueIndex(c, "code_hash", "`code_hash`", "")
	index(c, "member", "`member`")
	return app.Save(c)
}

// audit_log: append-only record of maintainer actions and of who created or confirmed a place (F1). Records are
// never updated (enforced by the store hooks).
func upAuditLog(app core.App, membersID string) error {
	c, err := findOrNew(app, store.AuditLog, core.CollectionTypeBase)
	if err != nil {
		return err
	}
	superusersOnly(c)
	c.Fields.Add(
		relation("actor", membersID, false, false),
		requiredText("action", 64),
		text("target_type", 64),
		text("target_uuid", 36),
		&core.JSONField{Name: "details"},
	)
	addCreated(c)
	index(c, "created", "`created`")
	index(c, "target", "`target_type`, `target_uuid`")
	index(c, "actor", "`actor`")
	return app.Save(c)
}

// instance_settings: the maintainers' settings, in a single row whose ID is store.SettingsID. The ID field accepts
// only that value, so PocketBase rejects any other row, and the primary key rejects a second row with the same ID.
func upInstanceSettings(app core.App) error {
	c, err := findOrNew(app, store.InstanceSettings, core.CollectionTypeBase)
	if err != nil {
		return err
	}
	superusersOnly(c)
	if id, ok := c.Fields.GetByName("id").(*core.TextField); ok {
		id.Min, id.Max = len(store.SettingsID), len(store.SettingsID)
		id.Pattern = "^" + store.SettingsID + "$"
		id.AutogeneratePattern = store.SettingsID
	}
	c.Fields.Add(
		text("name", 100),
		&core.JSONField{Name: "cities"}, // [{slug, name, bbox: [minLon, minLat, maxLon, maxLat], center: [lon, lat]}]
		&core.NumberField{Name: "report_threshold", OnlyInt: true, Min: ptr(1), Required: true},
		integer("invite_cooldown_days", ptr(0)),
		text("geocoder_contact", 200),
	)
	addTimestamps(c)
	if err := app.Save(c); err != nil {
		return err
	}

	_, err = app.FindRecordById(c, store.SettingsID)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	r := core.NewRecord(c)
	r.Id = store.SettingsID
	r.Set("cities", []any{})
	r.Set("report_threshold", 3)
	r.Set("invite_cooldown_days", 30)
	return app.Save(r)
}
