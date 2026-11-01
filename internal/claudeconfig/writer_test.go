package claudeconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

// newWriter returns a Writer whose paths all live under t.TempDir() and whose
// clock advances one second per call.
func newWriter(t *testing.T) *Writer {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	n := 0
	return &Writer{
		Paths: Paths{
			ClaudeConfig: filepath.Join(home, ".claude.json"),
			Ledger:       filepath.Join(dir, "hub", "state.json"),
			BackupDir:    filepath.Join(dir, "hub", "backups"),
		},
		Now: func() time.Time { n++; return t0.Add(time.Duration(n) * time.Second) },
	}
}

func entry(id string) ServerEntry {
	return ServerEntry{Type: "http", URL: "http://127.0.0.1:7420/mcp/" + id}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readTop(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(readBytes(t, path), &top); err != nil {
		t.Fatalf("config is not valid JSON: %v", err)
	}
	return top
}

func readServers(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	var s map[string]json.RawMessage
	if err := json.Unmarshal(readTop(t, path)["mcpServers"], &s); err != nil {
		t.Fatalf("mcpServers: %v", err)
	}
	return s
}

func compact(t *testing.T, raw []byte) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s should not exist (err=%v)", path, err)
	}
}

func backups(t *testing.T, w *Writer) []string {
	t.Helper()
	ents, err := os.ReadDir(w.BackupDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("leftover temp file %s", e.Name())
		}
	}
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

const userConfig = `{
  "numStartups": 42,
  "projects": {"/work/app": {"allowedTools": ["Bash"], "note": "<b>&amp</b>", "big": 12345678901234567890, "ratio": 1.50}},
  "theme": "dark",
  "mcpServers": {
    "github": {"type": "stdio", "command": "gh-mcp", "env": {"GH_HOST": "github.example"}},
    "hub__mine": {"type": "http", "url": "http://localhost:9999/mine"}
  }
}`

func TestAddCreatesMissingConfig(t *testing.T) {
	w := newWriter(t)
	if err := w.Add("service-directory", entry("service-directory")); err != nil {
		t.Fatal(err)
	}
	top := readTop(t, w.ClaudeConfig)
	if len(top) != 1 {
		t.Fatalf("new config should only hold mcpServers, got %v", top)
	}
	servers := readServers(t, w.ClaudeConfig)
	want := `{"type":"http","url":"http://127.0.0.1:7420/mcp/service-directory"}`
	if len(servers) != 1 || compact(t, servers["hub__service-directory"]) != want {
		t.Fatalf("servers = %s", top["mcpServers"])
	}
	if m := mode(t, w.ClaudeConfig); m != 0o600 {
		t.Fatalf("new config mode = %o, want 600", m)
	}
	if !bytes.Contains(readBytes(t, w.ClaudeConfig), []byte("\n  \"mcpServers\": {\n    \"hub__")) {
		t.Fatalf("output not 2-space indented:\n%s", readBytes(t, w.ClaudeConfig))
	}
	if b := backups(t, w); len(b) != 0 {
		t.Fatalf("no backup expected for a new file, got %v", b)
	}

	owned, err := w.Owned()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(want))
	rec := owned["service-directory"]
	if len(owned) != 1 || rec.ID != "service-directory" || rec.ConfigName != "hub__service-directory" ||
		rec.URL != "http://127.0.0.1:7420/mcp/service-directory" || rec.Hash != hex.EncodeToString(sum[:]) ||
		!rec.EnabledAt.Equal(t0.Add(time.Second)) {
		t.Fatalf("ledger record = %+v", owned)
	}
	if m := mode(t, w.Ledger); m != 0o600 {
		t.Fatalf("ledger mode = %o", m)
	}
	if m := mode(t, filepath.Dir(w.Ledger)); m != 0o700 {
		t.Fatalf("ledger dir mode = %o", m)
	}
	var ledger struct {
		Version int                        `json:"version"`
		Servers map[string]json.RawMessage `json:"servers"`
	}
	if err := json.Unmarshal(readBytes(t, w.Ledger), &ledger); err != nil || ledger.Version != 1 || len(ledger.Servers) != 1 {
		t.Fatalf("ledger = %s (err %v)", readBytes(t, w.Ledger), err)
	}
	assertNoTempFiles(t, filepath.Dir(w.ClaudeConfig))
	assertNoTempFiles(t, filepath.Dir(w.Ledger))
}

