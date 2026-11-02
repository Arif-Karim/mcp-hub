// Package app wires the HTTP surface: GET /healthz (no auth) and the MCP
// streamable HTTP endpoint at /mcp behind hub token verification.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arif-Karim/mcp-hub/examples/mcp-server-template/internal/directory"
	"github.com/Arif-Karim/mcp-hub/examples/mcp-server-template/internal/hubauth"
)

// MCPPath is where the streamable HTTP MCP endpoint is served.
const MCPPath = "/mcp"

// sessionTimeout closes MCP sessions idle this long, so sessions abandoned
// without a DELETE (e.g. the client was killed) do not leak. Var for tests.
var sessionTimeout = 30 * time.Minute

// Handler returns the root handler.
func Handler(v *hubauth.Verifier, logger *slog.Logger) http.Handler {
	server := directory.NewServer(logger)
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{SessionTimeout: sessionTimeout, Logger: logger},
	)

	// hubauth.Middleware does the real verification and returns our JSON 401.
	// This SDK adapter only lifts the already-verified subject into the SDK's
	// TokenInfo, so tools see it (req.Extra.TokenInfo) and the SDK binds each
	// MCP session to its user (a different sub cannot reuse the session id).
	lift := sdkauth.RequireBearerToken(func(ctx context.Context, _ string, _ *http.Request) (*sdkauth.TokenInfo, error) {
		sub := hubauth.SubjectFrom(ctx)
		if sub == "" {
			return nil, sdkauth.ErrInvalidToken
		}
		return &sdkauth.TokenInfo{UserID: sub}, nil
	}, &sdkauth.RequireBearerTokenOptions{AllowMissingExpiration: true}) // exp already enforced (with leeway) by hubauth

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})
	mux.Handle(MCPPath, hubauth.Middleware(v, logger, lift(mcpHandler)))
	return mux
}
