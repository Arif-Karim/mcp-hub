package hubauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testKID = "test-key-1"
	testAud = "service-directory"
	testIss = "mcp-hub"
)

// jwksServer serves a JWKS containing pub under kid and counts fetches.
type jwksServer struct {
	*httptest.Server
	fetches atomic.Int32
}

func newJWKSServer(t *testing.T, kid string, pub ed25519.PublicKey) *jwksServer {
	t.Helper()
	s := &jwksServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "use": "sig", "kid": kid,
			"x": base64.RawURLEncoding.EncodeToString(pub),
		}}})
	}))
	t.Cleanup(s.Close)
	return s
}

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func claims(mut func(jwt.MapClaims)) jwt.MapClaims {
	now := time.Now()
	c := jwt.MapClaims{
		"sub": "dev.user@example.test",
		"iss": testIss,
		"aud": testAud,
		"iat": now.Unix(),
		"nbf": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	}
	if mut != nil {
		mut(c)
	}
	return c
}

func signEd(t *testing.T, priv ed25519.PrivateKey, kid string, c jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, c)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type fixture struct {
	priv    ed25519.PrivateKey
	jwks    *jwksServer
	handler http.Handler
	logs    *bytes.Buffer
	gotSub  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pub, priv := newKey(t)
	f := &fixture{priv: priv, jwks: newJWKSServer(t, testKID, pub), logs: &bytes.Buffer{}}
	v := &Verifier{Keys: NewJWKS(f.jwks.URL, nil), Issuer: testIss, Audience: testAud}
	logger := slog.New(slog.NewJSONHandler(f.logs, nil))
	f.handler = Middleware(v, logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.gotSub = SubjectFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	return f
}

func (f *fixture) do(authz string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func TestMiddlewareValidTokenPassesWithSubject(t *testing.T) {
	f := newFixture(t)
	rec := f.do("Bearer " + signEd(t, f.priv, testKID, claims(nil)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body %s", rec.Code, rec.Body)
	}
	if f.gotSub != "dev.user@example.test" {
		t.Fatalf("subject = %q", f.gotSub)
	}
}

func TestMiddlewareRejects(t *testing.T) {
	_, otherPriv := newKey(t)
	cases := []struct {
		name  string
		authz func(t *testing.T, f *fixture) string
	}{
		{"missing header", func(*testing.T, *fixture) string { return "" }},
		{"not bearer", func(*testing.T, *fixture) string { return "Basic dXNlcjpwYXNz" }},
		{"malformed token", func(*testing.T, *fixture) string { return "Bearer not.a.jwt" }},
		{"empty bearer", func(*testing.T, *fixture) string { return "Bearer " }},
		{"wrong alg HS256", func(t *testing.T, _ *fixture) string {
			tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims(nil))
			tok.Header["kid"] = testKID
			s, err := tok.SignedString([]byte("shared-secret-shared-secret-1234"))
			if err != nil {
				t.Fatal(err)
			}
			return "Bearer " + s
		}},
		{"alg none", func(t *testing.T, _ *fixture) string {
			tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims(nil))
			tok.Header["kid"] = testKID
			s, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
			if err != nil {
				t.Fatal(err)
			}
			return "Bearer " + s
		}},
		{"wrong audience", func(t *testing.T, f *fixture) string {
			return "Bearer " + signEd(t, f.priv, testKID, claims(func(c jwt.MapClaims) { c["aud"] = "payments-sandbox" }))
		}},
		{"wrong issuer", func(t *testing.T, f *fixture) string {
			return "Bearer " + signEd(t, f.priv, testKID, claims(func(c jwt.MapClaims) { c["iss"] = "someone-else" }))
		}},
		{"expired beyond leeway", func(t *testing.T, f *fixture) string {
			return "Bearer " + signEd(t, f.priv, testKID, claims(func(c jwt.MapClaims) {
				c["exp"] = time.Now().Add(-Leeway - 5*time.Second).Unix()
			}))
		}},
		{"missing exp", func(t *testing.T, f *fixture) string {
			return "Bearer " + signEd(t, f.priv, testKID, claims(func(c jwt.MapClaims) { delete(c, "exp") }))
		}},
		{"not yet valid beyond leeway", func(t *testing.T, f *fixture) string {
			return "Bearer " + signEd(t, f.priv, testKID, claims(func(c jwt.MapClaims) {
				c["nbf"] = time.Now().Add(Leeway + time.Minute).Unix()
			}))
		}},
		{"missing subject", func(t *testing.T, f *fixture) string {
			return "Bearer " + signEd(t, f.priv, testKID, claims(func(c jwt.MapClaims) { delete(c, "sub") }))
		}},
		{"unknown kid", func(t *testing.T, f *fixture) string {
			return "Bearer " + signEd(t, f.priv, "rotated-away", claims(nil))
		}},
		{"signed by other key", func(t *testing.T, _ *fixture) string {
			return "Bearer " + signEd(t, otherPriv, testKID, claims(nil))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			authz := tc.authz(t, f)
			rec := f.do(authz)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("content-type = %q", ct)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] != "unauthorized" || len(body) != 1 {
				t.Errorf("body = %s", rec.Body)
			}
			if f.gotSub != "" {
				t.Errorf("next handler ran")
			}
			if !strings.Contains(f.logs.String(), `"reason"`) {
				t.Errorf("rejection reason not logged: %s", f.logs)
			}
			if tok := strings.TrimPrefix(authz, "Bearer "); len(tok) > 10 && strings.Contains(f.logs.String(), tok) {
				t.Errorf("token leaked into logs")
			}
		})
	}
}

