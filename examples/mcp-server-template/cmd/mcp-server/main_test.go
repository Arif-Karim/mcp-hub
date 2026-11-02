package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Arif-Karim/mcp-hub/examples/mcp-server-template/internal/app"
	"github.com/Arif-Karim/mcp-hub/examples/mcp-server-template/internal/hubauth"
)

func testHandlerAndToken(t *testing.T) (http.Handler, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "OKP", "crv": "Ed25519", "kid": "k1", "x": base64.RawURLEncoding.EncodeToString(pub),
		}}})
	}))
	t.Cleanup(jwks.Close)
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"sub": "dev.user@example.test", "iss": "mcp-hub", "aud": "service-directory",
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	tok.Header["kid"] = "k1"
	signed, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	v := &hubauth.Verifier{Keys: hubauth.NewJWKS(jwks.URL, nil), Issuer: "mcp-hub", Audience: "service-directory"}
	return app.Handler(v, slog.New(slog.DiscardHandler)), signed
}

func post(t *testing.T, url, token, session, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// A client holding the standalone SSE stream (Claude Code does by default) must
// not stall shutdown: SIGTERM has to end promptly and cleanly.
func TestServeShutsDownPromptlyWithOpenSSEStream(t *testing.T) {
	h, token := testHandlerAndToken(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + ln.Addr().String() + app.MCPPath

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, ln, h, slog.New(slog.DiscardHandler)) }()

	resp := post(t, url, token, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	session := resp.Header.Get("Mcp-Session-Id")
	if resp.StatusCode != http.StatusOK || session == "" {
		t.Fatalf("initialize: status %d session %q", resp.StatusCode, session)
	}
	resp = post(t, url, token, session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	_ = resp.Body.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Mcp-Session-Id", session)
	sse, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sse.Body.Close() }()
	if sse.StatusCode != http.StatusOK {
		t.Fatalf("SSE GET status %d", sse.StatusCode)
	}

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
		if d := time.Since(start); d > time.Second {
			t.Fatalf("shutdown took %v, want < 1s", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown hung with an open SSE stream")
	}
}

func TestServeReturnsListenerErrors(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()
	if err := serve(context.Background(), ln, http.NotFoundHandler(), slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("expected error from closed listener")
	}
}