func TestAddPreservesUnrelatedKeysAndEntries(t *testing.T) {
	w := newWriter(t)
	writeFile(t, w.ClaudeConfig, userConfig, 0o600)
	var origTop, origServers map[string]json.RawMessage
	_ = json.Unmarshal([]byte(userConfig), &origTop)
	_ = json.Unmarshal(origTop["mcpServers"], &origServers)

	if err := w.Add("svc", entry("svc")); err != nil {
		t.Fatal(err)
	}
	top := readTop(t, w.ClaudeConfig)
	for k, v := range origTop {
		if k == "mcpServers" {
			continue
		}
		if compact(t, top[k]) != compact(t, v) {
			t.Fatalf("top-level %q changed:\n got %s\nwant %s", k, top[k], v)
		}
	}
	if len(top) != len(origTop) {
		t.Fatalf("top-level keys changed: %d vs %d", len(top), len(origTop))
	}
	servers := readServers(t, w.ClaudeConfig)
	for k, v := range origServers {
		if compact(t, servers[k]) != compact(t, v) {
			t.Fatalf("mcpServers %q changed:\n got %s\nwant %s", k, servers[k], v)
		}
	}
	if len(servers) != len(origServers)+1 {
		t.Fatalf("servers = %v", servers)
	}
	// No HTML escaping, no number reformatting.
	if out := readBytes(t, w.ClaudeConfig); !bytes.Contains(out, []byte(`"<b>&amp</b>"`)) || !bytes.Contains(out, []byte("12345678901234567890")) {
		t.Fatalf("values were re-encoded:\n%s", out)
	}
}

