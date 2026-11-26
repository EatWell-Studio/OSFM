// Package config loads the instance configuration from the OSFM_* environment variables and prepares the data
// directory: its subdirectories and the instance keys in secrets/.
//
// Data directory layout (OSFM_DATA_DIR, default ./data):
//
//	<data>/pb_data/   the PocketBase data directory (SQLite databases, PocketBase backups)
//	<data>/tiles/     one PMTiles file per city
//	<data>/secrets/   master.key (AES-256-GCM key) and instance_ed25519 (instance signing key)
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Environment variables read by [Load].
const (
	EnvDataDir   = "OSFM_DATA_DIR"
	EnvListen    = "OSFM_LISTEN"
	EnvPublicURL = "OSFM_PUBLIC_URL"
	EnvAdminUI   = "OSFM_ADMIN_UI"
	// EnvAdminListen is the address of the separate admin listener, used only when OSFM_ADMIN_UI is on.
	EnvAdminListen = "OSFM_ADMIN_LISTEN"
)

// Defaults for unset environment variables.
const (
	// DefaultDataDir is relative to the working directory. The container image sets OSFM_DATA_DIR=/data.
	DefaultDataDir = "./data"
	// DefaultListen keeps a local run private to the machine. The container image sets OSFM_LISTEN=0.0.0.0:8090.
	DefaultListen = "127.0.0.1:8090"
	// DefaultAdminListen is loopback-only: the admin UI is reached through an SSH tunnel, never through the reverse
	// proxy.
	DefaultAdminListen = "127.0.0.1:8091"
)

// Names of the subdirectories of the data directory.
const (
	PBDataDirName  = "pb_data"
	TilesDirName   = "tiles"
	SecretsDirName = "secrets"
)

// Config is the validated instance configuration.
type Config struct {
	// DataDir is the absolute path of the data directory.
	DataDir string
	// Listen is the default HTTP listen address of `osfm serve`; an explicit --http flag takes precedence.
	Listen string
	// PublicURL is the absolute URL the instance is reached at, without a trailing slash; empty when unset. It is
	// https://, or http:// for localhost or a loopback address only.
	PublicURL string
	// AdminUI reports whether the PocketBase admin UI (/_/) and PocketBase's own APIs are served, on a separate
	// listener at AdminListen. They are never served on the main listener.
	AdminUI bool
	// AdminListen is the address of the admin listener (see AdminUI).
	AdminListen string

	// listenSet records whether OSFM_LISTEN was given, as opposed to the default.
	listenSet bool
}

// Load reads the configuration from the process environment.
func Load() (*Config, error) {
	return Parse(os.LookupEnv)
}

// Parse reads the configuration through lookup, which has the signature of [os.LookupEnv]. Empty or blank values count
// as unset. All invalid values are reported together.
func Parse(lookup func(string) (string, bool)) (*Config, error) {
	get := func(name string) string {
		v, _ := lookup(name)
		return strings.TrimSpace(v)
	}

	var errs []error
	cfg := &Config{}

	dataDir := get(EnvDataDir)
	if dataDir == "" {
		dataDir = DefaultDataDir
	}
	if abs, err := filepath.Abs(dataDir); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", EnvDataDir, err))
	} else {
		cfg.DataDir = abs
	}

	cfg.Listen = get(EnvListen)
	cfg.listenSet = cfg.Listen != ""
	if !cfg.listenSet {
		cfg.Listen = DefaultListen
	}
	if err := validateListen(cfg.Listen); err != nil {
		errs = append(errs, fmt.Errorf("%s=%q: %w", EnvListen, cfg.Listen, err))
	}

	if raw := get(EnvPublicURL); raw != "" {
		u, err := parsePublicURL(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s=%q: %w", EnvPublicURL, raw, err))
		}
		cfg.PublicURL = u
	}

	if raw := get(EnvAdminUI); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s=%q: not a boolean (use true or false)", EnvAdminUI, raw))
		}
		cfg.AdminUI = v
	}

	cfg.AdminListen = get(EnvAdminListen)
	if cfg.AdminListen == "" {
		cfg.AdminListen = DefaultAdminListen
	}
	if err := validateListen(cfg.AdminListen); err != nil {
		errs = append(errs, fmt.Errorf("%s=%q: %w", EnvAdminListen, cfg.AdminListen, err))
	}

	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// PublicHTTPS reports whether PublicURL is an https:// URL whose host is not the local machine: the instance is
