// Package hubauth verifies the short-lived JWTs that mcp-hub injects in front of
// every request it proxies to an MCP server.
//
// Tokens are EdDSA (Ed25519) only. The signing key is looked up by "kid" in the
// hub's JWKS, which is cached for MaxKeyAge and refetched on an unknown kid at
// most once per MinRefetchInterval (FailureBackoff after a failed fetch).
// "iss", "aud", "exp" and "nbf" are checked with Leeway.
package hubauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Leeway tolerates clock skew between hub and server when checking exp/nbf.
const Leeway = 30 * time.Second

// Sentinel errors. Every verification failure wraps ErrUnauthorized.
var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrUnknownKID   = errors.New("unknown kid")
)

// Verifier validates hub tokens.
type Verifier struct {
	Keys     KeySource
	Issuer   string
	Audience string
}

// Verify checks raw and returns its subject. Errors wrap ErrUnauthorized.
func (v *Verifier) Verify(ctx context.Context, raw string) (string, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(v.Issuer),
		jwt.WithAudience(v.Audience),
		jwt.WithLeeway(Leeway),
		jwt.WithExpirationRequired(),
	)
	var claims jwt.RegisteredClaims
	_, err := parser.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("missing kid")
		}
		return v.Keys.Key(ctx, kid)
	})
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnauthorized, err)
	}
	if claims.Subject == "" {
		return "", fmt.Errorf("%w: missing sub", ErrUnauthorized)
	}
	return claims.Subject, nil
}

type subjectKey struct{}

// WithSubject returns a copy of ctx carrying the authenticated subject.
func WithSubject(ctx context.Context, sub string) context.Context {
	return context.WithValue(ctx, subjectKey{}, sub)
}

// SubjectFrom returns the authenticated subject placed in ctx by Middleware.
func SubjectFrom(ctx context.Context) string {
	s, _ := ctx.Value(subjectKey{}).(string)
	return s
}

// Middleware rejects requests without a valid hub token with 401
// {"error":"unauthorized"} and otherwise puts the subject in the request context.
// The rejection reason is logged; the token never is.
func Middleware(v *Verifier, logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub, err := authenticate(r, v)
		if err != nil {
			logger.Warn("request rejected", "reason", logSafe(err.Error()), "method", logSafe(r.Method), "path", logSafe(r.URL.Path))
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r.WithContext(WithSubject(r.Context(), sub)))
	})
}

func authenticate(r *http.Request, v *Verifier) (string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", fmt.Errorf("%w: missing Authorization header", ErrUnauthorized)
	}
	scheme, tok, ok := strings.Cut(h, " ")
	tok = strings.TrimSpace(tok)
	if !ok || !strings.EqualFold(scheme, "Bearer") || tok == "" {
		return "", fmt.Errorf("%w: Authorization is not a bearer token", ErrUnauthorized)
	}
	return v.Verify(r.Context(), tok)
}

// logSafe strips CR/LF from request-derived values before logging (log injection).
func logSafe(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	return strings.ReplaceAll(s, "\r", "")
}
