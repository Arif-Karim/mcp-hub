package audit

import (
	"net/http"
	"strconv"

	"github.com/Arif-Karim/mcp-hub/internal/httpx"
)

// Limits for GET /api/v1/audit?limit=.
const (
	DefaultLimit = 50
	MaxLimit     = 500
)

// Handler serves GET /api/v1/audit.
func Handler(l *Log) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := DefaultLimit
		if s := r.URL.Query().Get("limit"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 || n > MaxLimit {
				httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "limit must be an integer between 1 and 500")
				return
			}
			limit = n
		}
		httpx.WriteJSON(w, http.StatusOK, map[string][]Event{"events": l.Recent(limit)})
	})
}
