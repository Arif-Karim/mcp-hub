package hubauth

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	// MinRefetchInterval rate-limits refetches after a successful fetch, so a
	// flood of tokens with random kids cannot hammer the hub.
	MinRefetchInterval = 30 * time.Second
	// FailureBackoff is the (shorter) wait before retrying a failed fetch, so a
	// brief hub blip does not lock auth out for MinRefetchInterval.
	FailureBackoff = 5 * time.Second
	// MaxKeyAge is how long a fetched key set is trusted before it is refetched
	// and replaced, so keys the hub retires stop being accepted.
	MaxKeyAge = 10 * time.Minute

	fetchTimeout = 5 * time.Second
)

// ErrJWKSUnavailable wraps every failure to fetch or parse the JWKS.
var ErrJWKSUnavailable = errors.New("jwks unavailable")

// KeySource resolves a key id to an Ed25519 public key.
type KeySource interface {
	Key(ctx context.Context, kid string) (ed25519.PublicKey, error)
}

// JWKS is a KeySource backed by a remote JWKS document. It is safe for
// concurrent use; the network fetch runs outside the lock and concurrent
// misses share a single in-flight fetch.
type JWKS struct {
	url    string
	client *http.Client
	now    func() time.Time

	mu          sync.Mutex
	keys        map[string]ed25519.PublicKey
	fetchedAt   time.Time     // last successful fetch
	nextAttempt time.Time     // earliest time another fetch may start
	lastErr     error         // error from the last fetch, nil after a success
	inflight    chan struct{} // non-nil while a fetch runs; closed when it ends
}

// NewJWKS returns a JWKS cache for url. A nil client uses one with a 5s
// timeout that refuses redirects to a different host.
func NewJWKS(url string, client *http.Client) *JWKS {
	if client == nil {
		client = &http.Client{
			Timeout: fetchTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if req.URL.Host != via[0].URL.Host || len(via) >= 3 {
					return errors.New("refusing cross-host redirect")
				}
				return nil
			},
		}
	}
	return &JWKS{url: url, client: client, now: time.Now}
}

// Key implements KeySource.
//
// A known kid in a fresh key set returns immediately. Otherwise a fetch runs if
// the rate limit allows (refreshing an expired set, or looking for a new kid).
// If the hub is unreachable, cached keys keep working until it is back.
func (j *JWKS) Key(ctx context.Context, kid string) (ed25519.PublicKey, error) {
	for {
		j.mu.Lock()
		now := j.now()
		k, known := j.keys[kid]
		if known && now.Sub(j.fetchedAt) < MaxKeyAge {
			j.mu.Unlock()
			return k, nil
		}
		if ch := j.inflight; ch != nil {
			j.mu.Unlock()
			select {
			case <-ch:
				continue // re-evaluate with the new state
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if now.Before(j.nextAttempt) {
			lastErr := j.lastErr
			j.mu.Unlock()
			switch {
			case known:
				return k, nil // expired, but refresh is backing off: serve cached
			case lastErr != nil:
				return nil, lastErr
			default:
				return nil, ErrUnknownKID
			}
		}
		ch := make(chan struct{})
		j.inflight = ch
		j.nextAttempt = now.Add(MinRefetchInterval)
		j.mu.Unlock()

		keys, err := j.fetch()

		j.mu.Lock()
		if err != nil {
			j.lastErr = err
			j.nextAttempt = j.now().Add(FailureBackoff)
		} else {
			j.keys, j.lastErr = keys, nil
			j.fetchedAt = j.now()
			j.nextAttempt = j.fetchedAt.Add(MinRefetchInterval)
		}
		j.inflight = nil
		close(ch)
		j.mu.Unlock()
	}
}

type jwk struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Kid string `json:"kid"`
}

// fetch uses its own timeout rather than the caller's context: the result is
// shared, so one cancelled request must not fail it for everyone.
func (j *JWKS) fetch() (map[string]ed25519.PublicKey, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.url, nil) //nolint:gosec // G704: see below
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJWKSUnavailable, err)
	}
	resp, err := j.client.Do(req) //nolint:gosec // G704: URL is operator config (HUB_JWKS_URL), validated at startup
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJWKSUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: unexpected status %d", ErrJWKSUnavailable, resp.StatusCode)
	}
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: decode: %w", ErrJWKSUnavailable, err)
	}
	keys := make(map[string]ed25519.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "OKP" || k.Crv != "Ed25519" || k.Kid == "" {
			continue // not a key we can use; ignore rather than fail the whole set
		}
		x, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil || len(x) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w: key %q has invalid x", ErrJWKSUnavailable, k.Kid)
		}
		keys[k.Kid] = ed25519.PublicKey(x)
	}
	return keys, nil
}
