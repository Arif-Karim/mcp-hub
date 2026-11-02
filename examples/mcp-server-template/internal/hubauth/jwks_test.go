package hubauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeIDP is a JWKS endpoint whose key set, status and latency tests can change.
type fakeIDP struct {
	*httptest.Server
	mu      sync.Mutex
	keys    map[string]ed25519.PublicKey
	status  int
	gate    chan struct{} // if non-nil, requests block until it is closed
	fetches atomic.Int32
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	f := &fakeIDP{keys: map[string]ed25519.PublicKey{}, status: http.StatusOK}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.fetches.Add(1)
		f.mu.Lock()
		gate, status := f.gate, f.status
		var keys []map[string]string
		for kid, pub := range f.keys {
			keys = append(keys, map[string]string{
				"kty": "OKP", "crv": "Ed25519", "kid": kid, "x": base64.RawURLEncoding.EncodeToString(pub),
			})
		}
		f.mu.Unlock()
		if gate != nil {
			<-gate
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeIDP) set(fn func(f *fakeIDP)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestJWKS(url string) (*JWKS, *clock) {
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	j := NewJWKS(url, nil)
	j.now = c.now
	return j, c
}

// A failed fetch must not lock auth out for the full refetch interval.
func TestJWKSFailedFetchRetriesAfterShortBackoff(t *testing.T) {
	pub, _ := newKey(t)
	idp := newFakeIDP(t)
	idp.set(func(f *fakeIDP) { f.status = http.StatusServiceUnavailable; f.keys["k"] = pub })
	j, clk := newTestJWKS(idp.URL)
	ctx := context.Background()

	if _, err := j.Key(ctx, "k"); !errors.Is(err, ErrJWKSUnavailable) {
		t.Fatalf("err = %v, want ErrJWKSUnavailable", err)
	}
	idp.set(func(f *fakeIDP) { f.status = http.StatusOK })

	// Within the failure backoff: no new fetch.
	if _, err := j.Key(ctx, "k"); err == nil {
		t.Fatal("expected error inside backoff")
	}
	if n := idp.fetches.Load(); n != 1 {
		t.Fatalf("fetches = %d, want 1", n)
	}

	clk.add(FailureBackoff)
	got, err := j.Key(ctx, "k")
	if err != nil || !bytes.Equal(got, pub) {
		t.Fatalf("after backoff: %x, %v", got, err)
	}
	if FailureBackoff >= MinRefetchInterval {
		t.Fatalf("FailureBackoff %v must be shorter than MinRefetchInterval %v", FailureBackoff, MinRefetchInterval)
	}
}

// Keys expire after MaxKeyAge so a key the IdP retired stops being accepted.
func TestJWKSKeysExpireAndRetiredKeysAreDropped(t *testing.T) {
	oldPub, _ := newKey(t)
	newPub, _ := newKey(t)
	idp := newFakeIDP(t)
	idp.set(func(f *fakeIDP) { f.keys["old"] = oldPub })
	j, clk := newTestJWKS(idp.URL)
	ctx := context.Background()

	if _, err := j.Key(ctx, "old"); err != nil {
		t.Fatal(err)
	}
	idp.set(func(f *fakeIDP) { delete(f.keys, "old"); f.keys["new"] = newPub })

	clk.add(MaxKeyAge - time.Second)
	if _, err := j.Key(ctx, "old"); err != nil {
		t.Fatalf("still within MaxKeyAge: %v", err)
	}
	clk.add(time.Second)
	if _, err := j.Key(ctx, "old"); !errors.Is(err, ErrUnknownKID) {
		t.Fatalf("retired key after MaxKeyAge: err = %v, want ErrUnknownKID", err)
	}
	if got, err := j.Key(ctx, "new"); err != nil || !bytes.Equal(got, newPub) {
		t.Fatalf("new key: %x, %v", got, err)
	}
	if n := idp.fetches.Load(); n != 2 {
		t.Fatalf("fetches = %d, want 2", n)
	}
}

// If the IdP is down when keys expire, keep serving the cached set rather than
// rejecting every request (and retry after the short backoff).
func TestJWKSServesCachedKeysWhenRefreshFails(t *testing.T) {
	pub, _ := newKey(t)
	idp := newFakeIDP(t)
	idp.set(func(f *fakeIDP) { f.keys["k"] = pub })
	j, clk := newTestJWKS(idp.URL)
	ctx := context.Background()
	if _, err := j.Key(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	idp.set(func(f *fakeIDP) { f.status = http.StatusInternalServerError })
	clk.add(MaxKeyAge)
	if got, err := j.Key(ctx, "k"); err != nil || !bytes.Equal(got, pub) {
		t.Fatalf("cached key during outage: %x, %v", got, err)
	}
	if n := idp.fetches.Load(); n != 2 {
		t.Fatalf("fetches = %d, want 2 (one refresh attempt)", n)
	}
}

// The lock is not held during network I/O: cached lookups proceed while a slow
// refetch is in flight, and concurrent misses share one fetch.
func TestJWKSFetchDoesNotBlockCachedLookupsAndIsShared(t *testing.T) {
	pub, _ := newKey(t)
	idp := newFakeIDP(t)
	idp.set(func(f *fakeIDP) { f.keys["k"] = pub })
	j, clk := newTestJWKS(idp.URL)
	ctx := context.Background()
	if _, err := j.Key(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	clk.add(MinRefetchInterval)

	gate := make(chan struct{})
	idp.set(func(f *fakeIDP) { f.gate = gate })
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = j.Key(ctx, "unknown")
		}()
	}
	// Wait until the refetch is in flight.
	deadline := time.Now().Add(2 * time.Second)
	for idp.fetches.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	got := make(chan error, 1)
	go func() { _, err := j.Key(ctx, "k"); got <- err }()
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("cached lookup: %v", err)
		}
	case <-time.After(time.Second):
		close(gate)
		t.Fatal("cached lookup blocked behind an in-flight fetch")
	}
	close(gate)
	wg.Wait()
	if n := idp.fetches.Load(); n != 2 {
		t.Fatalf("fetches = %d, want 2 (concurrent misses share one fetch)", n)
	}
}

