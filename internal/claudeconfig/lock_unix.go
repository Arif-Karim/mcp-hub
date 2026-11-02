//go:build unix

package claudeconfig

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// lockFile takes an exclusive advisory flock on path (created 0600), blocking
// until any other process (e.g. the CLI next to the daemon) releases it.
func lockFile(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- lock file under MCP_HUB_HOME
	if err != nil {
		return nil, err
	}
	fd := int(f.Fd()) // #nosec G115 -- file descriptors fit in int
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// chownLike gives the temp file the original's owner (best effort), so running
// under sudo does not leave ~/.claude.json owned by root.
func chownLike(f *os.File, orig fs.FileInfo) {
	if st, ok := orig.Sys().(*syscall.Stat_t); ok {
		_ = f.Chown(int(st.Uid), int(st.Gid))
	}
}
