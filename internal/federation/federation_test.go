package federation_test

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EatWell-Studio/OSFM/internal/federation"
	"github.com/EatWell-Studio/OSFM/internal/version"
)

func newKey(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func get(t *testing.T, h http.Handler, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, federation.WellKnownPath, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestDocument(t *testing.T) {
	key := newKey(t)
	h, err := federation.NewHandler(key)
	if err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != federation.CacheControl {
		t.Errorf("Cache-Control %q", cc)
	}

	// Exactly these fields: anything else (an instance name, members, cities) would be a change of the format.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields["dataFormatVersion"] == nil || fields["softwareVersion"] == nil ||
		fields["publicKey"] == nil {
		t.Errorf("document fields: %s", rec.Body.String())
	}
	var keyFields map[string]string
	if err := json.Unmarshal(fields["publicKey"], &keyFields); err != nil {
		t.Fatal(err)
	}
	if len(keyFields) != 3 || keyFields["kty"] != "OKP" || keyFields["crv"] != "Ed25519" {
		t.Errorf("publicKey: %s", fields["publicKey"])
	}

	var doc federation.Document
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.DataFormatVersion != federation.DataFormatVersion || doc.SoftwareVersion != version.String() {
		t.Errorf("document %+v", doc)
	}
	got, err := doc.PublicKey.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(key) {
		t.Errorf("public key %x, want %x", got, key)
	}
}

// TestJWKEncoding checks the encoding against the example key of RFC 8037, appendix A.2.
func TestJWKEncoding(t *testing.T) {
	raw, err := hex.DecodeString("d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a")
	if err != nil {
		t.Fatal(err)
	}
	jwk := federation.NewJWK(raw)
	want := federation.JWK{Kty: "OKP", Crv: "Ed25519", X: "11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}
	if jwk != want {
		t.Errorf("NewJWK = %+v, want %+v", jwk, want)
	}
	for _, bad := range []federation.JWK{
		{Kty: "OKP", Crv: "X25519", X: want.X},
		{Kty: "EC", Crv: "Ed25519", X: want.X},
		{Kty: "OKP", Crv: "Ed25519", X: want.X + "="},
		{Kty: "OKP", Crv: "Ed25519", X: want.X[:20]},
	} {
		if _, err := bad.PublicKey(); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func TestConditionalRequest(t *testing.T) {
	h, err := federation.NewHandler(newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	etag := get(t, h, nil).Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	for _, inm := range []string{etag, "W/" + etag, `"other", ` + etag, "*"} {
		rec := get(t, h, http.Header{"If-None-Match": {inm}})
		if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
			t.Errorf("If-None-Match %s: status %d, %d bytes", inm, rec.Code, rec.Body.Len())
		}
		if rec.Header().Get("Cache-Control") != federation.CacheControl {
			t.Errorf("If-None-Match %s: no Cache-Control on the 304", inm)
		}
	}
	if rec := get(t, h, http.Header{"If-None-Match": {`"other"`}}); rec.Code != http.StatusOK {
		t.Errorf("non-matching If-None-Match: status %d", rec.Code)
	}
	// Another key, another document, another ETag.
	h2, err := federation.NewHandler(newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if get(t, h2, nil).Header().Get("ETag") == etag {
		t.Error("two keys, one ETag")
	}
}

func TestRejectsOtherKeys(t *testing.T) {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []any{nil, ec.Public(), ed25519.PublicKey(make([]byte, 31))} {
		if _, err := federation.NewHandler(key); err == nil {
			t.Errorf("NewHandler(%T) accepted", key)
		}
	}
}