// A cancelled request must not poison the shared fetch for everyone else.
func TestJWKSFetchIgnoresCallerCancellation(t *testing.T) {
	pub, _ := newKey(t)
	idp := newFakeIDP(t)
	idp.set(func(f *fakeIDP) { f.keys["k"] = pub })
	j, _ := newTestJWKS(idp.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = j.Key(ctx, "k")
	if got, err := j.Key(context.Background(), "k"); err != nil || !bytes.Equal(got, pub) {
		t.Fatalf("after cancelled caller: %x, %v", got, err)
	}
}

func TestJWKSDocumentHandling(t *testing.T) {
	cases := []struct {
		name, doc string
		want      error
	}{
		{"wrong kty is skipped", `{"keys":[{"kty":"RSA","crv":"Ed25519","kid":"k","x":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}]}`, ErrUnknownKID},
		{"wrong crv is skipped", `{"keys":[{"kty":"OKP","crv":"X25519","kid":"k","x":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}]}`, ErrUnknownKID},
		{"short x", `{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"k","x":"AAAA"}]}`, ErrJWKSUnavailable},
		{"not base64url", `{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"k","x":"!!!"}]}`, ErrJWKSUnavailable},
		{"not json", `<html>`, ErrJWKSUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.doc)
			}))
			defer srv.Close()
			if _, err := NewJWKS(srv.URL, nil).Key(context.Background(), "k"); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestJWKSHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := NewJWKS(srv.URL, nil).Key(context.Background(), "k"); !errors.Is(err, ErrJWKSUnavailable) {
		t.Fatalf("err = %v, want ErrJWKSUnavailable", err)
	}
}

// A misconfigured or hijacked JWKS URL must not redirect key trust to another host.
func TestJWKSRefusesCrossHostRedirect(t *testing.T) {
	pub, _ := newKey(t)
	other := newFakeIDP(t)
	other.set(func(f *fakeIDP) { f.keys["k"] = pub })
	redirector := httptest.NewServer(http.RedirectHandler(other.URL, http.StatusFound))
	defer redirector.Close()
	if _, err := NewJWKS(redirector.URL, nil).Key(context.Background(), "k"); !errors.Is(err, ErrJWKSUnavailable) {
		t.Fatalf("err = %v, want ErrJWKSUnavailable", err)
	}
	if n := other.fetches.Load(); n != 0 {
		t.Fatalf("followed redirect to another host (%d fetches)", n)
	}
}
