package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCatalogue(t *testing.T) {
	t.Setenv("MCP_HUB_CATALOGUE", "")
	s, err := loadCatalogue()
	if err != nil {
		t.Fatalf("embedded: %v", err)
	}
	if _, err := s.Get("deepwiki"); err != nil {
		t.Fatalf("embedded catalogue missing deepwiki: %v", err)
	}

	t.Setenv("MCP_HUB_CATALOGUE", filepath.Join("..", "..", "sample-data", "invalid-catalogue.yaml"))
	if _, err := loadCatalogue(); err == nil || !strings.Contains(err.Error(), "invalid-catalogue.yaml") {
		t.Fatalf("invalid override: err = %v, want error naming the file", err)
	}

	good := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(good, []byte("version: 1\nentries: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCP_HUB_CATALOGUE", good)
	if s, err := loadCatalogue(); err != nil || len(s.Search("", "")) != 0 {
		t.Fatalf("override: %v", err)
	}
}

func TestRequireLoopback(t *testing.T) {
	cases := []struct {
		addr string
		ok   bool
	}{
		{"127.0.0.1:7420", true},
		{"localhost:7420", true},
		{"[::1]:7420", true},
		{"0.0.0.0:7420", false},
		{":7420", false},
		{"10.1.2.3:7420", false},
		{"not-an-addr", false},
	}
	for _, c := range cases {
		err := requireLoopback(c.addr)
		if (err == nil) != c.ok {
			t.Errorf("requireLoopback(%q) err=%v, want ok=%v", c.addr, err, c.ok)
		}
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	if err := run([]string{"bogus"}); err == nil {
		t.Fatal("expected error for unknown command")
	}
	if err := run(nil); err == nil {
		t.Fatal("expected usage error with no args")
	}
}

func TestClaudeDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_HOME_DIR", dir)
	if got := claudeDir(); got != dir {
		t.Fatalf("claudeDir() = %q, want %q", got, dir)
	}
	t.Setenv("CLAUDE_HOME_DIR", "relative")
	if got := claudeDir(); got != "" {
		t.Fatalf("claudeDir() with a relative override = %q, want empty (disabled)", got)
	}
}
