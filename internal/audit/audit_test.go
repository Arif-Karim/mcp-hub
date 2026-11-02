package audit_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/audit"
)

func newLog(t *testing.T, capacity int) (*audit.Log, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logs", "audit.jsonl")
	l, err := audit.NewLog(path, capacity)
	if err != nil {
		t.Fatalf("NewLog: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l, path
}

func ev(i int) audit.Event {
	return audit.Event{
		Time:       time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC),
		Server:     "svc",
		Method:     "tools/call",
		Tool:       fmt.Sprintf("tool-%d", i),
		Status:     200,
		DurationMs: int64(i),
		User:       "dev@example.test",
	}
}

func TestEventJSONMatchesContract(t *testing.T) {
	b, err := json.Marshal(ev(1))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"time":"2026-01-01T00:00:01Z","server":"svc","method":"tools/call","tool":"tool-1","status":200,"durationMs":1,"user":"dev@example.test"}`
	if string(b) != want {
		t.Fatalf("json =\n%s\nwant\n%s", b, want)
	}
	e := ev(1)
	e.Tool = ""
	b, _ = json.Marshal(e)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["tool"]; ok {
		t.Fatal("empty tool should be omitted")
	}
}

func TestRecentNewestFirstAndLimit(t *testing.T) {
	l, _ := newLog(t, 10)
	for i := range 3 {
		l.Record(ev(i))
	}
	got := l.Recent(10)
	if len(got) != 3 || got[0].Tool != "tool-2" || got[2].Tool != "tool-0" {
		t.Fatalf("recent = %+v", got)
	}
	got = l.Recent(2)
	if len(got) != 2 || got[0].Tool != "tool-2" || got[1].Tool != "tool-1" {
		t.Fatalf("recent(2) = %+v", got)
	}
	if got := l.Recent(0); len(got) != 0 {
		t.Fatalf("recent(0) = %+v", got)
	}
}

func TestRingWrapsAround(t *testing.T) {
	l, path := newLog(t, 3)
	for i := range 7 {
		l.Record(ev(i))
	}
	got := l.Recent(100)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, want := range []string{"tool-6", "tool-5", "tool-4"} {
		if got[i].Tool != want {
			t.Fatalf("got[%d] = %q, want %q", i, got[i].Tool, want)
		}
	}
	// The file keeps everything.
	if n := len(readLines(t, path)); n != 7 {
		t.Fatalf("file lines = %d, want 7", n)
	}
}

func TestDefaultCapacity(t *testing.T) {
	l, _ := newLog(t, 0)
	for i := range audit.DefaultCapacity + 5 {
		l.Record(ev(i))
	}
	if n := len(l.Recent(10_000)); n != audit.DefaultCapacity {
		t.Fatalf("len = %d, want %d", n, audit.DefaultCapacity)
	}
	if audit.DefaultCapacity != 500 {
		t.Fatalf("DefaultCapacity = %d", audit.DefaultCapacity)
	}
}

func TestFileIsJSONLWithPrivatePerms(t *testing.T) {
	l, path := newLog(t, 10)
	l.Record(ev(1))
	l.Record(ev(2))

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("perms = %o, want 600", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(path))
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir perms = %o, want 700", di.Mode().Perm())
	}
	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}
	var e audit.Event
	if err := json.Unmarshal([]byte(lines[1]), &e); err != nil || e.Tool != "tool-2" {
		t.Fatalf("line 2 = %q (%v)", lines[1], err)
	}

	// Reopening appends rather than truncating.
	_ = l.Close()
	l2, err := audit.NewLog(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l2.Close() }()
	l2.Record(ev(3))
	if n := len(readLines(t, path)); n != 3 {
		t.Fatalf("after reopen lines = %d, want 3", n)
	}
}

func TestRecordAfterCloseKeepsMemory(t *testing.T) {
	l, _ := newLog(t, 10)
	_ = l.Close()
	l.Record(ev(1)) // file write fails; must not panic
	if got := l.Recent(1); len(got) != 1 || got[0].Tool != "tool-1" {
		t.Fatalf("recent = %+v", got)
	}
}

func TestNewLogFailsOnUnwritablePath(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := audit.NewLog(filepath.Join(blocker, "audit.jsonl"), 1); err == nil {
		t.Fatal("expected error when parent is a file")
	}
}

func TestConcurrentRecord(t *testing.T) {
	l, path := newLog(t, 1000)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 50 {
				l.Record(ev(g*50 + i))
				_ = l.Recent(5)
			}
		}()
	}
	wg.Wait()
	if n := len(l.Recent(1000)); n != 400 {
		t.Fatalf("recent = %d, want 400", n)
	}
	lines := readLines(t, path)
	if len(lines) != 400 {
		t.Fatalf("file lines = %d, want 400", len(lines))
	}
	for _, ln := range lines {
		var e audit.Event
		if err := json.Unmarshal([]byte(ln), &e); err != nil {
			t.Fatalf("interleaved line %q: %v", ln, err)
		}
	}
}

func TestHandler(t *testing.T) {
	l, _ := newLog(t, 600)
	for i := range 60 {
		l.Record(ev(i))
	}
	h := audit.Handler(l)

	cases := []struct {
		query  string
		status int
		n      int
	}{
		{"", 200, 50},
		{"?limit=1", 200, 1},
		{"?limit=500", 200, 60},
		{"?limit=0", 400, 0},
		{"?limit=501", 400, 0},
		{"?limit=-1", 400, 0},
		{"?limit=abc", 400, 0},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/audit"+tc.query, nil))
		if rec.Code != tc.status {
			t.Errorf("%q: status = %d, want %d", tc.query, rec.Code, tc.status)
			continue
		}
		if tc.status != 200 {
			var body struct {
				Error struct{ Code string } `json:"error"`
			}
			_ = json.NewDecoder(rec.Body).Decode(&body)
			if body.Error.Code != "bad_request" {
				t.Errorf("%q: code = %q", tc.query, body.Error.Code)
			}
			continue
		}
		var body struct {
			Events []audit.Event `json:"events"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Events) != tc.n || body.Events[0].Tool != "tool-59" {
			t.Errorf("%q: n = %d first = %+v", tc.query, len(body.Events), body.Events[0])
		}
	}
}

func TestHandlerEmptyIsArrayNotNull(t *testing.T) {
	l, _ := newLog(t, 5)
	rec := httptest.NewRecorder()
	audit.Handler(l).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil))
	if got := rec.Body.String(); got != "{\"events\":[]}\n" {
		t.Fatalf("body = %q", got)
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // test path
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		out = append(out, s.Text())
	}
	return out
}