func TestInvalidConfigIsNeverWritten(t *testing.T) {
	cases := map[string]string{
		"unparseable":          `{"mcpServers": {`,
		"empty":                ``,
		"array":                `[]`,
		"string":               `"x"`,
		"null":                 `null`,
		"mcpServers array":     `{"mcpServers": []}`,
		"mcpServers string":    `{"mcpServers": "x"}`,
		"mcpServers null":      `{"mcpServers": null}`,
		"mcpServers wrong val": `{"mcpServers": 3}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWriter(t)
			writeFile(t, w.ClaudeConfig, content, 0o644)
			for op, f := range map[string]func() error{
				"Add":       func() error { return w.Add("svc", entry("svc")) },
				"Remove":    func() error { return w.Remove("svc") },
				"RemoveAll": w.RemoveAll,
			} {
				if err := f(); !errors.Is(err, ErrConfigInvalid) {
					t.Fatalf("%s: err = %v, want ErrConfigInvalid", op, err)
				}
			}
			if got := string(readBytes(t, w.ClaudeConfig)); got != content {
				t.Fatalf("config modified: %q", got)
			}
			if m := mode(t, w.ClaudeConfig); m != 0o644 {
				t.Fatalf("mode changed to %o", m)
			}
			assertNotExist(t, w.Ledger)
			if b := backups(t, w); len(b) != 0 {
				t.Fatalf("backups written: %v", b)
			}
		})
	}
}

func TestAddConflictsWithUserOwnedHubEntry(t *testing.T) {
	w := newWriter(t)
	writeFile(t, w.ClaudeConfig, userConfig, 0o600)
	if err := w.Add("mine", entry("mine")); !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("err = %v, want ErrConfigConflict", err)
	}
	if string(readBytes(t, w.ClaudeConfig)) != userConfig {
		t.Fatal("config modified")
	}
	if owned, err := w.Owned(); err != nil || len(owned) != 0 {
		t.Fatalf("owned = %v, %v", owned, err)
	}
	if err := w.Remove("mine"); !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("Remove of user entry: err = %v, want ErrConfigConflict", err)
	}
	if string(readBytes(t, w.ClaudeConfig)) != userConfig {
		t.Fatal("config modified by Remove")
	}
}

func TestUserEditedOwnedEntryConflicts(t *testing.T) {
	w := newWriter(t)
	if err := w.Add("svc", entry("svc")); err != nil {
		t.Fatal(err)
	}
	edited := `{"mcpServers": {"hub__svc": {"type": "http", "url": "http://127.0.0.1:1/custom"}}}`
	writeFile(t, w.ClaudeConfig, edited, 0o600)

	for op, f := range map[string]func() error{
		"Add":       func() error { return w.Add("svc", entry("svc")) },
		"Remove":    func() error { return w.Remove("svc") },
		"RemoveAll": w.RemoveAll,
	} {
		if err := f(); !errors.Is(err, ErrConfigConflict) {
			t.Fatalf("%s: err = %v, want ErrConfigConflict", op, err)
		}
		if string(readBytes(t, w.ClaudeConfig)) != edited {
			t.Fatalf("%s modified the user's edit", op)
		}
	}
	if owned, _ := w.Owned(); len(owned) != 1 {
		t.Fatalf("ledger record should be kept, got %v", owned)
	}
}

func TestAddIsIdempotentAndUpdatesOwnedEntry(t *testing.T) {
	w := newWriter(t)
	if err := w.Add("svc", entry("svc")); err != nil {
		t.Fatal(err)
	}
	first := readBytes(t, w.ClaudeConfig)
	if err := w.Add("svc", entry("svc")); err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if !bytes.Equal(first, readBytes(t, w.ClaudeConfig)) {
		t.Fatal("idempotent Add changed the file")
	}
	if b := backups(t, w); len(b) != 0 {
		t.Fatalf("idempotent Add should not write (backups %v)", b)
	}

	// Claude Code reformatting (key order, whitespace) is not a user edit.
	writeFile(t, w.ClaudeConfig, `{"mcpServers":{"hub__svc":{ "url":"http://127.0.0.1:7420/mcp/svc","type":"http" }}}`, 0o600)
	moved := ServerEntry{Type: "http", URL: "http://127.0.0.1:7421/mcp/svc"}
	if err := w.Add("svc", moved); err != nil {
		t.Fatalf("Add after reformat: %v", err)
	}
	servers := readServers(t, w.ClaudeConfig)
	if len(servers) != 1 || compact(t, servers["hub__svc"]) != `{"type":"http","url":"http://127.0.0.1:7421/mcp/svc"}` {
		t.Fatalf("servers = %v", servers)
	}
	owned, _ := w.Owned()
	sum := sha256.Sum256([]byte(`{"type":"http","url":"http://127.0.0.1:7421/mcp/svc"}`))
	if owned["svc"].Hash != hex.EncodeToString(sum[:]) || owned["svc"].URL != moved.URL {
		t.Fatalf("ledger not updated: %+v", owned["svc"])
	}
	if len(backups(t, w)) != 1 {
		t.Fatalf("expected one backup, got %v", backups(t, w))
	}

	// Entry deleted by the user but still in the ledger: Add restores it.
	writeFile(t, w.ClaudeConfig, `{"mcpServers":{}}`, 0o600)
	if err := w.Add("svc", entry("svc")); err != nil {
		t.Fatal(err)
	}
	if _, ok := readServers(t, w.ClaudeConfig)["hub__svc"]; !ok {
		t.Fatal("entry not restored")
	}
}

func TestRemoveOnlyTouchesOwnedEntry(t *testing.T) {
	w := newWriter(t)
	writeFile(t, w.ClaudeConfig, userConfig, 0o600)
	if err := w.Add("svc", entry("svc")); err != nil {
		t.Fatal(err)
	}
	if err := w.Remove("svc"); err != nil {
		t.Fatal(err)
	}
	var origTop, origServers map[string]json.RawMessage
	_ = json.Unmarshal([]byte(userConfig), &origTop)
	_ = json.Unmarshal(origTop["mcpServers"], &origServers)
	servers := readServers(t, w.ClaudeConfig)
	if len(servers) != len(origServers) {
		t.Fatalf("servers = %v", servers)
	}
	for k, v := range origServers {
		if compact(t, servers[k]) != compact(t, v) {
			t.Fatalf("%q changed", k)
		}
	}
	if owned, _ := w.Owned(); len(owned) != 0 {
		t.Fatalf("ledger = %v", owned)
	}
}

func TestRemoveNoop(t *testing.T) {
	w := newWriter(t)
	if err := w.Remove("svc"); err != nil {
		t.Fatal(err)
	}
	assertNotExist(t, w.ClaudeConfig)
	assertNotExist(t, w.Ledger)

	writeFile(t, w.ClaudeConfig, userConfig, 0o600)
	if err := w.Remove("svc"); err != nil {
		t.Fatal(err)
	}
	if err := w.RemoveAll(); err != nil {
		t.Fatal(err)
	}
	if string(readBytes(t, w.ClaudeConfig)) != userConfig {
		t.Fatal("no-op Remove modified config")
	}
	assertNotExist(t, w.Ledger)
	if b := backups(t, w); len(b) != 0 {
		t.Fatalf("backups = %v", b)
	}
}

func TestRemoveDropsLedgerWhenEntryAlreadyGone(t *testing.T) {
	w := newWriter(t)
	if err := w.Add("svc", entry("svc")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, w.ClaudeConfig, `{"mcpServers":{}}`, 0o600)
	if err := w.Remove("svc"); err != nil {
		t.Fatal(err)
	}
	if string(readBytes(t, w.ClaudeConfig)) != `{"mcpServers":{}}` {
		t.Fatal("config should not be rewritten")
	}
	if owned, _ := w.Owned(); len(owned) != 0 {
		t.Fatalf("ledger = %v", owned)
	}
}

func TestRemoveAllLeavesUserAndEditedEntries(t *testing.T) {
	w := newWriter(t)
	writeFile(t, w.ClaudeConfig, userConfig, 0o600)
	for _, id := range []string{"aa", "bb", "cc", "gone"} {
		if err := w.Add(id, entry(id)); err != nil {
			t.Fatal(err)
		}
	}
	// User edits hub__cc and deletes hub__gone.
	var top map[string]any
	_ = json.Unmarshal(readBytes(t, w.ClaudeConfig), &top)
	servers := top["mcpServers"].(map[string]any)
	servers["hub__cc"] = map[string]any{"type": "http", "url": "http://edited"}
	delete(servers, "hub__gone")
	b, _ := json.Marshal(top)
	writeFile(t, w.ClaudeConfig, string(b), 0o600)

	err := w.RemoveAll()
	if !errors.Is(err, ErrConfigConflict) || !strings.Contains(err.Error(), "hub__cc") {
		t.Fatalf("err = %v, want ErrConfigConflict naming hub__cc", err)
	}
	got := readServers(t, w.ClaudeConfig)
	var names []string
	for k := range got {
		names = append(names, k)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "github,hub__cc,hub__mine" {
		t.Fatalf("remaining = %v", names)
	}
	owned, _ := w.Owned()
	if len(owned) != 1 || owned["cc"].ID != "cc" {
		t.Fatalf("ledger = %v", owned)
	}
	if _, ok := readTop(t, w.ClaudeConfig)["projects"]; !ok {
		t.Fatal("unrelated keys lost")
	}

	// An edited entry stays the user's; RemoveAll keeps reporting it.
	if err := w.RemoveAll(); !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("second RemoveAll err = %v", err)
	}
}

func TestRemoveAllAllClean(t *testing.T) {
	w := newWriter(t)
	for _, id := range []string{"aa", "bb"} {
		if err := w.Add(id, entry(id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.RemoveAll(); err != nil {
		t.Fatal(err)
	}
	if s := readServers(t, w.ClaudeConfig); len(s) != 0 {
		t.Fatalf("servers = %v", s)
	}
	if owned, _ := w.Owned(); len(owned) != 0 {
		t.Fatalf("ledger = %v", owned)
	}
}

func TestFileModePreserved(t *testing.T) {
	w := newWriter(t)
	writeFile(t, w.ClaudeConfig, userConfig, 0o644)
	if err := w.Add("svc", entry("svc")); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, w.ClaudeConfig); m != 0o644 {
		t.Fatalf("mode = %o, want 644", m)
	}
	if err := w.Remove("svc"); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, w.ClaudeConfig); m != 0o644 {
		t.Fatalf("mode after Remove = %o, want 644", m)
	}
}

func TestSymlinkTargetUpdatedAndLinkKept(t *testing.T) {
	w := newWriter(t)
	dotfiles := filepath.Join(filepath.Dir(w.Ledger), "..", "dotfiles")
	if err := os.MkdirAll(dotfiles, 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(dotfiles, "claude.json")
	writeFile(t, linked, userConfig, 0o640)
	if err := os.Symlink(linked, w.ClaudeConfig); err != nil {
		t.Fatal(err)
	}
	if err := w.Add("svc", entry("svc")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(w.ClaudeConfig)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced by a regular file (err %v)", err)
	}
	if dst, _ := os.Readlink(w.ClaudeConfig); dst != linked {
		t.Fatalf("symlink now points at %s", dst)
	}
	var s map[string]json.RawMessage
	var top map[string]json.RawMessage
	_ = json.Unmarshal(readBytes(t, linked), &top)
	_ = json.Unmarshal(top["mcpServers"], &s)
	if _, ok := s["hub__svc"]; !ok {
		t.Fatalf("target not updated: %s", readBytes(t, linked))
	}
	if m := mode(t, linked); m != 0o640 {
		t.Fatalf("target mode = %o", m)
	}
	assertNoTempFiles(t, dotfiles)
	assertNoTempFiles(t, filepath.Dir(w.ClaudeConfig))
}

func TestDanglingSymlinkIsAnError(t *testing.T) {
	w := newWriter(t)
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing.json"), w.ClaudeConfig); err != nil {
		t.Fatal(err)
	}
	if err := w.Add("svc", entry("svc")); err == nil {
		t.Fatal("expected error for dangling symlink")
	}
	if fi, err := os.Lstat(w.ClaudeConfig); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("dangling symlink was replaced")
	}
}

func TestBackupsCreatedAndPruned(t *testing.T) {
	w := newWriter(t)
	writeFile(t, w.ClaudeConfig, userConfig, 0o644)
	var before []byte
	for i := range 7 {
		before = readBytes(t, w.ClaudeConfig)
		e := ServerEntry{Type: "http", URL: "http://127.0.0.1:7420/mcp/svc?v=" + string(rune('a'+i))}
		if err := w.Add("svc", e); err != nil {
			t.Fatal(err)
		}
	}
	names := backups(t, w)
	if len(names) != maxBackups {
		t.Fatalf("backups = %v, want %d", names, maxBackups)
	}
	// Clock ticks once per Add (EnabledAt) and once per backup: backups are at t0+2s, +4s, ... +14s.
	// The oldest two (t0+2s, t0+4s) were pruned, so the oldest kept is t0+6s.
	if !strings.HasPrefix(names[0], "claude.json.20261101T090006") {
		t.Fatalf("oldest kept backup = %s", names[0])
	}
	newest := filepath.Join(w.BackupDir, names[len(names)-1])
	if !bytes.Equal(readBytes(t, newest), before) {
		t.Fatal("newest backup is not the pre-write content")
	}
	if m := mode(t, newest); m != 0o600 {
		t.Fatalf("backup mode = %o", m)
	}
	if m := mode(t, w.BackupDir); m != 0o700 {
		t.Fatalf("backup dir mode = %o", m)
	}
	// Files that are not ours are never pruned.
	writeFile(t, filepath.Join(w.BackupDir, "README"), "keep", 0o600)
	if err := w.Remove("svc"); err != nil {
		t.Fatal(err)
	}
	if names := backups(t, w); len(names) != maxBackups+1 || names[0] != "README" {
		t.Fatalf("after prune: %v", names)
	}
}

func TestConcurrentChangeIsReappliedOnce(t *testing.T) {
	w := newWriter(t)
	writeFile(t, w.ClaudeConfig, userConfig, 0o600)
	calls := 0
	w.beforeRename = func() {
		calls++
		if calls == 1 { // Claude Code writes the file between our read and rename
			writeFile(t, w.ClaudeConfig, `{"theme":"light","mcpServers":{"github":{"type":"stdio","command":"x"}}}`, 0o600)
		}
	}
	if err := w.Add("svc", entry("svc")); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("beforeRename calls = %d, want 2", calls)
	}
	top := readTop(t, w.ClaudeConfig)
	if string(top["theme"]) != `"light"` {
		t.Fatalf("concurrent change lost: %s", readBytes(t, w.ClaudeConfig))
	}
	if _, ok := readServers(t, w.ClaudeConfig)["hub__svc"]; !ok {
		t.Fatal("our entry missing")
	}
	assertNoTempFiles(t, filepath.Dir(w.ClaudeConfig))
}

func TestConcurrentChangeTwiceIsConflict(t *testing.T) {
	w := newWriter(t)
	writeFile(t, w.ClaudeConfig, userConfig, 0o600)
	calls := 0
	var last string
	w.beforeRename = func() {
		calls++
		last = `{"numStartups":` + string(rune('0'+calls)) + `}`
		writeFile(t, w.ClaudeConfig, last, 0o600)
	}
	err := w.Add("svc", entry("svc"))
	if !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("err = %v, want ErrConfigConflict", err)
	}
	if calls != 2 {
		t.Fatalf("beforeRename calls = %d, want 2", calls)
	}
	if string(readBytes(t, w.ClaudeConfig)) != last {
		t.Fatal("Claude Code's write was clobbered")
	}
	if owned, err := w.Owned(); err != nil || len(owned) != 0 {
		t.Fatalf("ledger should be rolled back, got %v (%v)", owned, err)
	}
	assertNoTempFiles(t, filepath.Dir(w.ClaudeConfig))
}

func TestConcurrentCreateOfMissingFileDetected(t *testing.T) {
	w := newWriter(t)
	// Claude Code creates the file after we saw it missing; we must merge into it, not clobber it.
	w.beforeRename = func() { writeFile(t, w.ClaudeConfig, `{"x":1}`, 0o644) }
	if err := w.Add("svc", entry("svc")); err != nil {
		t.Fatal(err)
	}
	if string(readTop(t, w.ClaudeConfig)["x"]) != "1" {
		t.Fatalf("clobbered: %s", readBytes(t, w.ClaudeConfig))
	}
	if _, ok := readServers(t, w.ClaudeConfig)["hub__svc"]; !ok {
		t.Fatal("our entry missing")
	}
	if m := mode(t, w.ClaudeConfig); m != 0o644 {
		t.Fatalf("mode = %o, want the concurrently created file's 644", m)
	}
}

func TestBadIDRejected(t *testing.T) {
	w := newWriter(t)
	for _, id := range []string{"", "a", "A1", "-ab", "ab_c", "../etc", "ab/c", "Ab", strings.Repeat("a", 64), "ab\n"} {
		for op, f := range map[string]func() error{
			"Add":    func() error { return w.Add(id, entry("x")) },
			"Remove": func() error { return w.Remove(id) },
		} {
			if err := f(); !errors.Is(err, ErrInvalidID) {
				t.Fatalf("%s(%q): err = %v, want ErrInvalidID", op, id, err)
			}
		}
	}
	assertNotExist(t, w.ClaudeConfig)
	assertNotExist(t, w.Ledger)
	for _, id := range []string{"ab", "0a", "a-", strings.Repeat("a", 63)} {
		if err := w.Add(id, entry(id)); err != nil {
			t.Fatalf("Add(%q): %v", id, err)
		}
	}
}

func TestCorruptLedgerIsAnErrorAndConfigUntouched(t *testing.T) {
	w := newWriter(t)
	writeFile(t, w.ClaudeConfig, userConfig, 0o600)
	if err := os.MkdirAll(filepath.Dir(w.Ledger), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{nope`, `{"version":2,"servers":{}}`} {
		writeFile(t, w.Ledger, bad, 0o600)
		if err := w.Add("svc", entry("svc")); err == nil {
			t.Fatalf("ledger %q: expected error", bad)
		}
		if _, err := w.Owned(); err == nil {
			t.Fatalf("Owned with ledger %q: expected error", bad)
		}
		if string(readBytes(t, w.ClaudeConfig)) != userConfig {
			t.Fatal("config modified")
		}
	}
}

