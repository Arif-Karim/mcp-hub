package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func sandbox(t *testing.T) (hubHome, claude string) {
	t.Helper()
	dir := t.TempDir()
	hubHome, claude = filepath.Join(dir, "hub"), filepath.Join(dir, "claude.json")
	t.Setenv("HOME", dir)
	t.Setenv("MCP_HUB_HOME", hubHome)
	t.Setenv("CLAUDE_CONFIG_PATH", claude)
	t.Setenv("MCP_HUB_CATALOGUE", "")
	t.Setenv("MCP_HUB_ADDR", "")
	return hubHome, claude
}

func TestServeStartsAndShutsDownOnSignal(t *testing.T) {
	hubHome, claude := sandbox(t)
	addr := freeAddr(t)
	done := make(chan error, 1)
	go func() { done <- serve([]string{"--addr", addr}) }()

	var status map[string]any
	deadline := time.Now().Add(10 * time.Second)
	for status == nil {
		if time.Now().After(deadline) {
			t.Fatal("daemon never answered /api/v1/status")
		}
		if resp, err := http.Get("http://" + addr + "/api/v1/status"); err == nil { //nolint:noctx // test poll
			_ = json.NewDecoder(resp.Body).Decode(&status)
			_ = resp.Body.Close()
		} else {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if status["addr"] != addr || status["claudeConfigPath"] != claude || status["catalogueSource"] != "embedded" ||
		status["user"] == "" || status["enabledCount"] != float64(0) {
		t.Fatalf("status = %v", status)
	}
	if _, err := os.Stat(filepath.Join(hubHome, "keys", "signing.pem")); err != nil {
		t.Fatalf("signing key not created under MCP_HUB_HOME: %v", err)
	}

	// A second daemon on the same address fails before touching anything.
	if err := serve([]string{"--addr", addr}); err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("second serve = %v, want address already in use", err)
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v after SIGINT, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not shut down on SIGINT")
	}
}

func TestServeRejectsBadInput(t *testing.T) {
	sandbox(t)
	if err := serve([]string{"--addr", "0.0.0.0:7420"}); err == nil || !strings.Contains(err.Error(), "non-loopback") {
		t.Fatalf("non-loopback = %v", err)
	}
	t.Setenv("MCP_HUB_CATALOGUE", filepath.Join("..", "..", "sample-data", "invalid-catalogue.yaml"))
	if err := serve([]string{"--addr", freeAddr(t)}); err == nil || !strings.Contains(err.Error(), "invalid-catalogue.yaml") {
		t.Fatalf("invalid catalogue = %v", err)
	}
	if err := serve([]string{"--nope"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

func TestCurrentUser(t *testing.T) {
	if u := currentUser(); u == "" {
		t.Fatal("currentUser is empty")
	}
}
