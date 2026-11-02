package server

import (
	"log/slog"
	"net/http"

	"github.com/Arif-Karim/mcp-hub/internal/httpx"
)

// loopbackGuard protects every route (API, proxy, UI) from browsers:
//   - a non-loopback Host means DNS rebinding → 403, for all methods;
//   - a state-changing request with a present, non-loopback Origin (including
//     "null") is cross-site → 403. CLI / Claude Code send no Origin.
func loopbackGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !httpx.IsLoopbackHost(r.Host) {
			slog.Warn("rejected non-loopback Host", "host", httpx.LogSafe(r.Host), "path", httpx.LogSafe(r.URL.Path))
			httpx.WriteError(w, http.StatusForbidden, httpx.CodeBadRequest, "host not allowed")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origins := r.Header.Values("Origin"); len(origins) > 0 &&
				(len(origins) > 1 || !httpx.IsLoopbackOrigin(origins[0])) {
				slog.Warn("rejected cross-site request", "origin", httpx.LogSafe(r.Header.Get("Origin")), "path", httpx.LogSafe(r.URL.Path))
				httpx.WriteError(w, http.StatusForbidden, httpx.CodeBadRequest, "cross-site request not allowed")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
