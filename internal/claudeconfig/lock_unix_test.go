//go:build unix

package claudeconfig

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Review should-fix 2: another process (the CLI next to the daemon) holding the
// advisory lock blocks our read-modify-write until it releases.
func TestOperationsWaitForCrossProcessLock(t *testing.T) {
	w := newWriter(t)
	if err := w.Add("svc", entry("svc")); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(w.Ledger), "claudeconfig.lock")
	if m := mode(t, lockPath); m != 0o600 {
		t.Fatalf("lock file mode = %o", m)
	}
	f, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.Add("other", entry("other")) }()
	select {
	case err := <-done:
		t.Fatalf("Add did not wait for the lock (err %v)", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Add still blocked after unlock")
	}
	if _, ok := readServers(t, w.ClaudeConfig)["hub__other"]; !ok {
		t.Fatal("entry missing")
	}
}
