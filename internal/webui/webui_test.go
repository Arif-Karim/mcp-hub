package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The placeholder build (only .gitkeep in dist) must still serve something useful.
func TestHandlerServesNoticeWhenUINotBuilt(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "index.html") && !strings.Contains(body, "UI not built") {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestTrimSlash(t *testing.T) {
	cases := map[string]string{"/": "index.html", "": "index.html", "/assets/a.js": "assets/a.js"}
	for in, want := range cases {
		if got := trimSlash(in); got != want {
			t.Errorf("trimSlash(%q) = %q, want %q", in, got, want)
		}
	}
}
