package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveHasSingleExecutableAtRoot(t *testing.T) {
	bin := []byte("\x7fELF fake binary")
	var buf bytes.Buffer
	if err := writeArchive(&buf, bin, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Name != "mcp-server" || hdr.Typeflag != tar.TypeReg || hdr.Mode != 0o755 {
		t.Fatalf("header = name %q type %c mode %o", hdr.Name, hdr.Typeflag, hdr.Mode)
	}
	if hdr.Uid != 0 || hdr.Gid != 0 || hdr.Uname != "" || !hdr.ModTime.Equal(time.Unix(0, 0)) {
		t.Fatalf("non-reproducible metadata: %+v", hdr)
	}
	got, err := io.ReadAll(tr)
	if err != nil || !bytes.Equal(got, bin) {
		t.Fatalf("content = %q, %v", got, err)
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Fatalf("expected exactly one entry, next err = %v", err)
	}
}

func TestArchiveIsReproducible(t *testing.T) {
	var a, b bytes.Buffer
	mtime := time.Unix(1_700_000_000, 0)
	if err := writeArchive(&a, []byte("same"), mtime); err != nil {
		t.Fatal(err)
	}
	if err := writeArchive(&b, []byte("same"), mtime); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("archives differ for identical input")
	}
}

func TestRunWritesFile(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "bin")
	out := filepath.Join(dir, "out.tar.gz")
	if err := os.WriteFile(in, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{in, out}, "0"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
		t.Fatalf("output missing: %v", err)
	}
	if err := run([]string{in}, "0"); err == nil {
		t.Fatal("expected usage error")
	}
	if err := run([]string{in, out}, "yesterday"); err == nil {
		t.Fatal("expected SOURCE_DATE_EPOCH error")
	}
}
