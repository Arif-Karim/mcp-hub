package registry

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// unpack extracts a .tar.gz into dst (a fresh, empty directory). Only plain
// directories and regular files are allowed: absolute paths, ".." traversal,
// symlinks, hardlinks and device/fifo entries are rejected, and the total
// unpacked size and entry count are capped (decompression bombs, inode
// exhaustion). pax global headers (emitted by git archive) carry no file data
// and are skipped.
func unpack(r io.Reader, dst string, maxBytes int64, maxEntries int) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("%w: not a gzip stream: %w", ErrUnsafeArchive, err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var total int64
	entries := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: corrupt tar: %w", ErrUnsafeArchive, err)
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if entries++; entries > maxEntries {
			return fmt.Errorf("%w: more than %d archive entries", ErrTooLarge, maxEntries)
		}
		name := filepath.FromSlash(hdr.Name)
		if !filepath.IsLocal(name) {
			return fmt.Errorf("%w: entry %q escapes the install dir", ErrUnsafeArchive, hdr.Name)
		}
		target := filepath.Join(dst, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return fmt.Errorf("unpack %q: %w", hdr.Name, err)
			}
		case tar.TypeReg:
			n, err := writeFile(target, tr, hdr.Mode, maxBytes-total)
			total += n
			if err != nil {
				return fmt.Errorf("unpack %q: %w", hdr.Name, err)
			}
		default:
			return fmt.Errorf("%w: entry %q has unsupported type %q", ErrUnsafeArchive, hdr.Name, hdr.Typeflag)
		}
	}
}

// writeFile copies at most budget bytes from r into a new file at target,
// 0755 if any execute bit is set in mode, else 0644.
func writeFile(target string, r io.Reader, mode, budget int64) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return 0, err
	}
	perm := os.FileMode(0o644)
	if mode&0o111 != 0 {
		perm = 0o755
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) // #nosec G302 G304 -- target checked by filepath.IsLocal; exec bit is required for server binaries
	if err != nil {
		return 0, err
	}
	n, err := io.CopyN(f, r, budget+1)
	if err != nil && !errors.Is(err, io.EOF) {
		_ = f.Close()
		return n, err
	}
	if n > budget {
		_ = f.Close()
		return n, fmt.Errorf("%w: unpacked size exceeds limit", ErrTooLarge)
	}
	// Chmod explicitly so the result does not depend on the caller's umask.
	return n, errors.Join(f.Chmod(perm), f.Close())
}
