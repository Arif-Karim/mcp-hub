package claudeconfig

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Regression (PR #4 review, must-fix 1): the retry fails after the first attempt
// already wrote the in-flight ledger. The ledger must be rolled back, otherwise a
// later Remove would delete an entry the user created.
func TestRetryFailureRollsBackLedger(t *testing.T) {
	w := newWriter(t)
	writeFile(t, w.ClaudeConfig, `{"mcpServers":{}}`, 0o600)
	userEntry := `{"mcpServers":{"hub__svc":{"type":"http","url":"http://127.0.0.1:7420/mcp/svc"}}}`
	calls := 0
	w.beforeRename = func() {
		calls++
		if calls == 1 {
			writeFile(t, w.ClaudeConfig, userEntry, 0o600)
		}
	}
	if err := w.Add("svc", entry("svc")); !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("err = %v, want ErrConfigConflict", err)
	}
	if owned, err := w.Owned(); err != nil || len(owned) != 0 {
		t.Fatalf("ledger must be rolled back, got %v (%v)", owned, err)
	}
	w.beforeRename = nil
	if err := w.Remove("svc"); !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("Remove of the user's entry: err = %v, want ErrConfigConflict", err)
	}
	if string(readBytes(t, w.ClaudeConfig)) != userEntry {
		t.Fatal("user's entry was deleted")
	}
}

func hashOf(e ServerEntry) string {
	b, _ := json.Marshal(e)
	return hashEntry(b)
}

// Review should-fix 3: while a URL change is in flight the ledger keeps the
// previous hash, so a crash before the config rename does not strand the entry.
func TestInFlightLedgerKeepsPrevHash(t *testing.T) {
	w := newWriter(t)
	a := entry("svc")
	b := ServerEntry{Type: "http", URL: "http://127.0.0.1:7421/mcp/svc"}
	if err := w.Add("svc", a); err != nil {
		t.Fatal(err)
	}
	var inflight Owned
	w.beforeRename = func() {
		owned, err := w.readLedger()
		if err != nil {
			t.Fatal(err)
		}
		inflight = owned["svc"]
	}
	if err := w.Add("svc", b); err != nil {
		t.Fatal(err)
	}
	if inflight.Hash != hashOf(b) || inflight.PrevHash != hashOf(a) {
		t.Fatalf("in-flight record = %+v", inflight)
	}
	owned, _ := w.Owned()
	if owned["svc"].Hash != hashOf(b) || owned["svc"].PrevHash != "" {
		t.Fatalf("settled record = %+v", owned["svc"])
	}
}

// crashMidURLChange leaves config holding entry a while the ledger already
// records b with PrevHash a — the state after a crash between ledger and rename.
func crashMidURLChange(t *testing.T) (w *Writer, a, b ServerEntry) {
	t.Helper()
	w = newWriter(t)
	a = entry("svc")
	b = ServerEntry{Type: "http", URL: "http://127.0.0.1:7421/mcp/svc"}
	if err := w.Add("svc", a); err != nil {
		t.Fatal(err)
	}
	owned, _ := w.readLedger()
	rec := owned["svc"]
	rec.Hash, rec.PrevHash, rec.URL = hashOf(b), hashOf(a), b.URL
	owned["svc"] = rec
	if err := w.writeLedger(owned); err != nil {
		t.Fatal(err)
	}
	return w, a, b
}

