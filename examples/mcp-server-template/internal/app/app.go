// Package app wires the HTTP surface: GET /healthz (no auth) and the MCP
// streamable HTTP endpoint at /mcp behind hub token verification.
package app

import (
	"log/slog"
	"net/http"

	"github.com/Arif-Karim/mcp-hub/examples/mcp-server-template/internal/hubauth"
)

// MCPPath is where the streamable HTTP MCP endpoint is served.
const MCPPath = "/mcp"

// Handler returns the root handler.
func Handler(_ *hubauth.Verifier, _ *slog.Logger) http.Handler {
	return http.NewServeMux()
}