func TestOwnedEmptyWithoutLedger(t *testing.T) {
	w := newWriter(t)
	owned, err := w.Owned()
	if err != nil || owned == nil || len(owned) != 0 {
		t.Fatalf("owned = %v, %v", owned, err)
	}
}

func TestDefaultPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_PATH", "")
	t.Setenv("MCP_HUB_HOME", "")
	p, err := DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{
		ClaudeConfig: filepath.Join(home, ".claude.json"),
		Ledger:       filepath.Join(home, ".mcp-hub", "state.json"),
		BackupDir:    filepath.Join(home, ".mcp-hub", "backups"),
	}
	if p != want {
		t.Fatalf("got %+v, want %+v", p, want)
	}

	t.Setenv("CLAUDE_CONFIG_PATH", "/cfg/claude.json")
	t.Setenv("MCP_HUB_HOME", "/hub")
	t.Setenv("HOME", "")
	p, err = DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if p != (Paths{ClaudeConfig: "/cfg/claude.json", Ledger: "/hub/state.json", BackupDir: "/hub/backups"}) {
		t.Fatalf("env not honoured: %+v", p)
	}

	t.Setenv("MCP_HUB_HOME", "")
	if _, err := DefaultPaths(); err == nil {
		t.Fatal("expected error with no HOME and no MCP_HUB_HOME")
	}
}

func TestConfigName(t *testing.T) {
	if ConfigName("svc") != "hub__svc" {
		t.Fatal(ConfigName("svc"))
	}
}
