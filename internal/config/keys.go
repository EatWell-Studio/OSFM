package config

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// Names of the key files in the secrets directory.
const (
	MasterKeyFile   = "master.key"
	InstanceKeyFile = "instance_ed25519"
)

// MasterKeySize is the size of the master key in bytes (AES-256).
const MasterKeySize = 32

const keyFileMode fs.FileMode = 0o600

// Keys holds the instance keys.
type Keys struct {
	// Master is the instance master key, MasterKeySize random bytes used with AES-256-GCM.
	Master []byte
	// Instance is the instance signing key.
	Instance ed25519.PrivateKey

	// MasterGenerated and InstanceGenerated report whether the key was created by this call to [EnsureKeys].
	MasterGenerated   bool
	InstanceGenerated bool
}

// EnsureKeys loads the keys from secretsDir, generating each key file that does not exist. An existing key file is
// never overwritten: if it has the wrong size or format, or is accessible to group or others, an error is returned.
func EnsureKeys(secretsDir string) (*Keys, error) {
	keys := &Keys{}
	var err error

	keys.Master, keys.MasterGenerated, err = ensureKeyFile(
		filepath.Join(secretsDir, MasterKeyFile), generateMasterKey, parseMasterKey)
	if err != nil {
		return nil, err
	}

	keys.Instance, keys.InstanceGenerated, err = ensureKeyFile(
		filepath.Join(secretsDir, InstanceKeyFile), generateInstanceKey, parseInstanceKey)
	if err != nil {
		return nil, err
	}

	return keys, nil
}

// ReadKeys loads and checks the keys in secretsDir without generating any: both key files must exist, be readable by
// their owner only and hold a valid key. Backups use it to check the keys they copy, and restores to check the keys they
// put back.
func ReadKeys(secretsDir string) (*Keys, error) {
	keys := &Keys{}
	var err error
	if keys.Master, err = readKey(filepath.Join(secretsDir, MasterKeyFile), parseMasterKey); err != nil {
		return nil, err
	}
	if keys.Instance, err = readKey(filepath.Join(secretsDir, InstanceKeyFile), parseInstanceKey); err != nil {
		return nil, err
	}
	return keys, nil
}

func readKey[K any](path string, parse func([]byte) (K, error)) (K, error) {
	var zero K
	data, err := readKeyFile(path)
	if err != nil {
		return zero, err
	}
	key, err := parse(data)
	if err != nil {
		return zero, fmt.Errorf("key file %s: %w", path, err)
	}
	return key, nil
}

// ensureKeyFile reads and parses the key at path, or, if the file does not exist, generates the key, writes it and
// parses what was written.
func ensureKeyFile[K any](path string, generate func() ([]byte, error), parse func([]byte) (K, error)) (K, bool, error) {
	var zero K

	data, err := readKeyFile(path)
	generated := false
	if errors.Is(err, fs.ErrNotExist) {
		if data, err = generate(); err != nil {
			return zero, false, fmt.Errorf("generate %s: %w", path, err)
		}
		if err = writeKeyFile(path, data); err != nil {
			return zero, false, err
		}
		generated = true
	} else if err != nil {
		return zero, false, err
	}

	key, err := parse(data)
	if err != nil {
		return zero, false, fmt.Errorf("key file %s: %w; it is never replaced automatically: restore it from a backup, "+
			"or remove it to generate a new key (data protected by the old key is then lost)", path, err)
	}
	return key, generated, nil
}

// readKeyFile reads an existing key file after checking that it is a regular file readable by its owner only.
func readKeyFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("key file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("key file %s: not a regular file", path)
	}
	// Windows has no Unix permission bits; the file's ACL governs access there.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("key file %s: permissions %v allow access by group or others; run: chmod 600 %s",
			path, info.Mode().Perm(), path)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the path is the data directory chosen by the operator
	if err != nil {
		return nil, fmt.Errorf("key file %s: %w", path, err)
	}
	return data, nil
}

// writeKeyFile creates path with mode 0600 and writes data to it. It fails if the file already exists, so a key is
// never overwritten, not even by a concurrent start.
func writeKeyFile(path string, data []byte) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, keyFileMode) // #nosec G304 -- see readKeyFile
	if err != nil {
		return fmt.Errorf("create key file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write key file %s: %w", path, err)
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("write key file %s: %w", path, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("write key file %s: %w", path, err)
	}
	syncDir(filepath.Dir(path))
	return nil
}

// syncDir makes a newly created directory entry durable where the platform supports it; failures are ignored.
func syncDir(dir string) {
	d, err := os.Open(dir) // #nosec G304 -- see readKeyFile
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

func generateMasterKey() ([]byte, error) {
	key := make([]byte, MasterKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

func parseMasterKey(data []byte) ([]byte, error) {
	if len(data) != MasterKeySize {
		return nil, fmt.Errorf("expected %d bytes, found %d", MasterKeySize, len(data))
	}
	return data, nil
}

const pemTypePrivateKey = "PRIVATE KEY"

func generateInstanceKey() ([]byte, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: pemTypePrivateKey, Bytes: der}), nil
}

func parseInstanceKey(data []byte) (ed25519.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != pemTypePrivateKey || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("expected one PEM block of type " + pemTypePrivateKey + " (PKCS #8)")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not a PKCS #8 private key: %w", err)
	}
	edKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("expected an Ed25519 key, found %T", key)
	}
	return edKey, nil
}
