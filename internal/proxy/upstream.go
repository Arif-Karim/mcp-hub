package proxy

import (
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/Arif-Karim/mcp-hub/internal/httpx"
)

var (
	errContentType  = errors.New("unexpected upstream content type")
	errUpstreamAuth = errors.New("upstream rejected hub credentials")
)

// stripResponseHeaders are removed from every upstream response: cookies and
// auth challenges (Claude Code would start an OAuth flow against the upstream),
// plus any security headers the upstream tries to weaken ours with.
var stripResponseHeaders = []string{"Set-Cookie", "WWW-Authenticate", "Content-Security-Policy", "X-Content-Type-Options"}

func (h *handler) reverseProxy(id string, target *url.URL, rest, authz string, sw *statusWriter) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			out := pr.Out
			out.URL.Scheme, out.URL.Host = target.Scheme, target.Host
			out.URL.Path, out.URL.RawPath = target.Path+rest, ""
			out.Host = "" // send the upstream's own Host
			out.Header = http.Header{}
			for _, k := range forwardHeaders {
				if v := pr.In.Header.Values(k); len(v) > 0 {
					out.Header[k] = v
				}
			}
			if authz != "" {
				out.Header.Set("Authorization", authz)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			for _, k := range stripResponseHeaders {
				resp.Header.Del(k)
			}
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				sw.auditStatus = resp.StatusCode
				return errUpstreamAuth
			}
			if resp.StatusCode == http.StatusAccepted && resp.Header.Get("Content-Type") == "" {
				// MCP answers notifications with 202 and no content; servers may
				// send it chunked. Drop any body rather than forward it untyped.
				dropBody(id, resp)
				return nil
			}
			if resp.StatusCode >= 400 && resp.StatusCode < 500 && !allowedContentType(resp) {
				// MCP signals with client errors: 404 = session expired, so the
				// client re-initializes; 405 = optional GET SSE stream refused
				// (DeepWiki, as text/plain); 400/406 = bad request or missing
				// Accept (go-sdk, as text/plain). Pass the status through so the
				// client can react, but never the untyped body. (401/403 were
				// handled above.)
				dropBody(id, resp)
				return nil
			}
			if !allowedContentType(resp) {
				return errContentType
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			switch {
			case errors.Is(err, errUpstreamAuth), errors.Is(err, errContentType):
				slog.Warn("proxy blocked upstream response", "server", httpx.LogSafe(id), "reason", httpx.LogSafe(err.Error()))
				httpx.WriteError(w, http.StatusBadGateway, httpx.CodeStartFailed, err.Error())
			case r.Context().Err() != nil:
				sw.auditStatus = StatusClientClosed // client went away; nobody to answer
			default:
				slog.Warn("proxy upstream unreachable", "server", httpx.LogSafe(id), "err", httpx.LogSafe(err.Error()))
				httpx.WriteError(w, http.StatusBadGateway, httpx.CodeStartFailed, "upstream MCP server unreachable")
			}
		},
		FlushInterval: -1,
		Transport:     h.cfg.Transport,
		ErrorLog:      slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}
}

// dropBody discards resp's body and content headers, draining a little first
// so the upstream connection can be reused.
func dropBody(id string, resp *http.Response) {
	if n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10)); n > 0 {
		slog.Debug("proxy dropped upstream body", "server", httpx.LogSafe(id), "status", resp.StatusCode, "bytes", n)
	}
	_ = resp.Body.Close()
	resp.Body, resp.ContentLength = http.NoBody, 0
	resp.TransferEncoding = nil
	resp.Header.Del("Content-Length")
	resp.Header.Del("Content-Type")
}

// allowedContentType passes only what MCP streamable HTTP uses. Anything else
// (notably text/html or SVG) could run script on the hub UI's origin.
func allowedContentType(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		return resp.ContentLength == 0 || resp.StatusCode == http.StatusNoContent
	}
	mt, _, err := mime.ParseMediaType(ct)
	return err == nil && (mt == "application/json" || mt == "text/event-stream")
}
