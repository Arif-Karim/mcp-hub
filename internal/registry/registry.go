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
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
)

// Sentinel errors; the API maps them to contract error codes.
var (
	ErrUnsupportedPlatform = errors.New("no artifact for this platform")
	ErrRegistryUnavailable = errors.New("registry unavailable")
	ErrChecksumMismatch    = errors.New("artifact checksum mismatch")
	ErrTooLarge            = errors.New("artifact too large")
	ErrUnsafeArchive       = errors.New("unsafe archive or install path")
)

// DefaultMaxBytes caps both the download and the total unpacked size.
const DefaultMaxBytes int64 = 100 << 20

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
type Installer struct {
	Client   *http.Client // nil: a client with a 10 minute timeout
	Dir      string
	MaxBytes int64 // <= 0: DefaultMaxBytes
}

// Install makes the verified artifact available at Dir/<id>/<version> and
// returns that path. Nothing is unpacked unless the sha256 matches, and a
// failed install leaves nothing behind. It is idempotent: an existing install
// with a matching .sha256 marker is returned without contacting the registry.
func (in *Installer) Install(ctx context.Context, id, version string, a catalogue.Artifact) (string, error) {
	if !safeSegment(id) || !safeSegment(version) {
		return "", fmt.Errorf("%w: id %q / version %q", ErrUnsafeArchive, id, version)
	}
	parent := filepath.Join(in.Dir, id)
	final := filepath.Join(parent, version)
	if marker, err := os.ReadFile(filepath.Join(final, markerFile)); err == nil && string(marker) == a.SHA256 { // #nosec G304 -- path built from validated segments
		return final, nil
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("create install dir: %w", err)
	}

	tmp, err := os.CreateTemp(parent, ".download-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if err := in.fetch(ctx, a, tmp); err != nil {
		return "", err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind download: %w", err)
	}

	staging, err := os.MkdirTemp(parent, ".staging-*")
	if err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}
	done := false
	defer func() {
		if !done {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := unpack(tmp, staging, in.maxBytes()); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(staging, markerFile), []byte(a.SHA256), 0o600); err != nil {
		return "", fmt.Errorf("write marker: %w", err)
	}
	// A stale install (different hash) is replaced; the rename itself is atomic.
	if err := os.RemoveAll(final); err != nil {
		return "", fmt.Errorf("remove stale install: %w", err)
	}
	if err := os.Rename(staging, final); err != nil {
		return "", fmt.Errorf("activate install: %w", err)
	}
	done = true
	return final, nil
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

func (in *Installer) client() *http.Client {
	if in.Client != nil {
		return in.Client
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (in *Installer) maxBytes() int64 {
	if in.MaxBytes > 0 {
		return in.MaxBytes
	}
	return DefaultMaxBytes
}

// safeSegment reports whether s can be used as a single path element.
func safeSegment(s string) bool {
	return s != "" && s != "." && !strings.ContainsAny(s, `/\`) && filepath.IsLocal(s)
}
