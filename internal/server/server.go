// Package server assembles the daemon's HTTP surface: the /api/v1 control
// plane, the /mcp proxy and the embedded UI.
package server

import (
	"net/http"

	"github.com/Arif-Karim/mcp-hub/internal/httpx"
)

// Options configures the handler. Lanes add fields as they land.
type Options struct {
	Version string
	// UI serves the embedded web app at "/". Nil disables it.
	UI http.Handler
}

// New returns the root handler.
func New(opts Options) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": opts.Version})
	})
	// The /api/ catch-all below is more general than any method-scoped route, so
	// without this the mux would answer a wrong-method request with 404, not 405.
	mux.HandleFunc("/api/v1/health", methodNotAllowed)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "no such API route")
	})
	if opts.UI != nil {
		mux.Handle("/", opts.UI)
	}
	return mux
}

func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteError(w, http.StatusMethodNotAllowed, httpx.CodeBadRequest, "method not allowed")
}
