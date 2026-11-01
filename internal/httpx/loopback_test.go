package httpx_test

import (
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/httpx"
)

func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:7420": true, "127.0.0.1": true, "localhost:7420": true, "localhost": true,
		"[::1]:7420": true, "[::1]": true, "::1": true,
		"": false, "evil.example": false, "evil.example:7420": false, "localhost.evil:7420": false,
		"127.0.0.2:7420": false, "0.0.0.0:7420": false, "[::2]:7420": false,
	}
	for in, want := range cases {
		if got := httpx.IsLoopbackHost(in); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsLoopbackOrigin(t *testing.T) {
	cases := map[string]bool{
		"http://127.0.0.1:7420": true, "http://localhost:5173": true, "http://[::1]:7420": true, "http://localhost": true,
		"null": false, "": false, "https://evil.example": false, "http://127.0.0.1.evil.example": false,
		"https://127.0.0.1:7420": false, "file://": false, "http://evil.example/127.0.0.1": false, "%zz": false,
	}
	for in, want := range cases {
		if got := httpx.IsLoopbackOrigin(in); got != want {
			t.Errorf("IsLoopbackOrigin(%q) = %v, want %v", in, got, want)
		}
	}
}
