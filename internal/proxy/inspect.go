package proxy

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
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

func isLoopbackName(h string) bool {
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

// isLoopbackHost checks the Host header so a DNS-rebound name pointing at
// 127.0.0.1 cannot reach the proxy from a browser.
func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
	}
	return isLoopbackName(host)
}

// allowedOrigin accepts requests without an Origin (Claude Code, curl) and
// browser requests only from http loopback pages.
func allowedOrigin(h http.Header) bool {
	vals := h.Values("Origin")
	if len(vals) == 0 {
		return true
	}
	if len(vals) > 1 {
		return false
	}
	u, err := url.Parse(vals[0])
	return err == nil && u.Scheme == "http" && isLoopbackName(u.Hostname())
}
