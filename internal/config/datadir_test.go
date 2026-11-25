package config

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testConfig(t *testing.T) *Config {
	t.Helper()
	cfg, err := Parse(env(map[string]string{EnvDataDir: filepath.Join(t.TempDir(), "data")}))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func testLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- a file in the test's temporary directory
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not used on Windows")
	}
}

func TestEnsureDirsCreatesLayout(t *testing.T) {
	cfg := testConfig(t)
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{cfg.DataDir, cfg.PBDataDir(), cfg.TilesDir(), cfg.SecretsDir()} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(cfg.SecretsDir())
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("secrets/ has mode %v, want 0700", perm)
		}
	}
	// Running again on an existing layout is fine.
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureDirsFailsWhenDataDirIsAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(env(map[string]string{EnvDataDir: path}))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.EnsureDirs(); err == nil {
		t.Fatal("no error")
	}
}

func TestPrepareGeneratesKeys(t *testing.T) {
	cfg := testConfig(t)
	logger, logs := testLogger()
	keys, err := cfg.Prepare(cfg.PBDataDir(), logger)
	if err != nil {
		t.Fatal(err)
	}
	if !keys.MasterGenerated || !keys.InstanceGenerated {
		t.Errorf("generated = %v, %v; want true, true", keys.MasterGenerated, keys.InstanceGenerated)
	}
	if len(keys.Master) != MasterKeySize {
		t.Errorf("master key has %d bytes", len(keys.Master))
	}
	if bytes.Equal(keys.Master, make([]byte, MasterKeySize)) {
		t.Error("master key is all zeros")
	}
	if len(keys.Instance) != 64 {
		t.Errorf("instance key has %d bytes", len(keys.Instance))
	}

	master := readFile(t, filepath.Join(cfg.SecretsDir(), MasterKeyFile))
	if !bytes.Equal(master, keys.Master) {
		t.Error("master.key differs from the returned key")
	}
	instancePEM := readFile(t, filepath.Join(cfg.SecretsDir(), InstanceKeyFile))
	block, _ := pem.Decode(instancePEM)
	if block == nil || block.Type != "PRIVATE KEY" {
		t.Fatalf("instance_ed25519 is not a PRIVATE KEY PEM block: %q", instancePEM)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !keys.Instance.Equal(parsed) {
		t.Error("instance_ed25519 differs from the returned key")
	}

	if runtime.GOOS != "windows" {
		for _, name := range []string{MasterKeyFile, InstanceKeyFile} {
			info, err := os.Stat(filepath.Join(cfg.SecretsDir(), name))
			if err != nil {
				t.Fatal(err)
			}
			if perm := info.Mode().Perm(); perm != 0o600 {
				t.Errorf("%s has mode %v, want 0600", name, perm)
			}
		}
	}

	out := logs.String()
	if strings.Contains(out, "level=WARN") {
		t.Errorf("warning logged on a fresh data directory:\n%s", out)
	}
	if !strings.Contains(out, "OSFM configuration") {
		t.Errorf("configuration not logged:\n%s", out)
	}
	assertNoKeyMaterial(t, out, keys.Master, instancePEM)
}

func TestPrepareKeepsExistingKeys(t *testing.T) {
	cfg := testConfig(t)
	logger, _ := testLogger()
	first, err := cfg.Prepare(cfg.PBDataDir(), logger)
	if err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(cfg.SecretsDir(), MasterKeyFile)
	instancePath := filepath.Join(cfg.SecretsDir(), InstanceKeyFile)
	masterBefore := readFile(t, masterPath)
	instanceBefore := readFile(t, instancePath)

	logger, logs := testLogger()
	second, err := cfg.Prepare(cfg.PBDataDir(), logger)
	if err != nil {
		t.Fatal(err)
	}
	if second.MasterGenerated || second.InstanceGenerated {
		t.Error("a key was generated although both exist")
	}
	if !bytes.Equal(first.Master, second.Master) || !first.Instance.Equal(second.Instance) {
		t.Error("keys changed between starts")
	}
	masterAfter := readFile(t, masterPath)
	instanceAfter := readFile(t, instancePath)
	if !bytes.Equal(masterBefore, masterAfter) || !bytes.Equal(instanceBefore, instanceAfter) {
		t.Error("key files were rewritten")
	}
	if strings.Contains(logs.String(), "generated") {
		t.Errorf("second start logged a generated key:\n%s", logs.String())
	}
}

func TestPrepareWarnsWhenKeyIsNewButDataExists(t *testing.T) {
	for _, missing := range []string{MasterKeyFile, InstanceKeyFile} {
		t.Run(missing, func(t *testing.T) {
			cfg := testConfig(t)
			logger, _ := testLogger()
			if _, err := cfg.Prepare(cfg.PBDataDir(), logger); err != nil {
				t.Fatal(err)
			}
			// Simulate a started instance whose key went missing, e.g. a restore without secrets/.
			if err := os.WriteFile(filepath.Join(cfg.PBDataDir(), "data.db"), []byte("db"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(cfg.SecretsDir(), missing)); err != nil {
				t.Fatal(err)
			}

			logger, logs := testLogger()
			keys, err := cfg.Prepare(cfg.PBDataDir(), logger)
			if err != nil {
				t.Fatal(err)
			}
			if keys.MasterGenerated != (missing == MasterKeyFile) || keys.InstanceGenerated != (missing == InstanceKeyFile) {
				t.Errorf("generated = %v, %v", keys.MasterGenerated, keys.InstanceGenerated)
			}
			out := logs.String()
			if strings.Count(out, "level=WARN") != 1 || !strings.Contains(out, missing) {
				t.Errorf("want exactly one warning naming %s, got:\n%s", missing, out)
			}
		})
	}
}

func TestPrepareChecksDataInPocketBaseDirOverride(t *testing.T) {
	cfg := testConfig(t)
	// `osfm serve --dir elsewhere`: the existing database is looked for where PocketBase actually keeps it.
	elsewhere := t.TempDir()
	if err := os.WriteFile(filepath.Join(elsewhere, "data.db"), []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	logger, logs := testLogger()
	if _, err := cfg.Prepare(elsewhere, logger); err != nil {
		t.Fatal(err)
	}
	if strings.Count(logs.String(), "level=WARN") != 2 {
		t.Errorf("want two warnings (both keys new, data exists), got:\n%s", logs.String())
	}
}

func TestEnsureKeysRefusesLoosePermissions(t *testing.T) {
	skipOnWindows(t)
	for _, name := range []string{MasterKeyFile, InstanceKeyFile} {
		for _, mode := range []os.FileMode{0o640, 0o604, 0o660, 0o644} {
			cfg := testConfig(t)
			logger, _ := testLogger()
			if _, err := cfg.Prepare(cfg.PBDataDir(), logger); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(cfg.SecretsDir(), name)
			before := readFile(t, path)
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			_, err := EnsureKeys(cfg.SecretsDir())
			if err == nil {
				t.Errorf("%s with mode %v: no error", name, mode)
				continue
			}
			if !strings.Contains(err.Error(), "chmod 600") {
				t.Errorf("%s with mode %v: error %q does not say how to fix it", name, mode, err)
			}
			after := readFile(t, path)
			if !bytes.Equal(before, after) {
				t.Errorf("%s with mode %v: file was changed", name, mode)
			}
		}
	}
}

func TestEnsureKeysRefusesWrongFormat(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecDER, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		t.Fatal(err)
	}
	validInstance, err := generateInstanceKey()
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(validInstance)

	for _, tc := range []struct {
		name, file string
		data       []byte
	}{
		{"empty master key", MasterKeyFile, nil},
		{"short master key", MasterKeyFile, make([]byte, 31)},
		{"long master key", MasterKeyFile, make([]byte, 33)},
		{"hex master key", MasterKeyFile, []byte(strings.Repeat("ab", 32))},
		{"empty instance key", InstanceKeyFile, nil},
		{"not PEM", InstanceKeyFile, []byte("not a key")},
		{"wrong PEM type", InstanceKeyFile, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: block.Bytes})},
		{"PEM with garbage DER", InstanceKeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte{1, 2, 3}})},
		{"ECDSA key", InstanceKeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecDER})},
		{"two PEM blocks", InstanceKeyFile, append(append([]byte{}, validInstance...), validInstance...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(t)
			if err := cfg.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(cfg.SecretsDir(), tc.file)
			if err := os.WriteFile(path, tc.data, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := EnsureKeys(cfg.SecretsDir())
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), tc.file) {
				t.Errorf("error %q does not name the file", err)
			}
			after := readFile(t, path)
			if !bytes.Equal(after, tc.data) {
				t.Error("the invalid key file was overwritten")
			}
		})
	}
}

