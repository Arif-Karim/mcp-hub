// Package token is the hub's development token issuer. It signs short-lived
// Ed25519 (EdDSA) JWTs that the MCP proxy injects for `hub-jwt` servers and
// publishes the public key as a JWKS. In production an enterprise IdP (token
// exchange on behalf of the signed-in user) replaces it; the wire format is the same.
package token

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// MaxTTL caps token lifetime; injected tokens are meant to be per-request.
const MaxTTL = 15 * time.Minute

// Leeway tolerates small clock skew between issuer and verifier.
const Leeway = 30 * time.Second

const pemType = "PRIVATE KEY"

// Sentinel errors.
var (
	ErrInsecureKeyFile = errors.New("token: signing key file is readable by group or others")
	ErrInvalidKey      = errors.New("token: signing key file is not a PKCS8 Ed25519 key")
	ErrInvalidTTL      = errors.New("token: ttl must be > 0 and <= 15m")
	ErrInvalidClaims   = errors.New("token: subject and audience are required")
	ErrInvalidToken    = errors.New("token: invalid token")
)

// Issuer signs and verifies hub JWTs with one Ed25519 key.
type Issuer struct {
	priv   ed25519.PrivateKey
	pub    ed25519.PublicKey
	kid    string
	issuer string
	now    func() time.Time
}

// NewIssuer loads the PKCS8 PEM key at keyPath, or generates and persists one
// (directory 0700, file 0600). A key file with group/other permissions is refused.
func NewIssuer(keyPath, issuer string, now func() time.Time) (*Issuer, error) {
	if now == nil {
		now = time.Now
	}
	priv, err := loadOrCreateKey(keyPath)
	if err != nil {
		return nil, err
	}
	pub, _ := priv.Public().(ed25519.PublicKey)
	sum := sha256.Sum256(pub)
	return &Issuer{
		priv:   priv,
		pub:    pub,
		kid:    base64.RawURLEncoding.EncodeToString(sum[:8]),
		issuer: issuer,
		now:    now,
	}, nil
}

// KID is the key ID placed in token headers and the JWKS.
func (i *Issuer) KID() string { return i.kid }

// Issue signs a token for sub with a single-string audience aud.
func (i *Issuer) Issue(sub, aud string, ttl time.Duration) (string, error) {
	if ttl <= 0 || ttl > MaxTTL {
		return "", ErrInvalidTTL
	}
	if sub == "" || aud == "" {
		return "", ErrInvalidClaims
	}
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", fmt.Errorf("token: jti: %w", err)
	}
	now := i.now()
	t := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"iss": i.issuer,
		"sub": sub,
		"aud": aud,
		"iat": now.Unix(),
		"nbf": now.Unix(),
		"exp": now.Add(ttl).Unix(),
		"jti": base64.RawURLEncoding.EncodeToString(jti),
	})
	t.Header["kid"] = i.kid
	return t.SignedString(i.priv)
}

// Verify checks signature (EdDSA only), kid, iss, aud and time claims.
// Every failure wraps ErrInvalidToken.
func (i *Issuer) Verify(tok, aud string) (*jwt.RegisteredClaims, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(tok, claims, func(t *jwt.Token) (any, error) {
		if kid, _ := t.Header["kid"].(string); kid != i.kid {
			return nil, errors.New("unknown kid")
		}
		return i.pub, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(i.issuer),
		jwt.WithAudience(aud),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(Leeway),
		jwt.WithTimeFunc(i.now),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	return claims, nil
}

func loadOrCreateKey(path string) (ed25519.PrivateKey, error) {
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return createKey(path)
	case err != nil:
		return nil, fmt.Errorf("token: stat key: %w", err)
	case !fi.Mode().IsRegular():
		return nil, fmt.Errorf("%w: not a regular file", ErrInvalidKey)
	case fi.Mode().Perm()&0o077 != 0:
		return nil, ErrInsecureKeyFile
	}
	raw, err := os.ReadFile(path) //nolint:gosec // path is operator config, perms checked above
	if err != nil {
		return nil, fmt.Errorf("token: read key: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != pemType {
		return nil, ErrInvalidKey
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, ErrInvalidKey
	}
	return priv, nil
}

func createKey(path string) (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("token: generate key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("token: marshal key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("token: key dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // operator-configured path
	if err != nil {
		return nil, fmt.Errorf("token: create key: %w", err)
	}
	if err := pem.Encode(f, &pem.Block{Type: pemType, Bytes: der}); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("token: write key: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("token: write key: %w", err)
	}
	return priv, nil
}
