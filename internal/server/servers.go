package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/httpx"
	"github.com/Arif-Karim/mcp-hub/internal/skillfiles"
)

// Enabler enables and disables catalogue entries (hub.Service).
type Enabler interface {
	Enable(ctx context.Context, id string) (catalogue.InstallState, error)
	Disable(ctx context.Context, id string) (catalogue.InstallState, error)
}

// Status is the contract's Status schema (GET /api/v1/status).
type Status struct {
	Version          string `json:"version"`
	Addr             string `json:"addr"`
	ClaudeConfigPath string `json:"claudeConfigPath"`
	EnabledCount     int    `json:"enabledCount"`
	CatalogueSource  string `json:"catalogueSource"`
	User             string `json:"user"`
	// Skill is filled from Options.Skill; absent when the daemon cannot manage the skill.
	Skill *skillfiles.State `json:"skill,omitempty"`
}

func registerServers(mux *http.ServeMux, store *catalogue.Store, ctl Enabler) {
	action := func(name string, do func(context.Context, string) (catalogue.InstallState, error)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			e, err := store.Get(id)
			if err != nil {
				writeErr(w, err)
				return
			}
			state, err := do(r.Context(), id)
			if err != nil {
				slog.Warn("server "+name+" failed", "server", id, "err", err)
				writeErr(w, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, newItem(e, state))
		}
	}
	mux.HandleFunc("POST /api/v1/servers/{id}/enable", action("enable", ctl.Enable))
	mux.HandleFunc("POST /api/v1/servers/{id}/disable", action("disable", ctl.Disable))
	post := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", "POST")
		httpx.WriteError(w, http.StatusMethodNotAllowed, httpx.CodeBadRequest, "method not allowed")
	}
	mux.HandleFunc("/api/v1/servers/{id}/enable", post)
	mux.HandleFunc("/api/v1/servers/{id}/disable", post)
}

func registerStatus(mux *http.ServeMux, status func() Status, skill Skill) {
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, _ *http.Request) {
		s := status()
		if skill != nil {
			st := skill.State()
			s.Skill = &st
		}
		httpx.WriteJSON(w, http.StatusOK, s)
	})
	mux.HandleFunc("/api/v1/status", methodNotAllowed)
}

// writeErr maps err to the contract. Internal errors are not echoed (they may
// contain paths); contract errors carry their message, which the UI shows.
func writeErr(w http.ResponseWriter, err error) {
	status, code := ErrorStatus(err)
	msg := err.Error()
	if code == httpx.CodeInternal {
		msg = "internal error"
	}
	httpx.WriteError(w, status, code, msg)
}
