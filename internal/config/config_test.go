package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	}
}

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs("data")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != want {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
	}
	if cfg.Listen != DefaultListen {
		t.Errorf("Listen = %q, want %q", cfg.Listen, DefaultListen)
	}
	if cfg.PublicURL != "" {
		t.Errorf("PublicURL = %q, want empty", cfg.PublicURL)
	}
	if cfg.AdminUI {
		t.Error("AdminUI = true, want false")
	}
	if cfg.AdminListen != DefaultAdminListen {
		t.Errorf("AdminListen = %q, want %q", cfg.AdminListen, DefaultAdminListen)
	}
}

func TestParseValues(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Parse(env(map[string]string{
		EnvDataDir:     dir,
		EnvListen:      " 0.0.0.0:8090 ",
		EnvPublicURL:   "https://food.example.org/",
		EnvAdminUI:     "true",
		EnvAdminListen: "0.0.0.0:9091",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != dir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, dir)
	}
	if cfg.Listen != "0.0.0.0:8090" {
		t.Errorf("Listen = %q", cfg.Listen)
	}
	if cfg.PublicURL != "https://food.example.org" {
		t.Errorf("PublicURL = %q", cfg.PublicURL)
	}
	if !cfg.AdminUI {
		t.Error("AdminUI = false, want true")
	}
	if cfg.AdminListen != "0.0.0.0:9091" {
		t.Errorf("AdminListen = %q", cfg.AdminListen)
	}
	if got, want := cfg.PBDataDir(), filepath.Join(dir, "pb_data"); got != want {
		t.Errorf("PBDataDir = %q, want %q", got, want)
	}
	if got, want := cfg.TilesDir(), filepath.Join(dir, "tiles"); got != want {
		t.Errorf("TilesDir = %q, want %q", got, want)
	}
	if got, want := cfg.SecretsDir(), filepath.Join(dir, "secrets"); got != want {
		t.Errorf("SecretsDir = %q, want %q", got, want)
	}
}

func TestParseRelativeDataDirIsMadeAbsolute(t *testing.T) {
	cfg, err := Parse(env(map[string]string{EnvDataDir: "some/dir"}))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(cfg.DataDir) || !strings.HasSuffix(cfg.DataDir, filepath.Join("some", "dir")) {
		t.Errorf("DataDir = %q, want an absolute path ending in some/dir", cfg.DataDir)
	}
}

func TestParseValidPublicURLs(t *testing.T) {
	for raw, want := range map[string]string{
		"http://localhost:8090":          "http://localhost:8090",
		"http://127.0.0.1:8090/":         "http://127.0.0.1:8090",
		"http://[::1]:8090":              "http://[::1]:8090",
		"http://osfm.localhost":          "http://osfm.localhost",
		"https://example.org/osfm/":      "https://example.org/osfm",
		"HTTPS://example.org":            "https://example.org",
		"https://[2001:db8::1]:8443/map": "https://[2001:db8::1]:8443/map",
	} {
		cfg, err := Parse(env(map[string]string{EnvPublicURL: raw}))
		if err != nil {
			t.Errorf("%q: %v", raw, err)
			continue
		}
		if cfg.PublicURL != want {
			t.Errorf("%q: PublicURL = %q, want %q", raw, cfg.PublicURL, want)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	for _, tc := range []struct {
		name, value string
	}{
		{EnvListen, "8090"},
		{EnvListen, "localhost"},
		{EnvListen, "127.0.0.1:http"},
		{EnvListen, "127.0.0.1:0"},
		{EnvListen, "127.0.0.1:65536"},
		{EnvPublicURL, "example.org"},
		{EnvPublicURL, "/osfm"},
		{EnvPublicURL, "ftp://example.org"},
		{EnvPublicURL, "https://"},
		{EnvPublicURL, "https:example.org"},
		{EnvPublicURL, "https://user:secret@example.org"},
		{EnvPublicURL, "https://example.org/?x=1"},
		{EnvPublicURL, "https://example.org/#top"},
		{EnvPublicURL, "https://exa mple.org"},
		{EnvPublicURL, "http://example.org"}, // the Secure session cookie needs HTTPS
		{EnvPublicURL, "http://192.0.2.10:8090"},
		{EnvPublicURL, "http://localhost.example.org"},
		{EnvAdminUI, "yes"},
		{EnvAdminUI, "2"},
		{EnvAdminListen, "8091"},
		{EnvAdminListen, "127.0.0.1:0"},
	} {
		_, err := Parse(env(map[string]string{tc.name: tc.value}))
		if err == nil {
			t.Errorf("%s=%q: no error", tc.name, tc.value)
			continue
		}
		if !strings.Contains(err.Error(), tc.name) {
			t.Errorf("%s=%q: error %q does not name the variable", tc.name, tc.value, err)
		}
	}
}

func TestParseReportsAllErrors(t *testing.T) {
	_, err := Parse(env(map[string]string{EnvListen: "nope", EnvPublicURL: "nope", EnvAdminUI: "nope"}))
	if err == nil {
		t.Fatal("no error")
	}
	for _, name := range []string{EnvListen, EnvPublicURL, EnvAdminUI} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not mention %s", err, name)
		}
	}
}

func TestParseBooleans(t *testing.T) {
	for raw, want := range map[string]bool{"1": true, "TRUE": true, "false": false, "0": false, "": false, "  ": false} {
		cfg, err := Parse(env(map[string]string{EnvAdminUI: raw}))
		if err != nil {
			t.Errorf("%q: %v", raw, err)
			continue
		}
		if cfg.AdminUI != want {
			t.Errorf("%q: AdminUI = %v, want %v", raw, cfg.AdminUI, want)
		}
	}
}

func TestHTTPAddr(t *testing.T) {
	def, err := Parse(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := def.HTTPAddr(false); got != DefaultListen {
		t.Errorf("default, no domains: %q, want %q", got, DefaultListen)
	}
	if got := def.HTTPAddr(true); got != "" {
		t.Errorf("default, with domains: %q, want empty (PocketBase's default)", got)
	}

	set, err := Parse(env(map[string]string{EnvListen: "0.0.0.0:8090"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, withDomains := range []bool{false, true} {
		if got := set.HTTPAddr(withDomains); got != "0.0.0.0:8090" {
			t.Errorf("OSFM_LISTEN set, withDomains=%v: %q", withDomains, got)
		}
	}
}

func TestPublicHTTPS(t *testing.T) {
	for raw, want := range map[string]bool{
		"":                          false,
		"https://food.example.org":  true,
		"https://food.example.org/": true,
		"http://localhost:8090":     false,
		"http://127.0.0.1:8090":     false,
		"https://localhost:8443":    false, // HSTS and the cookie prefix would follow the developer to every local app
		"https://osfm.localhost":    false,
		"https://[::1]:8443":        false,
		"https://192.0.2.10":        true,
	} {
		cfg, err := Parse(env(map[string]string{EnvPublicURL: raw}))
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if got := cfg.PublicHTTPS(); got != want {
			t.Errorf("%q: PublicHTTPS() = %v, want %v", raw, got, want)
		}
	}
}

func TestPasskeyRP(t *testing.T) {
	for _, tc := range []struct {
		publicURL, listen string
		id                string
		origins           []string
	}{
		{"https://food.example.org", "", "food.example.org", []string{"https://food.example.org"}},
		{"https://Food.Example.org:443/", "", "food.example.org", []string{"https://food.example.org"}},
		{"https://food.example.org:8443", "", "food.example.org", []string{"https://food.example.org:8443"}},
		{"http://localhost:8090", "", "localhost", []string{"http://localhost:8090", DevServerOrigin}},
		{"http://127.0.0.1:8090", "", "localhost", []string{"http://localhost:8090", DevServerOrigin}},
		{"http://[::1]:8090", "", "localhost", []string{"http://localhost:8090", DevServerOrigin}},
		{"http://localhost:5173", "", "localhost", []string{DevServerOrigin}},
		{"https://localhost", "", "localhost", []string{"https://localhost", DevServerOrigin}},
		{"http://osfm.localhost:8090", "", "osfm.localhost", []string{"http://osfm.localhost:8090"}},
		{"", "", "localhost", []string{"http://localhost:8090", DevServerOrigin}},
		{"", "0.0.0.0:9000", "localhost", []string{"http://localhost:9000", DevServerOrigin}},
	} {
		vars := map[string]string{EnvPublicURL: tc.publicURL}
		if tc.listen != "" {
			vars[EnvListen] = tc.listen
		}
		cfg, err := Parse(env(vars))
		if err != nil {
			t.Fatalf("%q: %v", tc.publicURL, err)
		}
		id, origins := cfg.PasskeyRP()
		if id != tc.id || !slices.Equal(origins, tc.origins) {
			t.Errorf("%q, listen %q: PasskeyRP() = %q, %q; want %q, %q", tc.publicURL, tc.listen, id, origins, tc.id,
				tc.origins)
		}
	}
}
