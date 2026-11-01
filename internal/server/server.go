// Package server assembles the daemon's HTTP surface: the /api/v1 control
// plane, the /mcp proxy and the embedded UI.
package server

import "net/http"

// Options configures the handler. Lanes add fields as they land.
type Options struct {
	Version string
}

// New returns the root handler.
func New(_ Options) http.Handler {
	return http.NewServeMux()
}
