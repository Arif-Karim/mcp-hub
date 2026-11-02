//go:build !unix

package claudeconfig

import (
	"io/fs"
	"os"
)

// lockFile is a no-op off unix; only the in-process mutex applies.
func lockFile(string) (func(), error) { return func() {}, nil }

func chownLike(*os.File, fs.FileInfo) {}
