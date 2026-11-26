package auth_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// TestNewRecoveryCodes: ten distinct 128-bit codes, stored as hashes only; a new set replaces the old one.
func TestNewRecoveryCodes(t *testing.T) {
	en := newEnv(t)
	member := en.member("test_member", auth.RoleMember)
	other := en.member("test_other", auth.RoleMember)
	if _, err := auth.NewRecoveryCodes(en.app, other); err != nil {
		t.Fatal(err)
	}
	first, err := auth.NewRecoveryCodes(en.app, member)
	if err != nil {
		t.Fatal(err)
	}
	format := regexp.MustCompile(`^[0-9a-f]{4}(-[0-9a-f]{4}){7}$`)
	seen := map[string]bool{}
	for _, c := range first {
		if !format.MatchString(c) || seen[c] {
			t.Errorf("code %q", c)
		}
		seen[c] = true
	}
	second, err := auth.NewRecoveryCodes(en.app, member)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := en.app.FindAllRecords(store.RecoveryCodes, dbx.HashExp{"member": member.Id})
	if err != nil || len(rows) != auth.RecoveryCodeCount {
		t.Fatalf("%d rows, %v", len(rows), err)
	}
	hashes := map[string]bool{}
	for _, r := range rows {
		hashes[r.GetString("code_hash")] = true
	}
	for _, c := range second {
		if !hashes[auth.HashRecoveryCode(c)] {
			t.Errorf("new code %q not stored", c)
		}
	}
	for _, c := range first {
		if hashes[auth.HashRecoveryCode(c)] {
			t.Errorf("old code %q still stored", c)
		}
	}
	if n, _ := en.app.CountRecords(store.RecoveryCodes, dbx.HashExp{"member": other.Id}); n != auth.RecoveryCodeCount {
		t.Errorf("another member's codes: %d", n)
	}
	if auth.HashRecoveryCode(" "+second[0][:4]+" "+second[0][5:]+" ") != auth.HashRecoveryCode(second[0]) {
		t.Error("the hash depends on spaces and dashes")
	}
}

func TestNormalizeNames(t *testing.T) {
	if got, err := auth.NormalizeLoginName("  Test_Name-1 "); err != nil || got != "test_name-1" {
		t.Errorf("login name: %q, %v", got, err)
	}
	if got, err := auth.NormalizeDisplayName("   ", "test_name"); err != nil || got != "test_name" {
		t.Errorf("empty display name: %q, %v", got, err)
	}
	if got, err := auth.NormalizeDisplayName(" 测试 Name ", "x"); err != nil || got != "测试 Name" {
		t.Errorf("display name: %q, %v", got, err)
	}
	for _, bad := range []string{"a\nb", "a\u2066b", strings.Repeat("a", 65)} {
		if _, err := auth.NormalizeDisplayName(bad, "x"); err == nil {
			t.Errorf("display name %q accepted", bad)
		}
	}
}
