package token_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Arif-Karim/mcp-hub/internal/token"
)

const iss = "http://127.0.0.1:7420"

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newIssuer(t *testing.T) (*token.Issuer, *clock, string) {
	t.Helper()
	c := &clock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "keys", "signing.pem")
	is, err := token.NewIssuer(path, iss, c.now)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	return is, c, path
}

func TestIssueVerifyRoundTrip(t *testing.T) {
	is, c, _ := newIssuer(t)

	tok, err := is.Issue("dev@example.test", "service-directory", 5*time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	claims, err := is.Verify(tok, "service-directory")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "dev@example.test" || claims.Issuer != iss {
		t.Fatalf("claims sub=%q iss=%q", claims.Subject, claims.Issuer)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "service-directory" {
		t.Fatalf("aud = %v", claims.Audience)
	}
	if !claims.IssuedAt.Equal(c.t) || !claims.NotBefore.Equal(c.t) {
		t.Fatalf("iat=%v nbf=%v, want %v", claims.IssuedAt, claims.NotBefore, c.t)
	}
	if !claims.ExpiresAt.Equal(c.t.Add(5 * time.Minute)) {
		t.Fatalf("exp = %v", claims.ExpiresAt)
	}
	if claims.ID == "" {
		t.Fatal("jti is empty")
	}

	// Header carries kid and alg, and aud is a single string on the wire.
	parts := strings.Split(tok, ".")
	hdr := decodeSeg(t, parts[0])
	if hdr["alg"] != "EdDSA" || hdr["kid"] != is.KID() {
		t.Fatalf("header = %v, kid want %q", hdr, is.KID())
	}
	payload := decodeSeg(t, parts[1])
	if aud, ok := payload["aud"].(string); !ok || aud != "service-directory" {
		t.Fatalf("aud on wire = %#v, want single string", payload["aud"])
	}
}

func TestJTIIsUniquePerToken(t *testing.T) {
	is, _, _ := newIssuer(t)
	a, _ := is.Issue("u", "x1", time.Minute)
	b, _ := is.Issue("u", "x1", time.Minute)
	ca, _ := is.Verify(a, "x1")
	cb, _ := is.Verify(b, "x1")
	if ca.ID == cb.ID {
		t.Fatalf("jti repeated: %q", ca.ID)
	}
}

func TestVerifyRejectsWrongAudience(t *testing.T) {
	is, _, _ := newIssuer(t)
	tok, _ := is.Issue("u", "service-a", time.Minute)
	if _, err := is.Verify(tok, "service-b"); !errors.Is(err, token.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	is, c, _ := newIssuer(t)
	tok, _ := is.Issue("u", "svc", time.Minute)

	c.t = c.t.Add(time.Minute + token.Leeway - time.Second)
	if _, err := is.Verify(tok, "svc"); err != nil {
		t.Fatalf("within leeway: %v", err)
	}
	c.t = c.t.Add(2 * time.Second)
	if _, err := is.Verify(tok, "svc"); !errors.Is(err, token.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyRejectsNotYetValidToken(t *testing.T) {
	is, c, _ := newIssuer(t)
	tok, _ := is.Issue("u", "svc", time.Minute)
	c.t = c.t.Add(-token.Leeway - time.Second)
	if _, err := is.Verify(tok, "svc"); !errors.Is(err, token.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyRejectsAlgConfusionAndNone(t *testing.T) {
	is, c, _ := newIssuer(t)
	claims := jwt.MapClaims{
		"iss": iss, "sub": "attacker", "aud": "svc",
		"iat": c.t.Unix(), "nbf": c.t.Unix(), "exp": c.t.Add(time.Minute).Unix(),
	}

	pub := publicKeyFromJWKS(t, is)
	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	hs.Header["kid"] = is.KID()
	hsTok, err := hs.SignedString([]byte(pub))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := is.Verify(hsTok, "svc"); !errors.Is(err, token.ErrInvalidToken) {
		t.Fatalf("HS256 confusion: err = %v, want ErrInvalidToken", err)
	}

	none := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	none.Header["kid"] = is.KID()
	noneTok, err := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := is.Verify(noneTok, "svc"); !errors.Is(err, token.ErrInvalidToken) {
		t.Fatalf("alg none: err = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	is, _, _ := newIssuer(t)
	tok, _ := is.Issue("alice", "svc", time.Minute)
	parts := strings.Split(tok, ".")
	payload := decodeSeg(t, parts[1])
	payload["sub"] = "mallory"
	b, _ := json.Marshal(payload)
	parts[1] = base64.RawURLEncoding.EncodeToString(b)
	if _, err := is.Verify(strings.Join(parts, "."), "svc"); !errors.Is(err, token.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyRejectsWrongIssuerAndKID(t *testing.T) {
	is, c, path := newIssuer(t)

	other, err := token.NewIssuer(path, "http://evil.test", c.now) // same key, different iss
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := other.Issue("u", "svc", time.Minute)
	if _, err := is.Verify(tok, "svc"); !errors.Is(err, token.ErrInvalidToken) {
		t.Fatalf("wrong iss: err = %v", err)
	}

	stranger, err := token.NewIssuer(filepath.Join(t.TempDir(), "k.pem"), iss, c.now) // different key
	if err != nil {
		t.Fatal(err)
	}
	tok2, _ := stranger.Issue("u", "svc", time.Minute)
	if _, err := is.Verify(tok2, "svc"); !errors.Is(err, token.ErrInvalidToken) {
		t.Fatalf("foreign key: err = %v", err)
	}
}

func TestKeyIsPersistedWithSafePermsAndReloaded(t *testing.T) {
	is, c, path := newIssuer(t)

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("key perms = %o, want 600", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(path))
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir perms = %o, want 700", di.Mode().Perm())
	}
	raw, _ := os.ReadFile(path) //nolint:gosec // test path
	if !strings.HasPrefix(string(raw), "-----BEGIN PRIVATE KEY-----") {
		t.Fatalf("key file is not PKCS8 PEM: %.30q", raw)
	}

	again, err := token.NewIssuer(path, iss, c.now)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if again.KID() != is.KID() || len(is.KID()) != 11 {
		t.Fatalf("kid %q vs %q", again.KID(), is.KID())
	}
	tok, _ := is.Issue("u", "svc", time.Minute)
	if _, err := again.Verify(tok, "svc"); err != nil {
		t.Fatalf("reloaded issuer cannot verify: %v", err)
	}
}

func TestInsecureKeyFileRejected(t *testing.T) {
	_, c, path := newIssuer(t)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := token.NewIssuer(path, iss, c.now); !errors.Is(err, token.ErrInsecureKeyFile) {
		t.Fatalf("err = %v, want ErrInsecureKeyFile", err)
	}
}

func TestCorruptKeyFileRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k.pem")
	if err := os.WriteFile(path, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := token.NewIssuer(path, iss, time.Now); !errors.Is(err, token.ErrInvalidKey) {
		t.Fatalf("err = %v, want ErrInvalidKey", err)
	}
}

func TestTTLBounds(t *testing.T) {
	is, _, _ := newIssuer(t)
	for _, ttl := range []time.Duration{0, -time.Second, token.MaxTTL + time.Nanosecond} {
		if _, err := is.Issue("u", "svc", ttl); !errors.Is(err, token.ErrInvalidTTL) {
			t.Errorf("ttl %v: err = %v, want ErrInvalidTTL", ttl, err)
		}
	}
	if _, err := is.Issue("u", "svc", token.MaxTTL); err != nil {
		t.Fatalf("ttl = MaxTTL: %v", err)
	}
	if token.MaxTTL != 15*time.Minute {
		t.Fatalf("MaxTTL = %v", token.MaxTTL)
	}
}

func TestIssueRequiresSubjectAndAudience(t *testing.T) {
	is, _, _ := newIssuer(t)
	if _, err := is.Issue("", "svc", time.Minute); !errors.Is(err, token.ErrInvalidClaims) {
		t.Fatalf("empty sub: %v", err)
	}
	if _, err := is.Issue("u", "", time.Minute); !errors.Is(err, token.ErrInvalidClaims) {
		t.Fatalf("empty aud: %v", err)
	}
}

func TestJWKSHandler(t *testing.T) {
	is, c, _ := newIssuer(t)
	rec := httptest.NewRecorder()
	is.JWKSHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "max-age=300" {
		t.Fatalf("cache-control = %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q", got)
	}
	var doc struct {
		Keys []map[string]string `json:"keys"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Keys) != 1 {
		t.Fatalf("keys = %d", len(doc.Keys))
	}
	k := doc.Keys[0]
	want := map[string]string{"kty": "OKP", "crv": "Ed25519", "kid": is.KID(), "alg": "EdDSA", "use": "sig"}
	for f, v := range want {
		if k[f] != v {
			t.Errorf("%s = %q, want %q", f, k[f], v)
		}
	}
	if _, ok := k["d"]; ok {
		t.Fatal("JWKS leaks private key material")
	}

	// A third party holding only the JWKS can verify our tokens.
	pub := publicKeyFromJWKS(t, is)
	tok, _ := is.Issue("u", "svc", time.Minute)
	_, err := jwt.Parse(tok, func(*jwt.Token) (any, error) { return pub, nil },
		jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithTimeFunc(c.now))
	if err != nil {
		t.Fatalf("verify with JWKS key: %v", err)
	}
}

func publicKeyFromJWKS(t *testing.T, is *token.Issuer) ed25519.PublicKey {
	t.Helper()
	rec := httptest.NewRecorder()
	is.JWKSHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var doc struct {
		Keys []struct {
			X string `json:"x"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil || len(doc.Keys) != 1 {
		t.Fatalf("jwks decode: %v", err)
	}
	x, err := base64.RawURLEncoding.DecodeString(doc.Keys[0].X)
	if err != nil || len(x) != ed25519.PublicKeySize {
		t.Fatalf("x = %q (%v)", doc.Keys[0].X, err)
	}
	return ed25519.PublicKey(x)
}

func decodeSeg(t *testing.T, s string) map[string]any {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