func TestCrashMidURLChangeRecovers(t *testing.T) {
	t.Run("Add new URL completes", func(t *testing.T) {
		w, _, b := crashMidURLChange(t)
		if err := w.Add("svc", b); err != nil {
			t.Fatal(err)
		}
		if compact(t, readServers(t, w.ClaudeConfig)["hub__svc"]) != `{"type":"http","url":"http://127.0.0.1:7421/mcp/svc"}` {
			t.Fatal("config not updated")
		}
		if o, _ := w.Owned(); o["svc"].Hash != hashOf(b) || o["svc"].PrevHash != "" {
			t.Fatalf("record = %+v", o["svc"])
		}
	})
	t.Run("Add old URL settles ledger without rewriting config", func(t *testing.T) {
		w, a, _ := crashMidURLChange(t)
		before := readBytes(t, w.ClaudeConfig)
		if err := w.Add("svc", a); err != nil {
			t.Fatal(err)
		}
		if string(readBytes(t, w.ClaudeConfig)) != string(before) || len(backups(t, w)) != 0 {
			t.Fatal("config rewritten")
		}
		if o, _ := w.Owned(); o["svc"].Hash != hashOf(a) || o["svc"].PrevHash != "" || o["svc"].URL != a.URL {
			t.Fatalf("record = %+v", o["svc"])
		}
	})
	t.Run("Remove still works", func(t *testing.T) {
		w, _, _ := crashMidURLChange(t)
		if err := w.Remove("svc"); err != nil {
			t.Fatal(err)
		}
		if len(readServers(t, w.ClaudeConfig)) != 0 {
			t.Fatal("entry not removed")
		}
	})
	t.Run("RemoveAll still works", func(t *testing.T) {
		w, _, _ := crashMidURLChange(t)
		if err := w.RemoveAll(); err != nil {
			t.Fatal(err)
		}
		if len(readServers(t, w.ClaudeConfig)) != 0 {
			t.Fatal("entry not removed")
		}
	})
	t.Run("unrelated write keeps the pending PrevHash", func(t *testing.T) {
		w, a, _ := crashMidURLChange(t)
		if err := w.Add("other", entry("other")); err != nil {
			t.Fatal(err)
		}
		if o, _ := w.Owned(); o["svc"].PrevHash != hashOf(a) {
			t.Fatalf("PrevHash dropped: %+v", o["svc"])
		}
	})
	t.Run("a third value is still a user edit", func(t *testing.T) {
		w, _, _ := crashMidURLChange(t)
		writeFile(t, w.ClaudeConfig, `{"mcpServers":{"hub__svc":{"type":"http","url":"http://x"}}}`, 0o600)
		if err := w.Remove("svc"); !errors.Is(err, ErrConfigConflict) {
			t.Fatalf("err = %v", err)
		}
	})
}

// Review nice-to-have 5: a frozen clock must not overwrite backups, and only
// files with our exact name layout are pruned.
func TestBackupNamesUniqueAndPruneOnlyOurs(t *testing.T) {
	w := newWriter(t)
	w.Now = func() time.Time { return t0 }
	writeFile(t, w.ClaudeConfig, userConfig, 0o600)
	if err := os.MkdirAll(w.BackupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(w.BackupDir, "claude.json.keep"), "mine", 0o600)
	for i := range 3 {
		e := ServerEntry{Type: "http", URL: "http://127.0.0.1:7420/mcp/svc?v=" + string(rune('a'+i))}
		if err := w.Add("svc", e); err != nil {
			t.Fatal(err)
		}
	}
	names := backups(t, w)
	if len(names) != 4 || names[3] != "claude.json.keep" {
		t.Fatalf("backups = %v", names)
	}
	if names[0] != "claude.json.20261101T090000.000000000Z" || !strings.HasPrefix(names[1], names[0]+"-") {
		t.Fatalf("names = %v", names)
	}
	for i := range 6 {
		e := ServerEntry{Type: "http", URL: "http://127.0.0.1:7420/mcp/svc?w=" + string(rune('a'+i))}
		if err := w.Add("svc", e); err != nil {
			t.Fatal(err)
		}
	}
	names = backups(t, w)
	if len(names) != maxBackups+1 || names[maxBackups] != "claude.json.keep" {
		t.Fatalf("after prune: %v", names)
	}
	// The newest backup holds the content just before the last write (?w=e).
	if !strings.Contains(string(readBytes(t, filepath.Join(w.BackupDir, names[maxBackups-1]))), "?w=e") {
		t.Fatalf("newest backup is not last: %v", names)
	}
}
