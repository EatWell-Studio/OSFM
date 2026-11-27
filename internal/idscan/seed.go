package idscan

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Seeded is the made-up data [Seed] writes.
type Seeded struct {
	// IDs holds the internal identifiers no response may contain: the PocketBase record ID of every seeded record
	// and the ID of every OSFM collection.
	IDs []string
	// Records holds the seeded records by collection name.
	Records map[string][]*core.Record
	// Superuser is a seeded superuser (its ID is in IDs too).
	Superuser *core.Record
	// Member and Maintainer are seeded members who may sign in, with role member and maintainer.
	Member, Maintainer *core.Record
}

// Made-up invites [Seed] writes, for the probes of the invite routes.
const (
	// InviteCode is the code of the seeded active invite.
	InviteCode = "dGVzdC1pbnZpdGUtY29kZS1ub3QtYS1yZWFsLW9uZSE" // #nosec G101 -- made up
	// ActiveInviteUUID, UsedInviteUUID and ExpiredInviteUUID are the uuids of seeded invites in those states.
	ActiveInviteUUID  = "0190a1b2-0000-7000-8000-00000000a001"
	UsedInviteUUID    = "0190a1b2-0000-7000-8000-00000000a002"
	ExpiredInviteUUID = "0190a1b2-0000-7000-8000-00000000a003"
)

// Made-up credentials and reset links [Seed] writes, for the probes of the credential and reset routes.
const (
	// MemberPasskeyUUID is the uuid of the seeded member's only passkey.
	MemberPasskeyUUID = "0190a1b2-0000-7000-8000-00000000c001"
	// RecoveryCode is an unused recovery code of the seeded member.
	RecoveryCode = "0000-0000-0000-0001" // #nosec G101 -- made up
	// ResetToken is the token of the seeded member's active reset link.
	ResetToken = "dGVzdC1yZXNldC10b2tlbi1ub3QtYS1yZWFsLW9uZSE" // #nosec G101 -- made up
	// ActiveResetLinkUUID, UsedResetLinkUUID and ExpiredResetLinkUUID are the uuids of seeded reset links.
	ActiveResetLinkUUID  = "0190a1b2-0000-7000-8000-00000000b001"
	UsedResetLinkUUID    = "0190a1b2-0000-7000-8000-00000000b002"
	ExpiredResetLinkUUID = "0190a1b2-0000-7000-8000-00000000b003"
	// ResetMemberUUID is the uuid of a seeded member that maintainers may create reset links for.
	ResetMemberUUID = "0190a1b2-0000-7000-8000-00000000d001"
	// MaintainerUUID is the uuid of the seeded maintainer, LeftMemberUUID of a seeded member who left the circle.
	MaintainerUUID = "0190a1b2-0000-7000-8000-00000000d002"
	LeftMemberUUID = "0190a1b2-0000-7000-8000-00000000d003"
)

