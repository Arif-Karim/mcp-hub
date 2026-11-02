package server

import (
	"log/slog"
	"net/http"

	"github.com/Arif-Karim/mcp-hub/internal/httpx"
	"github.com/Arif-Karim/mcp-hub/internal/skillfiles"
)

// Skill installs the embedded Claude Code skill (skillfiles.Manager).
type Skill interface {
	State() skillfiles.State
	Install() (skillfiles.State, error)
}

// registerSkill serves POST /api/v1/skill/install. It changes files under
// ~/.claude, so it relies on the loopback guard's cross-site Origin check.
func registerSkill(mux *http.ServeMux, s Skill) {
	mux.HandleFunc("POST /api/v1/skill/install", func(w http.ResponseWriter, _ *http.Request) {
		st, err := s.Install()
		if err != nil {
			// The error carries filesystem paths: log it, do not echo it.
			slog.Error("skill install failed", "err", err)
			httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal,
				"could not install the Claude Code skill; see the daemon log")
			return
		}
		slog.Info("skill installed", "path", st.Path)
		httpx.WriteJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("/api/v1/skill/install", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", "POST")
		httpx.WriteError(w, http.StatusMethodNotAllowed, httpx.CodeBadRequest, "method not allowed")
	})
}
