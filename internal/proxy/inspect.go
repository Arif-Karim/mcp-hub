package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/Arif-Karim/mcp-hub/internal/httpx"
)

// rpcMessage decodes only what the audit log needs. params.arguments is never
// decoded into a Go value and never leaves this function.
type rpcMessage struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type callParams struct {
	Name string `json:"name"`
}

// describe returns the JSON-RPC method(s) and, for tools/call, the tool name(s)
// in body; batches are joined with ",". Non-JSON-RPC requests report httpMethod.
func describe(httpMethod string, body []byte) (method, tool string) {
	body = bytes.TrimSpace(body)
	var msgs []rpcMessage
	switch {
	case len(body) == 0:
	case body[0] == '[':
		_ = json.Unmarshal(body, &msgs)
	case body[0] == '{':
		var m rpcMessage
		if json.Unmarshal(body, &m) == nil {
			msgs = []rpcMessage{m}
		}
	}
	var methods, tools []string
	for _, m := range msgs {
		if m.Method == "" {
			continue // a response or malformed message
		}
		if !slices.Contains(methods, m.Method) {
			methods = append(methods, m.Method)
		}
		var p callParams
		if m.Method == "tools/call" && json.Unmarshal(m.Params, &p) == nil && p.Name != "" {
			tools = append(tools, p.Name)
		}
	}
	if len(methods) == 0 {
		return httpMethod, ""
	}
	return strings.Join(methods, ","), strings.Join(tools, ",")
}

// allowedOrigin accepts requests without an Origin (Claude Code, curl) and
// browser requests only from http loopback pages. Stricter than the server
// guard: applies to every method, since GET opens an SSE stream.
func allowedOrigin(h http.Header) bool {
	vals := h.Values("Origin")
	if len(vals) == 0 {
		return true
	}
	return len(vals) == 1 && httpx.IsLoopbackOrigin(vals[0])
}