// Seed writes made-up records into every OSFM collection of app, a bootstrapped app with the OSFM migrations
// applied, including soft-deleted ones, and returns their internal IDs. It fails the test if a collection that is
// not one of PocketBase's system collections is left without a record: a task that adds a collection adds its records
// here, so that the scan covers it.
func Seed(t testing.TB, app core.App) *Seeded {
	t.Helper()
	s := &Seeded{Records: map[string][]*core.Record{}}
	save := func(collection string, values map[string]any) *core.Record {
		t.Helper()
		c, err := app.FindCollectionByNameOrId(collection)
		if err != nil {
			t.Fatal(err)
		}
		r := core.NewRecord(c)
		for k, v := range values {
			r.Set(k, v)
		}
		if c.IsAuth() {
			r.SetRandomPassword()
		}
		if err := app.Save(r); err != nil {
			t.Fatalf("seed %s: %v", collection, err)
		}
		s.Records[collection] = append(s.Records[collection], r)
		s.IDs = append(s.IDs, r.Id)
		return r
	}
	deleted := func(r *core.Record) {
		t.Helper()
		if err := store.SoftDelete(app, r); err != nil {
			t.Fatal(err)
		}
	}

	maintainer := save(store.Members, map[string]any{store.FieldUUID: MaintainerUUID, "login_name": "test_maintainer",
		"display_name": "Test Maintainer", "role": "maintainer", "can_invite": true,
		"passkey_user_handle": "dGVzdC1tYWludGFpbmVyLWhhbmRsZS1ub3QtcmVhbCE"}) // #nosec G101 -- made up
	member := save(store.Members, map[string]any{"login_name": "test_member", "display_name": "Test Member", // #nosec G101 -- made up
		"pseudonym": "Test Alias", "role": "member", "invited_by": maintainer.Id,
		"passkey_user_handle": "dGVzdC11c2VyLWhhbmRsZS1ub3QtYS1yZWFsLW9uZSE"})
	save(store.Members, map[string]any{store.FieldUUID: ResetMemberUUID, "login_name": "test_reset",
		"display_name": "Test Reset", "role": "member", "invited_by": maintainer.Id})
	save(store.Members, map[string]any{store.FieldUUID: LeftMemberUUID, "login_name": "test_gone_away",
		"display_name": "Test Gone Away", "role": "member", "invited_by": maintainer.Id, "left_at": "2026-01-03 00:00:00Z"})
	left := save(store.Members, map[string]any{"login_name": "test_left", "display_name": "Test Left",
		"role": "member", "invited_by": maintainer.Id})
	deleted(left)
	s.Member, s.Maintainer = member, maintainer
	m := member.Id

	save(store.Credentials, map[string]any{store.FieldUUID: MemberPasskeyUUID, "member": m, "kind": "passkey",
		"credential_id": "dGVzdC1jcmVk", // #nosec G101 -- made up
		"public_key":    "dGVzdC1rZXk", "sign_count": 3, "aaguid": "00000000-0000-0000-0000-000000000000",
		"transports": []string{"internal"}, "user_verified": true, "attestation_format": "none",
		"device_name": "Test Phone", "last_used_at": "2026-01-01 00:00:00Z"})
	save(store.Credentials, map[string]any{"member": maintainer.Id, "kind": "passkey", // #nosec G101 -- made up
		"credential_id": "dGVzdC1jcmVkLW1haW50YWluZXI", "public_key": "dGVzdC1rZXk", "user_verified": true})
	save(store.RecoveryCodes, map[string]any{"member": m, "code_hash": "test-hash-recovery"})
	recoveryHash := sha256.Sum256([]byte(strings.ReplaceAll(RecoveryCode, "-", "")))
	save(store.RecoveryCodes, map[string]any{"member": m, "code_hash": hex.EncodeToString(recoveryHash[:])})
	// Invites in every state, with fixed uuids for the probes: used (by the member), active (its code is
	// InviteCode), expired and revoked.
	save(store.Invites, map[string]any{store.FieldUUID: UsedInviteUUID, "code_hash": strings.Repeat("1a", 32),
		"created_by": maintainer.Id, "used_by": m, "used_at": "2026-01-02 00:00:00Z", "expires_at": "2026-01-08 00:00:00Z"})
	inviteHash := sha256.Sum256([]byte(InviteCode))
	save(store.Invites, map[string]any{store.FieldUUID: ActiveInviteUUID, "code_hash": hex.EncodeToString(inviteHash[:]),
		"created_by": maintainer.Id, "expires_at": "2099-01-01 00:00:00Z"})
	save(store.Invites, map[string]any{store.FieldUUID: ExpiredInviteUUID, "code_hash": strings.Repeat("1b", 32),
		"created_by": maintainer.Id, "expires_at": "2020-01-01 00:00:00Z"})
	save(store.Invites, map[string]any{"code_hash": strings.Repeat("1c", 32), "created_by": maintainer.Id,
		"expires_at": "2099-01-01 00:00:00Z", "revoked_at": "2026-01-02 00:00:00Z"})
	resetHash := sha256.Sum256([]byte(ResetToken))
	save(store.ResetLinks, map[string]any{store.FieldUUID: ActiveResetLinkUUID, "code_hash": hex.EncodeToString(resetHash[:]),
		"member": m, "created_by": maintainer.Id, "expires_at": "2099-01-01 00:00:00Z"})
	save(store.ResetLinks, map[string]any{store.FieldUUID: UsedResetLinkUUID, "code_hash": "test-hash-reset-used",
		"member": m, "created_by": maintainer.Id, "expires_at": "2026-01-02 00:00:00Z", "used_at": "2026-01-01 12:00:00Z"})
	save(store.ResetLinks, map[string]any{store.FieldUUID: ExpiredResetLinkUUID, "code_hash": "test-hash-reset-expired",
		"member": m, "created_by": maintainer.Id, "expires_at": "2020-01-01 00:00:00Z"})
	save(store.AuditLog, map[string]any{"actor": maintainer.Id, "action": "test.seed"})
	// Sessions as stored: a hash of a token nobody holds, one live and one expired. The scan's member and maintainer
	// identities sign in with sessions of their own.
	save(store.Sessions, map[string]any{"member": m, "token_hash": strings.Repeat("0a", 32),
		"expires_at": "2030-01-01 00:00:00Z", "last_seen_at": "2026-01-01 00:00:00Z"})
	save(store.Sessions, map[string]any{"member": maintainer.Id, "token_hash": strings.Repeat("0b", 32),
		"expires_at": "2020-01-01 00:00:00Z"})

	place := save(store.Places, map[string]any{"name": "Test Noodle Bar", "lat": 31.23, "lon": 121.47,
		"precision": "exact", "category": "restaurant", "status": "open"})
	gone := save(store.Places, map[string]any{"name": "Test Closed Cafe", "lat": 31.24, "lon": 121.48,
		"precision": "approx", "category": "cafe", "status": "closed"})
	deleted(gone)
	rec := save(store.Recommendations, map[string]any{"place": place.Id, "author": m, "display_as": "pseudonym",
		"attitude": "good", "visibility": "circle", "text": "Test text"})
	private := save(store.Recommendations, map[string]any{"place": place.Id, "author": maintainer.Id,
		"display_as": "main", "attitude": "ok", "visibility": "private", "text": "Test private text"})
	deleted(private)
	save(store.RecommendationRevisions, map[string]any{"recommendation": rec.Id, "editor_role": "author",
		"editor": m, "snapshot": map[string]any{"text": "Test old text"}})
	save(store.Reports, map[string]any{"recommendation": rec.Id, "reporter": maintainer.Id, "reason": "test"})
	save(store.Notices, map[string]any{"member": m, "kind": "test"})

	// The single settings row exists from the migration; give it a made-up name and city.
	settings, err := app.FindRecordById(store.InstanceSettings, store.SettingsID)
	if err != nil {
		t.Fatal(err)
	}
	settings.Set("name", "Test Circle")
	settings.Set("cities", []map[string]any{{"slug": "testcity", "name": "Test City",
		"bbox": []float64{121.3, 31.1, 121.6, 31.4}, "center": []float64{121.47, 31.23}}})
	if err := app.Save(settings); err != nil {
		t.Fatal(err)
	}
	s.Records[store.InstanceSettings] = []*core.Record{settings}
	s.IDs = append(s.IDs, settings.Id)

	s.Superuser = save(core.CollectionNameSuperusers, map[string]any{"email": "superuser@example.test"})

	collections, err := app.FindAllCollections()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range collections {
		if c.System {
			continue
		}
		s.IDs = append(s.IDs, c.Id)
		if len(s.Records[c.Name]) == 0 {
			t.Errorf("idscan.Seed writes no record into collection %s: add made-up records for it", c.Name)
		}
	}
	return s
}
