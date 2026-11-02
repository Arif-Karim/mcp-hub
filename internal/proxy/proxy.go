// Package proxy is the /mcp/{id} reverse proxy that sits between Claude Code
// and every enabled MCP server (streamable HTTP transport). It drops all client
// credentials, injects a fresh short-lived hub JWT for `hub-jwt` upstreams,
// streams SSE responses unbuffered and writes one audit event per request.
package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/audit"
	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/httpx"
)

// StatusClientClosed is audited when the client disconnects mid-request (nginx convention).
const StatusClientClosed = 499

// Defaults for Config.
const (
	DefaultTokenTTL     = 5 * time.Minute
	DefaultMaxBodyBytes = 4 << 20
	// MaxAuditField bounds client-controlled strings (method, tool) in audit events.
	MaxAuditField = 128
)

// Upstream is where an enabled server lives and how to authenticate to it.
type Upstream struct {
	URL  string
	Auth catalogue.AuthMode
}

// Resolver maps an entry ID to its upstream. Only enabled servers resolve.
type Resolver interface {
	Resolve(id string) (Upstream, bool)
}

// TokenSource issues the JWT injected for hub-jwt upstreams.
type TokenSource interface {
	Issue(sub, aud string, ttl time.Duration) (string, error)
}

// Recorder receives one audit event per proxied request.
type Recorder interface {
	Record(audit.Event)
}

// Config configures New.
type Config struct {
	Resolver     Resolver
	Tokens       TokenSource
	Audit        Recorder
	User         string // token subject and audit user
	TokenTTL     time.Duration
	MaxBodyBytes int64
	Transport    http.RoundTripper // optional; defaults to a clone of http.DefaultTransport
}

// forwardHeaders is the allowlist of request headers sent upstream. Everything
// else — Authorization, Cookie, Proxy-Authorization, API keys — is dropped.
var forwardHeaders = []string{"Accept", "Content-Type", "Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-ID"}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

type handler struct {
	cfg Config
}

// New returns the proxy handler, to be mounted at /mcp/.
func New(cfg Config) http.Handler {
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = DefaultTokenTTL
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if cfg.Transport == nil {
		cfg.Transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	return &handler{cfg: cfg}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Upstream content must never be interpreted as a document on the hub's origin.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	if !httpx.IsLoopbackHost(r.Host) || !allowedOrigin(r.Header) {
		slog.Warn("proxy rejected non-loopback request", "host", httpx.LogSafe(r.Host), "origin", httpx.LogSafe(r.Header.Get("Origin")))
		httpx.WriteError(w, http.StatusForbidden, httpx.CodeBadRequest, "request must come from a loopback origin")
		return
	}
	id, rest, ok := splitPath(r.URL.Path)
	var up Upstream
	if ok {
		up, ok = h.cfg.Resolver.Resolve(id)
	}
	if !ok {
		// Unknown IDs are not audited: nothing was proxied and the ID is caller-controlled.
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "no enabled MCP server with that id")
		return
	}

	start := time.Now()
	sw := &statusWriter{ResponseWriter: w}
	method, tool := r.Method, ""
	defer func() {
		status := sw.code()
		if sw.auditStatus != 0 {
			status = sw.auditStatus
		}
		h.cfg.Audit.Record(audit.Event{
			Time: start.UTC(), Server: id, Method: truncate(method), Tool: truncate(tool),
			Status: status, DurationMs: time.Since(start).Milliseconds(), User: h.cfg.User,
		})
	}()

	body, err := io.ReadAll(io.LimitReader(r.Body, h.cfg.MaxBodyBytes+1))
	if err != nil {
		httpx.WriteError(sw, http.StatusBadRequest, httpx.CodeBadRequest, "could not read request body")
		return
	}
	if int64(len(body)) > h.cfg.MaxBodyBytes {
		httpx.WriteError(sw, http.StatusRequestEntityTooLarge, httpx.CodeBadRequest, "request body too large")
		return
	}
	r.Body, r.ContentLength = io.NopCloser(bytes.NewReader(body)), int64(len(body))
	if len(body) == 0 {
		r.Body = http.NoBody
	}
	method, tool = describe(r.Method, body)

	target, err := url.Parse(up.URL)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		slog.Error("proxy upstream url invalid", "server", httpx.LogSafe(id))
		httpx.WriteError(sw, http.StatusBadGateway, httpx.CodeInternal, "upstream URL is invalid")
		return
	}
	authz, ok := h.authorization(id, up.Auth, target)
	if !ok {
		httpx.WriteError(sw, http.StatusBadGateway, httpx.CodeInternal, "could not obtain upstream credentials")
		return
	}
	h.reverseProxy(id, target, rest, authz, sw).ServeHTTP(sw, r)
}

// authorization returns the Authorization header value for the upstream, or
// ok=false if credentials are required but unavailable or would travel in
// cleartext (fail closed).
func (h *handler) authorization(id string, mode catalogue.AuthMode, target *url.URL) (string, bool) {
	switch mode {
	case catalogue.AuthNone:
		return "", true
	case catalogue.AuthHubJWT:
		if target.Scheme != "https" && !httpx.IsLoopbackName(target.Hostname()) {
			slog.Error("proxy refused to send hub token over plaintext http", "server", httpx.LogSafe(id))
			return "", false
		}
		tok, err := h.cfg.Tokens.Issue(h.cfg.User, id, h.cfg.TokenTTL)
		if err != nil {
			slog.Error("proxy token issue failed", "server", httpx.LogSafe(id), "err", httpx.LogSafe(err.Error()))
			return "", false
		}
		return "Bearer " + tok, true
	default:
		slog.Error("proxy unknown auth mode", "server", httpx.LogSafe(id), "auth", httpx.LogSafe(string(mode)))
		return "", false
	}
}

// splitPath turns /mcp/{id}/rest into (id, "/rest"). Dot segments are refused
// so the trailing path cannot escape the upstream's base path.
func splitPath(p string) (id, rest string, ok bool) {
	tail, found := strings.CutPrefix(p, "/mcp/")
	if !found {
		return "", "", false
	}
	id, rest, _ = strings.Cut(tail, "/")
	if rest != "" {
		rest = "/" + rest
	}
	for _, seg := range strings.Split(rest, "/") {
		if seg == ".." || seg == "." || strings.Contains(seg, `\`) {
			return "", "", false
		}
	}
	return id, rest, idPattern.MatchString(id)
}

func truncate(s string) string {
	if len(s) > MaxAuditField {
		return s[:MaxAuditField]
	}
	return s
}
