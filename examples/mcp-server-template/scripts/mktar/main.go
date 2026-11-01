// Command mktar writes a reproducible release archive: a gzipped tar containing a
// single executable named "mcp-server" at the root (mode 0755, uid/gid 0, fixed mtime).
//
//	go run ./scripts/mktar <binary> <out.tar.gz>
//
// The mtime comes from SOURCE_DATE_EPOCH (default 0).
package main

import (
	"errors"
	"io"
	"os"
	"time"
)

func main() {}

func run(_ []string, _ string) error { return errors.New("not implemented") }

func writeArchive(_ io.Writer, _ []byte, _ time.Time) error { return errors.New("not implemented") }

var _ = os.Args
