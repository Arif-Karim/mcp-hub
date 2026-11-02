// Package directory implements the service-directory MCP tools over an embedded,
// synthetic list of fictional internal services.
package directory

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"

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
	Channel string   `json:"channel" jsonschema:"on-call Slack channel"`
	Tier    int      `json:"tier" jsonschema:"criticality, 1 (highest) to 3"`
	Runbook string   `json:"runbook,omitempty"`
}

//go:embed services.json
var servicesJSON []byte

// load parses the embedded data; a failure is a build defect, caught by tests.
func load() []Service {
	var s []Service
	if err := json.Unmarshal(servicesJSON, &s); err != nil {
		panic("directory: invalid embedded services.json: " + err.Error())
	}
	return s
}

// FindInput is the find_service input.
type FindInput struct {
	Query string `json:"query,omitempty" jsonschema:"case-insensitive substring of the service or team name"`
	Tag   string `json:"tag,omitempty" jsonschema:"exact tag, e.g. payments"`
}

// FindOutput is the find_service output.
type FindOutput struct {
	Services []Service `json:"services"`
}

// RunbookInput is the get_runbook input.
type RunbookInput struct {
	Service string `json:"service" jsonschema:"exact service name, as returned by find_service"`
}

// RunbookOutput is the get_runbook output.
type RunbookOutput struct {
	Service string `json:"service"`
	Runbook string `json:"runbook"`
}

type directory struct {
	services []Service
	logger   *slog.Logger
}

// NewServer returns an MCP server exposing find_service and get_runbook.
func NewServer(logger *slog.Logger) *mcp.Server {
	d := &directory{services: load(), logger: logger}
	s := mcp.NewServer(&mcp.Implementation{Name: Name, Version: Version}, nil)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "find_service",
		Description: "Search internal services by name/team substring and/or exact tag. Returns owning team, tags, on-call channel and tier.",
	}, d.find)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_runbook",
		Description: "Return the markdown runbook for a service by exact name.",
	}, d.runbook)
	return s
}

// audit logs who called which tool. Arguments and results are never logged.
func (d *directory) audit(req *mcp.CallToolRequest, tool string) {
	sub := ""
	if req != nil && req.Extra != nil && req.Extra.TokenInfo != nil {
		sub = req.Extra.TokenInfo.UserID
	}
	d.logger.Info("tool call", "tool", tool, "sub", sub)
}

func (d *directory) find(_ context.Context, req *mcp.CallToolRequest, in FindInput) (*mcp.CallToolResult, FindOutput, error) {
	d.audit(req, "find_service")
	q := strings.ToLower(strings.TrimSpace(in.Query))
	tag := strings.ToLower(strings.TrimSpace(in.Tag))
	if q == "" && tag == "" {
		return nil, FindOutput{}, ErrEmptySearch
	}
	out := FindOutput{Services: []Service{}}
	for _, s := range d.services {
		if q != "" && !strings.Contains(s.Name, q) && !strings.Contains(s.Team, q) {
			continue
		}
		if tag != "" && !slices.Contains(s.Tags, tag) {
			continue
		}
		s.Runbook = ""
		out.Services = append(out.Services, s)
	}
	return nil, out, nil
}

func (d *directory) runbook(_ context.Context, req *mcp.CallToolRequest, in RunbookInput) (*mcp.CallToolResult, RunbookOutput, error) {
	d.audit(req, "get_runbook")
	name := strings.ToLower(strings.TrimSpace(in.Service))
	for _, s := range d.services {
		if s.Name == name {
			res := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s.Runbook}}}
			return res, RunbookOutput{Service: s.Name, Runbook: s.Runbook}, nil
		}
	}
	return nil, RunbookOutput{}, ErrUnknownService
}
