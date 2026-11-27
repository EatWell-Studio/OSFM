package migrations_test

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

const passkeysMigration = "1795600500_passkeys.go" // #nosec G101 -- a file name

// TestPasskeysMigrationUpgrade: credentials written before the passkey fields existed get a uuid each; the down
// migration removes what the up migration added and nothing else.
func TestPasskeysMigrationUpgrade(t *testing.T) {
	app := newApp(t)
	runner := core.NewMigrationsRunner(app, core.AppMigrations)
	// Down to just before the passkeys migration, the later ones included.
	items := core.AppMigrations.Items()
	later := -1
	for i, item := range items {
		if item.File == passkeysMigration {
			later = len(items) - i
		}
	}
	if later < 1 {
		t.Fatalf("no migration %s", passkeysMigration)
	}
	if _, err := runner.Down(later); err != nil {
		t.Fatal(err)
	}
	c, err := app.FindCollectionByNameOrId(store.Credentials)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"uuid", "aaguid", "transports", "backup_eligible"} {
		if c.Fields.GetByName(name) != nil {
			t.Errorf("credentials.%s still exists after the down migration", name)
		}
	}
	members, err := app.FindCollectionByNameOrId(store.Members)
	if err != nil {
		t.Fatal(err)
	}
	if members.Fields.GetByName("passkey_user_handle") != nil {
		t.Error("members.passkey_user_handle still exists after the down migration")
	}

	// Rows as an earlier release wrote them.
	member := core.NewRecord(members)
	member.SetRandomPassword()
	member.Set("login_name", "test_upgrade")
	member.Set("display_name", "Test Upgrade")
	member.Set("role", "member")
	if err := app.Save(member); err != nil {
		t.Fatal(err)
	}
	var creds []*core.Record
	for _, id := range []string{"dGVzdC1vbmU", "dGVzdC10d28"} {
		r := core.NewRecord(c)
		r.Set("member", member.Id)
		r.Set("kind", "passkey")
		r.Set("credential_id", id)
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
		creds = append(creds, r)
	}

	if _, err := runner.Up(); err != nil {
		t.Fatal(err)
	}
	checkSchema(t, app)
	seen := map[string]bool{}
	for _, r := range creds {
		fresh, err := app.FindRecordById(store.Credentials, r.Id)
		if err != nil {
			t.Fatal(err)
		}
		u := fresh.GetString(store.FieldUUID)
		if !uuidV7.MatchString(u) || seen[u] {
			t.Errorf("credential uuid %q after the upgrade", u)
		}
		seen[u] = true
	}
}
