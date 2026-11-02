package proxy

import (
	"errors"
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
			if !allowedContentType(resp) {
				return errContentType
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			switch {
			case errors.Is(err, errUpstreamAuth), errors.Is(err, errContentType):
				slog.Warn("proxy blocked upstream response", "server", id, "reason", err.Error())
				httpx.WriteError(w, http.StatusBadGateway, httpx.CodeStartFailed, err.Error())
			case r.Context().Err() != nil:
				sw.auditStatus = StatusClientClosed // client went away; nobody to answer
			default:
				slog.Warn("proxy upstream unreachable", "server", id, "err", err)
				httpx.WriteError(w, http.StatusBadGateway, httpx.CodeStartFailed, "upstream MCP server unreachable")
			}
		},
		FlushInterval: -1,
		Transport:     h.cfg.Transport,
		ErrorLog:      slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}
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