func TestEnsureKeysRefusesDirectory(t *testing.T) {
	cfg := testConfig(t)
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(cfg.SecretsDir(), MasterKeyFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureKeys(cfg.SecretsDir()); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("err = %v, want a 'not a regular file' error", err)
	}
}

func TestWriteKeyFileDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), MasterKeyFile)
	if err := os.WriteFile(path, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeKeyFile(path, []byte("new")); err == nil {
		t.Fatal("no error")
	}
	got := readFile(t, path)
	if string(got) != "existing" {
		t.Errorf("file content = %q, want it unchanged", got)
	}
}

// assertNoKeyMaterial fails if the log output contains a key in any common encoding.
func assertNoKeyMaterial(t *testing.T, out string, master, instancePEM []byte) {
	t.Helper()
	block, _ := pem.Decode(instancePEM)
	for _, s := range []string{
		string(master),
		strings.TrimSpace(string(instancePEM)),
		strings.Split(string(instancePEM), "\n")[1],
		hex.EncodeToString(master),
	} {
		if s != "" && strings.Contains(out, s) {
			t.Errorf("log output contains key material:\n%s", out)
		}
	}
	if bytes.Contains([]byte(out), block.Bytes) {
		t.Errorf("log output contains the DER-encoded instance key")
	}
}
