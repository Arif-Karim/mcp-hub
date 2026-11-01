// Package directory implements the service-directory MCP tools over an embedded,
// synthetic list of fictional internal services.
package directory

import (
	"errors"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Name and Version identify the server to MCP clients.
const (
	Name    = "service-directory"
	Version = "0.1.0"
)

// Sentinel tool errors (returned to the client as IsError results).
var (
	ErrEmptySearch    = errors.New("query or tag is required")
	ErrUnknownService = errors.New("unknown service")
)

// Service is one directory entry.
type Service struct {
	Name    string   `json:"name"`
	Team    string   `json:"team"`
	Tags    []string `json:"tags"`
	Channel string   `json:"channel"`
	Tier    int      `json:"tier"`
	Runbook string   `json:"runbook,omitempty"`
}

// NewServer returns an MCP server exposing find_service and get_runbook.
func NewServer(_ *slog.Logger) *mcp.Server {
	return mcp.NewServer(&mcp.Implementation{Name: Name, Version: Version}, nil)
}
