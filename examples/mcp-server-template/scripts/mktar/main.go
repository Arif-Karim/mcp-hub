// Command mktar writes a reproducible release archive: a gzipped tar containing a
// single executable named "mcp-server" at the root (mode 0755, uid/gid 0, fixed mtime).
//
//	go run ./scripts/mktar <binary> <out.tar.gz>
//
// The mtime comes from SOURCE_DATE_EPOCH (default 0).
package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
)

// EntryName is the executable name the hub expects at the archive root.
const EntryName = "mcp-server"

func main() {
	if err := run(os.Args[1:], os.Getenv("SOURCE_DATE_EPOCH")); err != nil {
		fmt.Fprintln(os.Stderr, "mktar:", err)
		os.Exit(1)
	}
}

func run(args []string, epoch string) error {
	if len(args) != 2 {
		return errors.New("usage: mktar <binary> <out.tar.gz>")
	}
	secs := int64(0)
	if epoch != "" {
		var err error
		if secs, err = strconv.ParseInt(epoch, 10, 64); err != nil {
			return fmt.Errorf("SOURCE_DATE_EPOCH: %w", err)
		}
	}
	bin, err := os.ReadFile(args[0]) //nolint:gosec // G703: build-time CLI, paths come from package.sh
	if err != nil {
		return err
	}
	f, err := os.Create(args[1]) //nolint:gosec // G703: build-time CLI, paths come from package.sh
	if err != nil {
		return err
	}
	if err := writeArchive(f, bin, time.Unix(secs, 0)); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func writeArchive(w io.Writer, bin []byte, mtime time.Time) error {
	gz, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	gz.ModTime = mtime // gzip header has no name/OS-specific data beyond this
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     EntryName,
		Mode:     0o755,
		Size:     int64(len(bin)),
		ModTime:  mtime,
		Format:   tar.FormatUSTAR,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(bin); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}
