// Package federation serves the instance metadata that other OSFM instances will read, /.well-known/osfm.json (F5).
// Federation itself is not implemented; this is the part of it that is fixed from the start.
//
// The document is public, served without a session: it is meant for other instances, which have none, and it says
// nothing about the circle. It holds only the instance public key, the data format version and the software version;
// never the instance name, its cities, its members or its places. An instance is identified by its key, not by a name.
//
// Example:
//
//	{
//	  "dataFormatVersion": 1,
//	  "softwareVersion": "1.2.3",
//	  "publicKey": {"kty": "OKP", "crv": "Ed25519", "x": "11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}
//	}
//
// The public key is a JSON Web Key (RFC 8037 §2): key type OKP, curve Ed25519, and in x the 32-byte public key in
// base64url without padding. This encoding is part of the format; other instances can parse it with any JOSE library.
package federation

import (
	"crypto"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/EatWell-Studio/OSFM/internal/version"
)

// WellKnownPath is the path of the instance metadata document.
const WellKnownPath = "/.well-known/osfm.json"

// DataFormatVersion is the version of the OSFM data format: the objects an instance exports and will later exchange
// with other instances (stable UUIDv7 identifiers, tombstones for deleted objects, per-object visibility). It is
// incremented when the format changes incompatibly.
const DataFormatVersion = 1

// CacheControl is the Cache-Control header of the document. The key changes only if the instance loses it and
// generates a new one, and the software version only with an upgrade: an hour of caching, then revalidation by ETag.
const CacheControl = "public, max-age=3600"

// Document is the content of /.well-known/osfm.json.
type Document struct {
	DataFormatVersion int    `json:"dataFormatVersion"`
	SoftwareVersion   string `json:"softwareVersion"`
	PublicKey         JWK    `json:"publicKey"`
}

// JWK is an Ed25519 public key as a JSON Web Key (RFC 8037 §2).
type JWK struct {
	Kty string `json:"kty"` // always "OKP"
	Crv string `json:"crv"` // always "Ed25519"
	X   string `json:"x"`   // the public key, base64url without padding
}

// NewJWK returns key as a JSON Web Key.
func NewJWK(key ed25519.PublicKey) JWK {
	return JWK{Kty: "OKP", Crv: "Ed25519", X: base64.RawURLEncoding.EncodeToString(key)}
}

// PublicKey returns the Ed25519 public key that k holds.
func (k JWK) PublicKey() (ed25519.PublicKey, error) {
	if k.Kty != "OKP" || k.Crv != "Ed25519" {
		return nil, fmt.Errorf("federation: not an Ed25519 key (kty %q, crv %q)", k.Kty, k.Crv)
	}
	b, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("federation: x is not a base64url-encoded 32-byte Ed25519 public key")
	}
	return ed25519.PublicKey(b), nil
}

// NewHandler returns the handler of /.well-known/osfm.json for the instance public key, which must be an Ed25519
// key. The document is built once: its content does not change while the process runs.
func NewHandler(publicKey crypto.PublicKey) (http.Handler, error) {
	key, ok := publicKey.(ed25519.PublicKey)
	if !ok || len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("federation: the instance key must be an Ed25519 public key, got %T", publicKey)
	}
	body, err := json.Marshal(Document{
		DataFormatVersion: DataFormatVersion,
		SoftwareVersion:   version.String(),
		PublicKey:         NewJWK(key),
	})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", CacheControl)
		h.Set("ETag", etag)
		if matchesETag(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h.Set("Content-Type", "application/json")
		h.Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(body)
	}), nil
}

// Register adds the GET route of /.well-known/osfm.json to r, with publicKey the instance public key. The route is
// public: it must stay outside any session check.
func Register(r *router.Router[*core.RequestEvent], publicKey crypto.PublicKey) error {
	h, err := NewHandler(publicKey)
	if err != nil {
		return err
	}
	r.GET(WellKnownPath, apis.WrapStdHandler(h))
	return nil
}

// matchesETag reports whether an If-None-Match header value matches etag (weak comparison, RFC 9110 §13.1.2).
func matchesETag(header, etag string) bool {
	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == etag || candidate == "*" {
			return true
		}
	}
	return false
}
