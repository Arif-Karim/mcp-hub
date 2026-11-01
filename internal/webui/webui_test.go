package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHandlerServesNoticeWhenUINotBuilt(t *testing.T) {
	rec := get(t, handlerFor(fstest.MapFS{".gitkeep": {}}), "/")

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "UI not built") {
		t.Fatalf("got %d %q, want 200 with not-built notice", rec.Code, rec.Body.String())
	}
}

func TestHandlerServesBuiltUIWithSPAFallback(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":    {Data: []byte("<html>app</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	h := handlerFor(fsys)

	if rec := get(t, h, "/assets/app.js"); !strings.Contains(rec.Body.String(), "console.log") {
		t.Fatalf("asset: got %q", rec.Body.String())
	}
	// Unknown client-side routes fall back to index.html.
	if rec := get(t, h, "/activity"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "app") {
		t.Fatalf("fallback: got %d %q", rec.Code, rec.Body.String())
	}
}

// Handler must work against whatever is embedded (placeholder in CI, real UI after `make build`).
func TestEmbeddedHandlerServesRoot(t *testing.T) {
	if rec := get(t, Handler(), "/"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
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
