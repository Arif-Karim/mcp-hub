// Package server assembles the daemon's HTTP surface: the /api/v1 control
// plane, the /mcp proxy and the embedded UI.
package server

import (
	"net/http"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/audit"
	"github.com/Arif-Karim/mcp-hub/internal/httpx"
)

// Options configures the handler. Lanes add fields as they land.
type Options struct {
	Version string
	// UI serves the embedded web app at "/". Nil disables it.
	UI http.Handler
	// Catalogue serves /api/v1/catalogue. Nil disables those routes.
	Catalogue *catalogue.Store
	// State reports an entry's install state. Nil reports every entry disabled.
	State func(id string) catalogue.InstallState
	// Proxy serves /mcp/{id}[/...] (lane C). Nil disables it.
	Proxy http.Handler
	// JWKS serves GET /.well-known/jwks.json (lane C). Nil disables it.
	JWKS http.Handler
	// Audit backs GET /api/v1/audit (lane C). Nil disables it.
	Audit *audit.Log
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
	if opts.Catalogue != nil {
		registerCatalogue(mux, opts.Catalogue, opts.State)
	}
	registerLaneC(mux, opts)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "no such API route")
	})
	if opts.UI != nil {
		mux.Handle("/", opts.UI)
	}
	return mux
}

// methodNotAllowed answers the read-only routes; Allow is required by RFC 9110.
func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Allow", "GET, HEAD")
	httpx.WriteError(w, http.StatusMethodNotAllowed, httpx.CodeBadRequest, "method not allowed")
}

func registerLaneC(mux *http.ServeMux, opts Options) {
	if opts.Proxy != nil {
		mux.Handle("/mcp/", opts.Proxy)
	}
	if opts.JWKS != nil {
		mux.Handle("GET /.well-known/jwks.json", opts.JWKS)
	}
	if opts.Audit != nil {
		mux.Handle("GET /api/v1/audit", audit.Handler(opts.Audit))
		mux.HandleFunc("/api/v1/audit", methodNotAllowed)
	}
}
