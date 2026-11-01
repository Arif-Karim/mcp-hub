// Package registry installs local MCP server artifacts: it downloads a
// platform build, verifies its sha256 against the catalogue before touching
// it, and unpacks it safely into MCP_HUB_HOME.
package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
)

// Sentinel errors; the API maps them to contract error codes.
var (
	ErrUnsupportedPlatform = errors.New("no artifact for this platform")
	ErrRegistryUnavailable = errors.New("registry unavailable")
	ErrChecksumMismatch    = errors.New("artifact checksum mismatch")
	// ErrArtifactRejected (contract code artifact_rejected) is the parent of
	// ErrTooLarge and ErrUnsafeArchive.
	ErrArtifactRejected = errors.New("artifact rejected")
	ErrTooLarge         = fmt.Errorf("%w: too large", ErrArtifactRejected)
	ErrUnsafeArchive    = fmt.Errorf("%w: unsafe archive or install path", ErrArtifactRejected)
)

// Defaults for Installer limits.
const (
	// DefaultMaxBytes caps both the download and the total unpacked size.
	DefaultMaxBytes int64 = 100 << 20
	// DefaultMaxEntries caps archive entries (inode/time exhaustion).
	DefaultMaxEntries = 10_000
)

const markerFile = ".sha256"

// PlatformKey is this machine's artifact key, e.g. "darwin-arm64".
func PlatformKey() string { return runtime.GOOS + "-" + runtime.GOARCH }

// Select picks the entry's artifact for platform.
func Select(e catalogue.Entry, platform string) (catalogue.Artifact, error) {
	a, ok := e.Artifacts[platform]
	if !ok {
		return catalogue.Artifact{}, fmt.Errorf("%w: %s has no %s build", ErrUnsupportedPlatform, e.ID, platform)
	}
	return a, nil
}

// Installer downloads and unpacks artifacts into Dir/<id>/<version>.
//
// Installs of the same id/version are serialised within this process. Two
// processes sharing Dir are not coordinated (no file lock) — a known gap.
type Installer struct {
	Client     *http.Client // nil: a client with a 10 minute timeout
	Dir        string
	MaxBytes   int64 // <= 0: DefaultMaxBytes
	MaxEntries int   // <= 0: DefaultMaxEntries

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// Install makes the verified artifact available at Dir/<id>/<version> and
// returns that path. Nothing is unpacked unless the sha256 matches, and a
// failed install leaves nothing behind. It is idempotent: an existing install
// with a matching .sha256 marker is returned without contacting the registry.
//
// Scratch files (download, staging, the replaced install) live in
// Dir/<id>/.work-<version>, which is wiped before and after each install, so
// leftovers from a crashed install are cleaned up by the next one.
func (in *Installer) Install(ctx context.Context, id, version string, a catalogue.Artifact) (string, error) {
	if !safeSegment(id) || !safeSegment(version) {
		return "", fmt.Errorf("%w: id %q / version %q", ErrUnsafeArchive, id, version)
	}
	defer in.lock(id + "/" + version)()

	parent := filepath.Join(in.Dir, id)
	final := filepath.Join(parent, version)
	if marker, err := os.ReadFile(filepath.Join(final, markerFile)); err == nil && string(marker) == a.SHA256 { // #nosec G304 -- path built from validated segments
		return final, nil
	}
	work := filepath.Join(parent, ".work-"+version)
	if err := os.RemoveAll(work); err != nil {
		return "", fmt.Errorf("sweep work dir: %w", err)
	}
	if err := os.MkdirAll(work, 0o700); err != nil {
		return "", fmt.Errorf("create work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(work) }()

	tmp, err := os.Create(filepath.Join(work, "download")) // #nosec G304 -- path built from validated segments
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = tmp.Close() }()
	if err := in.fetch(ctx, a, tmp); err != nil {
		return "", err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind download: %w", err)
	}

	staging := filepath.Join(work, "staging")
	if err := os.Mkdir(staging, 0o700); err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}
	if err := unpack(tmp, staging, in.maxBytes(), in.maxEntries()); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(staging, markerFile), []byte(a.SHA256), 0o600); err != nil {
		return "", fmt.Errorf("write marker: %w", err)
	}
	// Replace a stale install with two atomic renames: old aside, new in. A
	// crash in between leaves no install (re-done next time), never a
	// half-deleted tree that still carries a marker.
	if err := os.Rename(final, filepath.Join(work, "old")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("move stale install aside: %w", err)
	}
	if err := os.Rename(staging, final); err != nil {
		return "", fmt.Errorf("activate install: %w", err)
	}
	return final, nil
}

// lock serialises installs of one id/version and returns the unlock func.
func (in *Installer) lock(key string) func() {
	in.mu.Lock()
	if in.locks == nil {
		in.locks = map[string]*sync.Mutex{}
	}
	l, ok := in.locks[key]
	if !ok {
		l = &sync.Mutex{}
		in.locks[key] = l
	}
	in.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// fetch streams the artifact into w, enforcing the size cap and sha256.
func (in *Installer) fetch(ctx context.Context, a catalogue.Artifact, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRegistryUnavailable, err)
	}
	resp, err := in.client().Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRegistryUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%w: registry returned HTTP %d", ErrRegistryUnavailable, resp.StatusCode)
	}
	limit := in.maxBytes()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("%w: download: %w", ErrRegistryUnavailable, err)
	}
	if n > limit {
		return fmt.Errorf("%w: download exceeds %d bytes", ErrTooLarge, limit)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		return fmt.Errorf("%w: got %s, catalogue says %s", ErrChecksumMismatch, got, a.SHA256)
	}
	return nil
}

// client returns a copy of the configured client that, unless the caller set
// its own policy, refuses https -> http redirect downgrades.
func (in *Installer) client() *http.Client {
	c := http.Client{Timeout: 10 * time.Minute}
	if in.Client != nil {
		c = *in.Client
	}
	if c.CheckRedirect == nil {
		c.CheckRedirect = noDowngrade
	}
	return &c
}

func noDowngrade(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("refusing redirect from https to %s", req.URL.Scheme)
	}
	return nil
}

func (in *Installer) maxBytes() int64 {
	if in.MaxBytes > 0 {
		return in.MaxBytes
	}
	return DefaultMaxBytes
}

func (in *Installer) maxEntries() int {
	if in.MaxEntries > 0 {
		return in.MaxEntries
	}
	return DefaultMaxEntries
}

// safeSegment reports whether s can be used as a single path element. Leading
// dots are refused so a version can't collide with a ".work-*" scratch dir.
func safeSegment(s string) bool {
	return s != "" && !strings.HasPrefix(s, ".") && !strings.ContainsAny(s, `/\`) && filepath.IsLocal(s)
}
