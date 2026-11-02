// Package audit records one event per proxied MCP request: an append-only
// JSONL file for durable evidence plus an in-memory ring for the UI.
// Tool arguments, results and credentials are never part of an Event.
package audit

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultCapacity is the ring size used when NewLog gets capacity <= 0.
const DefaultCapacity = 500

// Event mirrors the contract's AuditEvent.
type Event struct {
	Time       time.Time `json:"time"`
	Server     string    `json:"server"`
	Method     string    `json:"method"`
	Tool       string    `json:"tool,omitempty"`
	Status     int       `json:"status"`
	DurationMs int64     `json:"durationMs"`
	User       string    `json:"user"`
}

// Log is safe for concurrent use.
type Log struct {
	mu   sync.Mutex
	f    *os.File
	ring []Event
	next int // index of the slot the next event goes into
	n    int // number of valid events in ring

	writeErrors int64
}

// NewLog opens (appending) or creates the JSONL file at path with 0600 perms
// in a 0700 directory.
func NewLog(path string, capacity int) (*Log, error) {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("audit: dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600) //nolint:gosec // operator-configured path
	if err != nil {
		return nil, fmt.Errorf("audit: open: %w", err)
	}
	return &Log{f: f, ring: make([]Event, capacity)}, nil
}

// Record stores e in memory and appends it to the file. File errors are
// logged and otherwise ignored so auditing never fails a proxied request.
func (l *Log) Record(e Event) {
	line, err := json.Marshal(e)
	if err != nil { // unreachable for this struct; keep the guard cheap
		slog.Error("audit marshal", "err", err)
		return
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	l.ring[l.next] = e
	l.next = (l.next + 1) % len(l.ring)
	if l.n < len(l.ring) {
		l.n++
	}
	if _, err := l.f.Write(line); err != nil {
		l.writeErrors++
		// Log the first failure loudly, then every 100th, so a full disk can't flood logs.
		if l.writeErrors%100 == 1 {
			slog.Error("audit write failed; events kept in memory only", "err", err, "failures", l.writeErrors)
		}
	}
}

// WriteErrors is the number of events that could not be appended to the file.
// Non-zero means the durable audit trail is incomplete (e.g. disk full).
func (l *Log) WriteErrors() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.writeErrors
}

// Recent returns up to limit events, newest first.
func (l *Log) Recent(limit int) []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	limit = min(max(limit, 0), l.n)
	out := make([]Event, 0, limit)
	// range (not k++) so a mutated loop step can't run unbounded and exhaust memory.
	for k := range limit {
		out = append(out, l.ring[(l.next-1-k+len(l.ring))%len(l.ring)])
	}
	return out
}

// Close closes the file. Later Records still update the ring.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}
