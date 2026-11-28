package store_test

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

func TestNameKey(t *testing.T) {
	for _, tc := range []struct{ a, b string }{
		{"Test Name", "test name"},
		{"TEST NAME", "test name"},
		{"Ｔｅｓｔ\u3000Ｎａｍｅ", "test name"},   // full-width letters and ideographic space
		{"  Test   Name \t", "test name"}, // padding and runs of white space
		{"Test\u200bName", "testname"},    // zero-width space
		{"Te\u00adst", "test"},            // soft hyphen
		{"Straße", "strasse"},             // full case folding
		{"ﬁsh", "fish"},                   // compatibility ligature
		{"ｶﾞ", "ガ"},                       // half-width katakana with its voiced mark
		{"\u212bngström", "ångström"},     // the angstrom sign is the letter å
		{"测试 名字", "测试 名字"},
		{"   ", ""},
		{"", ""},
	} {
		if got := store.NameKey(tc.a); got != tc.b {
			t.Errorf("NameKey(%q) = %q, want %q", tc.a, got, tc.b)
		}
	}
	if store.NameKey("Test Name") == store.NameKey("Test Nam") {
		t.Error("different names have the same key")
	}
}

// TestMemberNameKeysFollowTheNames: the hooks set the name keys on create and on every update, whatever the caller
// sets them to.
func TestMemberNameKeysFollowTheNames(t *testing.T) {
	app := newApp(t)
	c, err := app.FindCollectionByNameOrId(store.Members)
	if err != nil {
		t.Fatal(err)
	}
	r := core.NewRecord(c)
	r.SetRandomPassword()
	r.Set("login_name", "test_keys")
	r.Set("display_name", "Test KEYS")
	r.Set(store.FieldDisplayNameKey, "something else")
	r.Set("role", "member")
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
	check := func(display, pseudonym string) {
		t.Helper()
		fresh, err := app.FindRecordById(store.Members, r.Id)
		if err != nil {
			t.Fatal(err)
		}
		if got := fresh.GetString(store.FieldDisplayNameKey); got != display {
			t.Errorf("display_name_key %q, want %q", got, display)
		}
		if got := fresh.GetString(store.FieldPseudonymKey); got != pseudonym {
			t.Errorf("pseudonym_key %q, want %q", got, pseudonym)
		}
	}
	check("test keys", "")

	r.Set("pseudonym", "Ｔｅｓｔ Pen")
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
	check("test keys", "test pen")

	r.Set("display_name", "Test Renamed")
	r.Set("pseudonym", "")
	r.Set(store.FieldPseudonymKey, "stale")
	if err := app.SaveNoValidate(r); err != nil {
		t.Fatal(err)
	}
	check("test renamed", "")
}

func TestNormalizeName(t *testing.T) {
	for in, want := range map[string]string{"  Test Name ": "Test Name", "测试 名字": "测试 名字"} {
		if got, ok := store.NormalizeName(in); !ok || got != want {
			t.Errorf("NormalizeName(%q) = %q, %v", in, got, ok)
		}
	}
	long := strings.Repeat("名", store.MaxNameLength)
	if _, ok := store.NormalizeName(long); !ok {
		t.Error("a name of MaxNameLength characters is refused")
	}
	// Control and format characters, a variation selector alone, a Hangul filler alone, invalid UTF-8.
	r := func(c rune) string { return string(c) }
	for _, bad := range []string{"", "   ", long + "x", "Test" + r(0x0a) + "Name", "Test" + r(0x200b) + "Name",
		"Test" + r(0x202e) + "Name", r(0xfe0f), r(0x3164), string([]byte{0xff})} {
		if _, ok := store.NormalizeName(bad); ok {
			t.Errorf("NormalizeName(%q) accepted", bad)
		}
	}
}
