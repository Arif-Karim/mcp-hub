package hubauth

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// MinRefetchInterval rate-limits JWKS refetches triggered by unknown kids.
const MinRefetchInterval = 30 * time.Second

// KeySource resolves a key id to an Ed25519 public key.
type KeySource interface {
	Key(ctx context.Context, kid string) (ed25519.PublicKey, error)
}

// JWKS is a KeySource backed by a remote JWKS document. It is safe for concurrent use.
type JWKS struct {
	url    string
	client *http.Client
	now    func() time.Time

	mu        sync.Mutex
	keys      map[string]ed25519.PublicKey
	lastFetch time.Time
}

// NewJWKS returns a JWKS cache for url. A nil client uses a client with a 5s timeout.
func NewJWKS(url string, client *http.Client) *JWKS {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &JWKS{url: url, client: client, now: time.Now}
}

// Key implements KeySource. Unknown kids trigger a refetch at most once per
// MinRefetchInterval so a flood of bad tokens cannot hammer the hub.
func (j *JWKS) Key(ctx context.Context, kid string) (ed25519.PublicKey, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if k, ok := j.keys[kid]; ok {
		return k, nil
	}
	if !j.lastFetch.IsZero() && j.now().Sub(j.lastFetch) < MinRefetchInterval {
		return nil, ErrUnknownKID
	}
	j.lastFetch = j.now()
	keys, err := j.fetch(ctx)
	if err != nil {
		return nil, err
	}
	j.keys = keys
	if k, ok := keys[kid]; ok {
		return k, nil
	}
	return nil, ErrUnknownKID
}

type jwk struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Kid string `json:"kid"`
}

func (j *JWKS) fetch(ctx context.Context) (map[string]ed25519.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.url, nil) //nolint:gosec // G704: see below
	if err != nil {
		return nil, fmt.Errorf("jwks: %w", err)
	}
	resp, err := j.client.Do(req) //nolint:gosec // G704: URL is operator config (HUB_JWKS_URL), validated at startup
	if err != nil {
		return nil, fmt.Errorf("jwks: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks: unexpected status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("jwks: decode: %w", err)
	}
	keys := make(map[string]ed25519.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "OKP" || k.Crv != "Ed25519" || k.Kid == "" {
			continue // not a key we can use; ignore rather than fail the whole set
		}
		x, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil || len(x) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("jwks: key %q has invalid x", k.Kid)
		}
		keys[k.Kid] = ed25519.PublicKey(x)
	}
	return keys, nil
}
