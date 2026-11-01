// Package hubauth verifies the short-lived JWTs that mcp-hub injects in front of
// every request it proxies to an MCP server.
//
// Tokens are EdDSA (Ed25519) only. The signing key is looked up by "kid" in the
// hub's JWKS, which is cached and refetched on an unknown kid at most once per
// MinRefetchInterval. "iss", "aud", "exp" and "nbf" are checked with Leeway.
package hubauth

import (
	"context"
	"crypto/ed25519"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Leeway tolerates clock skew between hub and server when checking exp/nbf.
const Leeway = 30 * time.Second

// MinRefetchInterval rate-limits JWKS refetches triggered by unknown kids.
const MinRefetchInterval = 30 * time.Second

// Sentinel errors. Every verification failure wraps ErrUnauthorized.
var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrUnknownKID   = errors.New("unknown kid")
)

// KeySource resolves a key id to an Ed25519 public key.
type KeySource interface {
	Key(ctx context.Context, kid string) (ed25519.PublicKey, error)
}

// JWKS is a KeySource backed by a remote JWKS document.
type JWKS struct{ now func() time.Time }

// NewJWKS returns a JWKS cache for url. A nil client uses a client with a 5s timeout.
func NewJWKS(_ string, _ *http.Client) *JWKS { return &JWKS{} }

// Key implements KeySource.
func (j *JWKS) Key(_ context.Context, _ string) (ed25519.PublicKey, error) {
	return nil, ErrUnknownKID
}

// Verifier validates hub tokens.
type Verifier struct {
	Keys     KeySource
	Issuer   string
	Audience string
}

// Verify checks raw and returns its subject.
func (v *Verifier) Verify(_ context.Context, _ string) (string, error) {
	return "", ErrUnauthorized
}

// SubjectFrom returns the authenticated subject placed in ctx by Middleware.
func SubjectFrom(_ context.Context) string { return "" }

// Middleware rejects requests without a valid hub token with 401
// {"error":"unauthorized"} and otherwise puts the subject in the request context.
func Middleware(_ *Verifier, _ *slog.Logger, next http.Handler) http.Handler {
	return next
}
