package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/httpx"
)

// Per-request timeouts. Enabling a local server downloads, verifies and starts
// it, so it gets far longer than a read.
const (
	readTimeout    = 10 * time.Second
	enableTimeout  = 3 * time.Minute
	disableTimeout = 30 * time.Second
	maxBody        = 8 << 20
)

// apiError is a contract-shaped error response from the daemon.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	raw     []byte
}

func (e *apiError) Error() string { return e.Message + " (" + e.Code + ")" }

type unreachableError struct{ addr string }

func (e *unreachableError) Error() string { return "daemon not running on " + e.addr }

// call performs one API request and returns the raw 2xx body. Non-2xx contract
// errors come back as *apiError (carrying the raw body for --json).
func (e Env) call(ctx context.Context, method, path string, query url.Values, timeout time.Duration) ([]byte, error) {
	addr := e.addr()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, usageErr("MCP_HUB_ADDR %q is not host:port", addr)
	}
	if !httpx.IsLoopbackName(host) {
		return nil, usageErr("MCP_HUB_ADDR %q is not a loopback address; the daemon only listens on this machine", addr)
	}
	u := url.URL{Scheme: "http", Host: addr, Path: "/api/v1" + path, RawQuery: query.Encode()}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return nil, &unreachableError{addr: addr}
		}
		return nil, fmt.Errorf("request to daemon failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("reading daemon response: %w", err)
	}
	if resp.StatusCode/100 == 2 {
		if !json.Valid(body) {
			return nil, errors.New("unexpected response from daemon: body is not JSON")
		}
		return body, nil
	}
	var env struct {
		Error *apiError `json:"error"`
	}
	if json.Unmarshal(body, &env) != nil || env.Error == nil || env.Error.Code == "" {
		return nil, fmt.Errorf("unexpected response from daemon: HTTP %d", resp.StatusCode)
	}
	env.Error.raw = body
	return nil, env.Error
}

// decode parses a 2xx body into v.
func decode(body []byte, v any) error {
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("unexpected response from daemon: %w", err)
	}
	return nil
}

// writeRaw prints a raw JSON body followed by a newline.
func writeRaw(w io.Writer, body []byte) {
	_, _ = w.Write(body)
	if len(body) == 0 || body[len(body)-1] != '\n' {
		_, _ = io.WriteString(w, "\n")
	}
}
