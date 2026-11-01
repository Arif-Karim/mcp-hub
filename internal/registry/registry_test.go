package registry_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/registry"
)

type entry struct {
	hdr  tar.Header
	body string
}

func file(name, body string, mode int64) entry {
	return entry{tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}, body}
}

func buildTarGz(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := e.hdr
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// serve returns a registry serving body and a counter of requests made.
func serve(t *testing.T, status int, body []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func installer(t *testing.T, srv *httptest.Server) (*registry.Installer, string) {
	dir := t.TempDir()
	return &registry.Installer{Client: srv.Client(), Dir: dir}, dir
}

// entries lists everything under dir (relative paths), to prove nothing was left behind.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func assertClean(t *testing.T, dir string) {
	t.Helper()
	for _, p := range entries(t, dir) {
		if p != "svc" { // the per-id parent may exist, but must be empty
			t.Errorf("left behind: %s", p)
		}
	}
}

func TestPlatformKey(t *testing.T) {
	if got, want := registry.PlatformKey(), runtime.GOOS+"-"+runtime.GOARCH; got != want {
		t.Fatalf("PlatformKey = %q, want %q", got, want)
	}
}

func TestSelect(t *testing.T) {
	a := catalogue.Artifact{URL: "https://x.test/a", SHA256: "ab"}
	e := catalogue.Entry{ID: "svc", Artifacts: map[string]catalogue.Artifact{"linux-amd64": a}}
	got, err := registry.Select(e, "linux-amd64")
	if err != nil || got != a {
		t.Fatalf("Select = %+v, %v", got, err)
	}
	if _, err := registry.Select(e, "darwin-arm64"); !errors.Is(err, registry.ErrUnsupportedPlatform) {
		t.Fatalf("err = %v, want ErrUnsupportedPlatform", err)
	}
}

func TestInstallHappyPath(t *testing.T) {
	body := buildTarGz(t,
		entry{tar.Header{Name: "bin/", Mode: 0o755, Typeflag: tar.TypeDir}, ""},
		file("bin/server", "#!/bin/sh\necho hi\n", 0o755),
		file("README.md", "readme", 0o600),
	)
	srv, _ := serve(t, http.StatusOK, body)
	in, root := installer(t, srv)

	dir, err := in.Install(context.Background(), "svc", "0.1.0", catalogue.Artifact{URL: srv.URL + "/a.tar.gz", SHA256: sum(body)})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if want := filepath.Join(root, "svc", "0.1.0"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	got, err := os.ReadFile(filepath.Join(dir, "bin", "server"))
	if err != nil || string(got) != "#!/bin/sh\necho hi\n" {
		t.Fatalf("server content = %q, %v", got, err)
	}
	checkMode(t, filepath.Join(dir, "bin", "server"), 0o755)
	checkMode(t, filepath.Join(dir, "README.md"), 0o644)
	checkMode(t, dir, 0o700)
	checkMode(t, filepath.Join(dir, "bin"), 0o700)
	marker, _ := os.ReadFile(filepath.Join(dir, ".sha256"))
	if string(marker) != sum(body) {
		t.Fatalf(".sha256 marker = %q", marker)
	}
	// Only the final install remains: no temp downloads or staging dirs.
	if got := entries(t, filepath.Join(root, "svc")); len(got) != 5 { // 0.1.0, .sha256, bin, bin/server, README.md
		t.Fatalf("unexpected files: %v", got)
	}
}

func checkMode(t *testing.T, p string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", filepath.Base(p), got, want)
	}
}

func TestInstallChecksumMismatch(t *testing.T) {
	body := buildTarGz(t, file("server", "x", 0o755))
	srv, _ := serve(t, http.StatusOK, body)
	in, root := installer(t, srv)

	_, err := in.Install(context.Background(), "svc", "1", catalogue.Artifact{URL: srv.URL, SHA256: sum([]byte("other"))})
	if !errors.Is(err, registry.ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
	assertClean(t, root)
}

func TestInstallRegistryUnavailable(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusMultipleChoices} {
		srv, _ := serve(t, status, []byte("nope"))
		in, root := installer(t, srv)
		_, err := in.Install(context.Background(), "svc", "1", catalogue.Artifact{URL: srv.URL, SHA256: sum([]byte("nope"))})
		if !errors.Is(err, registry.ErrRegistryUnavailable) {
			t.Errorf("status %d: err = %v, want ErrRegistryUnavailable", status, err)
		}
		assertClean(t, root)
	}
}

func TestInstallClosedServer(t *testing.T) {
	srv, _ := serve(t, http.StatusOK, nil)
	in, root := installer(t, srv)
	srv.Close()
	_, err := in.Install(context.Background(), "svc", "1", catalogue.Artifact{URL: srv.URL, SHA256: sum(nil)})
	if !errors.Is(err, registry.ErrRegistryUnavailable) {
		t.Fatalf("err = %v, want ErrRegistryUnavailable", err)
	}
	assertClean(t, root)
}

func TestInstallRejectsUnsafeArchives(t *testing.T) {
	cases := map[string][]entry{
		"dotdot traversal": {file("../evil", "x", 0o644)},
		"nested traversal": {file("bin/../../evil", "x", 0o644)},
		"absolute path":    {file("/tmp/evil", "x", 0o644)},
		"symlink":          {{tar.Header{Name: "link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink}, ""}},
		"hardlink":         {{tar.Header{Name: "link", Linkname: "server", Typeflag: tar.TypeLink}, ""}},
		"fifo":             {{tar.Header{Name: "pipe", Mode: 0o644, Typeflag: tar.TypeFifo}, ""}},
		"bad after good":   {file("ok", "x", 0o644), file("../evil", "x", 0o644)},
	}
	for name, es := range cases {
		t.Run(name, func(t *testing.T) {
			body := buildTarGz(t, es...)
			srv, _ := serve(t, http.StatusOK, body)
			in, root := installer(t, srv)
			_, err := in.Install(context.Background(), "svc", "1", catalogue.Artifact{URL: srv.URL, SHA256: sum(body)})
			if !errors.Is(err, registry.ErrUnsafeArchive) {
				t.Fatalf("err = %v, want ErrUnsafeArchive", err)
			}
			assertClean(t, root)
			if _, err := os.Stat(filepath.Join(filepath.Dir(root), "evil")); err == nil {
				t.Fatal("traversal wrote outside the install dir")
			}
		})
	}
}

func TestInstallRejectsCorruptArchive(t *testing.T) {
	body := []byte("not a gzip stream")
	srv, _ := serve(t, http.StatusOK, body)
	in, root := installer(t, srv)
	_, err := in.Install(context.Background(), "svc", "1", catalogue.Artifact{URL: srv.URL, SHA256: sum(body)})
	if !errors.Is(err, registry.ErrUnsafeArchive) {
		t.Fatalf("err = %v, want ErrUnsafeArchive", err)
	}
	assertClean(t, root)
}

func TestInstallRejectsOversizeDownload(t *testing.T) {
	body := buildTarGz(t, file("server", "0123456789", 0o755))
	srv, _ := serve(t, http.StatusOK, body)
	in, root := installer(t, srv)
	in.MaxBytes = int64(len(body)) - 1
	_, err := in.Install(context.Background(), "svc", "1", catalogue.Artifact{URL: srv.URL, SHA256: sum(body)})
	if !errors.Is(err, registry.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	assertClean(t, root)

	// Exactly at the limit is fine.
	in.MaxBytes = int64(len(body))
	if _, err := in.Install(context.Background(), "svc", "1", catalogue.Artifact{URL: srv.URL, SHA256: sum(body)}); err != nil {
		t.Fatalf("at limit: %v", err)
	}
}

func TestInstallRejectsDecompressionBomb(t *testing.T) {
	body := buildTarGz(t, file("server", string(make([]byte, 1<<20)), 0o755))
	srv, _ := serve(t, http.StatusOK, body)
	in, root := installer(t, srv)
	in.MaxBytes = 64 << 10
	if int64(len(body)) > in.MaxBytes {
		t.Fatalf("test archive unexpectedly large: %d", len(body))
	}
	_, err := in.Install(context.Background(), "svc", "1", catalogue.Artifact{URL: srv.URL, SHA256: sum(body)})
	if !errors.Is(err, registry.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	assertClean(t, root)
}

func zeros(n int) string { return string(make([]byte, n)) }

func TestInstallUnpackedSizeBudget(t *testing.T) {
	const limit = 8 << 10
	cases := []struct {
		name  string
		files []entry
		ok    bool
	}{
		{"one file at limit", []entry{file("a", zeros(limit), 0o644)}, true},
		{"one file over limit", []entry{file("a", zeros(limit+1), 0o644)}, false},
		{"files summing to limit", []entry{file("a", zeros(limit/2), 0o644), file("b", zeros(limit/2), 0o644)}, true},
		{"files summing over limit", []entry{file("a", zeros(limit/2), 0o644), file("b", zeros(limit/2+1), 0o644)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := buildTarGz(t, tc.files...)
			srv, _ := serve(t, http.StatusOK, body)
			in, root := installer(t, srv)
			in.MaxBytes = limit
			_, err := in.Install(context.Background(), "svc", "1", catalogue.Artifact{URL: srv.URL, SHA256: sum(body)})
			if tc.ok && err != nil {
				t.Fatalf("err = %v, want success", err)
			}
			if !tc.ok {
				if !errors.Is(err, registry.ErrTooLarge) {
					t.Fatalf("err = %v, want ErrTooLarge", err)
				}
				assertClean(t, root)
			}
		})
	}
}

func TestInstallDefaultsClientAndLimit(t *testing.T) {
	body := buildTarGz(t, file("server", "x", 0o755))
	srv, _ := serve(t, http.StatusOK, body)
	in := &registry.Installer{Dir: t.TempDir()} // nil Client, zero MaxBytes
	if _, err := in.Install(context.Background(), "svc", "1", catalogue.Artifact{URL: srv.URL, SHA256: sum(body)}); err != nil {
		t.Fatalf("Install with defaults: %v", err)
	}
	if registry.DefaultMaxBytes != 100<<20 {
		t.Fatalf("DefaultMaxBytes = %d, want 100 MiB", registry.DefaultMaxBytes)
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	body := buildTarGz(t, file("server", "x", 0o755))
	srv, hits := serve(t, http.StatusOK, body)
	in, _ := installer(t, srv)
	a := catalogue.Artifact{URL: srv.URL, SHA256: sum(body)}

	first, err := in.Install(context.Background(), "svc", "1", a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := in.Install(context.Background(), "svc", "1", a)
	if err != nil || second != first {
		t.Fatalf("second Install = %q, %v; want %q", second, err, first)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("registry hit %d times, want 1", n)
	}
}

func TestInstallReplacesStaleInstall(t *testing.T) {
	body := buildTarGz(t, file("server", "new", 0o755))
	srv, hits := serve(t, http.StatusOK, body)
	in, root := installer(t, srv)
	stale := filepath.Join(root, "svc", "1")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, ".sha256"), []byte(sum([]byte("old"))), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "server"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	dir, err := in.Install(context.Background(), "svc", "1", catalogue.Artifact{URL: srv.URL, SHA256: sum(body)})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "server"))
	if string(got) != "new" || hits.Load() != 1 {
		t.Fatalf("server = %q, hits = %d; want fresh install", got, hits.Load())
	}
}

func TestInstallHonoursContext(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(block); srv.Close() })
	in, root := installer(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := in.Install(ctx, "svc", "1", catalogue.Artifact{URL: srv.URL, SHA256: sum(nil)})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, registry.ErrRegistryUnavailable) {
		t.Fatalf("err = %v, want context.Canceled wrapped in ErrRegistryUnavailable", err)
	}
	assertClean(t, root)
}

func TestInstallRejectsUnsafeIDOrVersion(t *testing.T) {
	srv, hits := serve(t, http.StatusOK, nil)
	in, _ := installer(t, srv)
	for _, iv := range [][2]string{{"..", "1"}, {"svc", "../x"}, {"svc", "a/b"}, {"", "1"}, {"svc", ""}} {
		_, err := in.Install(context.Background(), iv[0], iv[1], catalogue.Artifact{URL: srv.URL, SHA256: sum(nil)})
		if !errors.Is(err, registry.ErrUnsafeArchive) {
			t.Errorf("%v: err = %v, want ErrUnsafeArchive", iv, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("registry contacted for an unsafe id/version")
	}
}