func TestMiddlewareAcceptsWithinLeeway(t *testing.T) {
	f := newFixture(t)
	tok := signEd(t, f.priv, testKID, claims(func(c jwt.MapClaims) {
		c["exp"] = time.Now().Add(-Leeway / 2).Unix()
		c["nbf"] = time.Now().Add(Leeway / 2).Unix()
	}))
	if rec := f.do("Bearer " + tok); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestVerifyErrorsAreSentinels(t *testing.T) {
	f := newFixture(t)
	v := &Verifier{Keys: NewJWKS(f.jwks.URL, nil), Issuer: testIss, Audience: testAud}
	_, err := v.Verify(context.Background(), signEd(t, f.priv, "nope", claims(nil)))
	if !errors.Is(err, ErrUnauthorized) || !errors.Is(err, ErrUnknownKID) {
		t.Fatalf("err = %v, want ErrUnauthorized wrapping ErrUnknownKID", err)
	}
}

func TestJWKSCachesAndRateLimitsRefetch(t *testing.T) {
	pub, _ := newKey(t)
	srv := newJWKSServer(t, testKID, pub)
	j := NewJWKS(srv.URL, nil)
	now := time.Unix(1_700_000_000, 0)
	j.now = func() time.Time { return now }
	ctx := context.Background()

	got, err := j.Key(ctx, testKID)
	if err != nil || !bytes.Equal(got, pub) {
		t.Fatalf("Key = %x, %v", got, err)
	}
	if _, err := j.Key(ctx, testKID); err != nil {
		t.Fatal(err)
	}
	if n := srv.fetches.Load(); n != 1 {
		t.Fatalf("fetches after cached hit = %d, want 1", n)
	}

	// Unknown kid within the interval: no refetch.
	if _, err := j.Key(ctx, "other"); !errors.Is(err, ErrUnknownKID) {
		t.Fatalf("err = %v, want ErrUnknownKID", err)
	}
	if n := srv.fetches.Load(); n != 1 {
		t.Fatalf("fetches = %d, want 1 (rate limited)", n)
	}

	// After the interval an unknown kid triggers exactly one refetch.
	now = now.Add(MinRefetchInterval)
	_, _ = j.Key(ctx, "other")
	_, _ = j.Key(ctx, "other")
	if n := srv.fetches.Load(); n != 2 {
		t.Fatalf("fetches = %d, want 2", n)
	}
}
