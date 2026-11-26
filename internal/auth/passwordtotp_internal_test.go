package auth

import (
	"context"
	"encoding/base32"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestPasswordHash(t *testing.T) {
	ctx := context.Background()
	h, err := hashPassword(ctx, "test password 1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=256,t=1,p=1$") {
		t.Errorf("hash %q", h)
	}
	h2, _ := hashPassword(ctx, "test password 1")
	if h == h2 {
		t.Error("two hashes of the same password are equal: the salt is not random")
	}
	for pw, want := range map[string]bool{"test password 1": true, "test password 2": false, "": false} {
		if ok, err := verifyPassword(ctx, h, pw); err != nil || ok != want {
			t.Errorf("verify %q: %v, %v", pw, ok, err)
		}
	}
	// NFKC: the full-width form of a password matches the ASCII one; composed and decomposed forms match.
	h, _ = hashPassword(ctx, "test-password")
	if ok, _ := verifyPassword(ctx, h, "ｔｅｓｔ－ｐａｓｓｗｏｒｄ"); !ok {
		t.Error("the full-width form does not match")
	}
	h, _ = hashPassword(ctx, "café-password")
	if ok, _ := verifyPassword(ctx, h, "café-password"); !ok {
		t.Error("the decomposed form does not match")
	}
	// A hash with the production parameters verifies too: each hash carries its parameters.
	old := passwordParams
	passwordParams = PasswordParams{Memory: 1024, Time: 2, Threads: 2}
	h, _ = hashPassword(ctx, "test password 3")
	passwordParams = old
	if ok, err := verifyPassword(ctx, h, "test password 3"); !ok || err != nil {
		t.Errorf("hash with other parameters: %v, %v", ok, err)
	}
}

func TestParsePasswordHashBounds(t *testing.T) {
	salt, sum := "c2FsdHNhbHRzYWx0c2FsdA", "aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g"
	for _, h := range []string{
		"",
		"$argon2i$v=19$m=256,t=1,p=1$" + salt + "$" + sum,
		"$argon2id$v=16$m=256,t=1,p=1$" + salt + "$" + sum,
		"$argon2id$v=19$t=1,m=256,p=1$" + salt + "$" + sum,
		"$argon2id$v=19$m=4,t=1,p=1$" + salt + "$" + sum,
		"$argon2id$v=19$m=9999999,t=1,p=1$" + salt + "$" + sum,
		"$argon2id$v=19$m=256,t=0,p=1$" + salt + "$" + sum,
		"$argon2id$v=19$m=256,t=1,p=99$" + salt + "$" + sum,
		"$argon2id$v=19$m=256,t=1,p=1$c2FsdA$" + sum,
		"$argon2id$v=19$m=256,t=1,p=1$" + salt + "$aGFzaA",
		"$argon2id$v=19$m=256,t=1,p=1$" + salt + "$" + sum + "$x",
	} {
		if _, _, _, err := parsePasswordHash(h); err == nil {
			t.Errorf("accepted %q", h)
		}
	}
	if _, _, _, err := parsePasswordHash("$argon2id$v=19$m=256,t=1,p=1$" + salt + "$" + sum); err != nil {
		t.Error(err)
	}
}

func TestMatchTOTP(t *testing.T) {
	secret := []byte("12345678901234567890") // RFC 6238 test secret, made up
	s := base32.StdEncoding.EncodeToString(secret)
	t0 := time.Unix(1_800_000_015, 0)
	step := totpStep(t0)
	code := func(at time.Time) string {
		c, err := totp.GenerateCode(s, at)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	for _, tc := range []struct {
		at    time.Time
		after int64
		want  int64
		ok    bool
	}{
		{t0, 0, step, true},
		{t0.Add(-30 * time.Second), 0, step - 1, true}, // one step behind
		{t0.Add(30 * time.Second), 0, step + 1, true},  // one step ahead
		{t0.Add(-60 * time.Second), 0, 0, false},       // two steps behind
		{t0.Add(60 * time.Second), 0, 0, false},        // two steps ahead
		{t0, step, 0, false},                           // used already
		{t0.Add(-30 * time.Second), step, 0, false},    // older than the last used step
	} {
		got, ok := matchTOTP(secret, code(tc.at), t0, tc.after)
		if ok != tc.ok || got != tc.want {
			t.Errorf("code of %v, after %d: %d, %v; want %d, %v", tc.at, tc.after, got, ok, tc.want, tc.ok)
		}
	}
	c := code(t0)
	if _, ok := matchTOTP(secret, c[:3]+" "+c[3:], t0, 0); !ok {
		t.Error("a code with a space is refused")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef", "12345a"} {
		if _, ok := matchTOTP(secret, bad, t0, 0); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestRecoveryCodeForm(t *testing.T) {
	code := "0123456789ABCDEF"
	if d := displayRecoveryCode(code); d != "0123-4567-89AB-CDEF" {
		t.Errorf("display %q", d)
	}
	for in, want := range map[string]string{
		"0123-4567-89AB-CDEF":  code,
		"0123 4567 89ab cdef":  code,
		"o123-4567-89ab-cdef":  code, // O is 0
		"0i23-4567-89ab-cdef":  "0123456789ABCDEF",
		"0l23-4567-89ab-cdef":  "0123456789ABCDEF",
		" 0123456789abcdef\t ": code,
	} {
		got, ok := normalizeRecoveryCode(in)
		if !ok || got != want {
			t.Errorf("%q: %q, %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "0123-4567-89AB-CDE", "0123-4567-89AB-CDEFG", "0123-4567-89AB-CDEU",
		"0123-4567-89AB-CDE!", "０123-4567-89AB-CDEF"} {
		if _, ok := normalizeRecoveryCode(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	// Generated codes have the canonical form.
	b := make([]byte, recoveryCodeBytes)
	if c := crockford.EncodeToString(b); len(c) != recoveryCodeLength {
		t.Errorf("code length %d", len(c))
	}
}

func TestLoginAndDisplayNames(t *testing.T) {
	for in, ok := range map[string]bool{"test_member": true, "abc": true, "ab": false, "a-b_9": true,
		strings.Repeat("a", 32): true, strings.Repeat("a", 33): false, "test member": false, "测试": false,
		"test.member": false} {
		if validLoginName(NormalizeLoginName(in)) != ok {
			t.Errorf("login name %q: want %v", in, ok)
		}
	}
	if NormalizeLoginName("  Test_Member ") != "test_member" {
		t.Error("login names are not trimmed and lowercased")
	}
	if n, ok := normalizeDisplayName("  ", "test_member"); !ok || n != "test_member" {
		t.Errorf("empty display name: %q, %v", n, ok)
	}
	if n, ok := normalizeDisplayName(" 测试 成员 ", "x"); !ok || n != "测试 成员" {
		t.Errorf("display name: %q, %v", n, ok)
	}
	for _, bad := range []string{strings.Repeat("名", 65), "a\nb", "a\u200eb", "a\x00b"} {
		if _, ok := normalizeDisplayName(bad, "x"); ok {
			t.Errorf("accepted display name %q", bad)
		}
	}
}