// served over HTTPS at a real host name. Settings that would break plain-HTTP local development, or that browsers would
// pin to localhost, depend on it: HSTS and the __Host- prefix of the session cookie.
func (c *Config) PublicHTTPS() bool {
	u, err := url.Parse(c.PublicURL)
	return err == nil && u.Scheme == "https" && !isLoopbackHost(u.Hostname())
}

// DevServerOrigin is the origin of the web app's development server (Vite, web/vite.config.ts), which proxies the
// API to an instance running on the same machine.
const DevServerOrigin = "http://localhost:5173"

// PasskeyRP returns the WebAuthn relying party ID and the origins passkey ceremonies are accepted from (D3, D4). A
// passkey is bound to its relying party ID, a host name, and the browser reports the origin of the page that ran the
// ceremony, which the server checks against the list.
//
//   - An instance at a public host name (PublicHTTPS): the ID is that host name and the only origin is PublicURL's,
//     without a default port. Nothing else is accepted.
//   - Local development (PublicURL on localhost, a name under .localhost or a loopback address, or unset): browsers do
//     not accept an IP address as relying party ID, so a loopback address is replaced with localhost, and passkeys
//     work at http://localhost:<port> but not at http://127.0.0.1:<port>. With PublicURL unset the port is the one of
//     Listen. The development server's origin, [DevServerOrigin], is accepted besides, when the ID is localhost.
func (c *Config) PasskeyRP() (rpID string, origins []string) {
	u, err := url.Parse(c.PublicURL)
	if c.PublicURL == "" || err != nil {
		origin := "http://localhost"
		if _, port, err := net.SplitHostPort(c.Listen); err == nil {
			origin += ":" + port
		}
		return "localhost", withDevServer(origin)
	}
	scheme, host, port := strings.ToLower(u.Scheme), strings.ToLower(u.Hostname()), u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	local := isLoopbackHost(host)
	if local && net.ParseIP(host) != nil {
		host = "localhost"
	}
	origin := scheme + "://" + host
	if strings.Contains(host, ":") { // an IPv6 address, which only a public URL keeps
		origin = scheme + "://[" + host + "]"
	}
	if port != "" {
		origin += ":" + port
	}
	if local && host == "localhost" {
		return host, withDevServer(origin)
	}
	return host, []string{origin}
}

func withDevServer(origin string) []string {
	if origin == DevServerOrigin {
		return []string{origin}
	}
	return []string{origin, DevServerOrigin}
}

// PBDataDir returns the PocketBase data directory, <data>/pb_data.
func (c *Config) PBDataDir() string { return filepath.Join(c.DataDir, PBDataDirName) }

// TilesDir returns the directory of the per-city PMTiles files, <data>/tiles.
func (c *Config) TilesDir() string { return filepath.Join(c.DataDir, TilesDirName) }

// SecretsDir returns the directory of the instance keys, <data>/secrets.
func (c *Config) SecretsDir() string { return filepath.Join(c.DataDir, SecretsDirName) }

// HTTPAddr returns the address `osfm serve` listens on when no --http flag is given. withDomains reports whether
// domain arguments were passed to serve (PocketBase then obtains certificates itself and defaults to 0.0.0.0:80);
// in that case an empty string is returned unless OSFM_LISTEN was set, leaving PocketBase's default in place.
func (c *Config) HTTPAddr(withDomains bool) string {
	if withDomains && !c.listenSet {
		return ""
	}
	return c.Listen
}

// isLoopbackHost reports whether host names the local machine: localhost, a name under .localhost, or a loopback IP
// address. Browsers treat these as secure contexts and accept Secure cookies from them over plain HTTP.
func isLoopbackHost(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateListen(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.New("not a host:port address (e.g. 127.0.0.1:8090 or 0.0.0.0:8090)")
	}
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
		return errors.New("port must be a number from 1 to 65535")
	}
	return nil
}

func parsePublicURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("not a valid URL")
	}
	switch {
	case u.Scheme != "http" && u.Scheme != "https":
		return "", errors.New("must be an absolute http:// or https:// URL")
	case u.Host == "" || u.Hostname() == "":
		return "", errors.New("must include a host name")
	case u.User != nil:
		return "", errors.New("must not contain user information")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return "", errors.New("must not contain a query or fragment")
	case strings.EqualFold(u.Scheme, "http") && !isLoopbackHost(u.Hostname()):
		// The session cookie is Secure: over plain HTTP, browsers neither store nor send it, except for the local
		// machine. An instance at such a URL would let nobody sign in.
		return "", errors.New("must be https:// unless the host is localhost or a loopback address (the session " +
			"cookie is Secure, so browsers would not keep it over plain HTTP)")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
