package server

import (
	"errors"
	"net/http"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/httpx"
)

// catalogueItem is the contract's CatalogueItem: the entry plus install state.
// Entry.Artifacts is tagged json:"-", so artifact URLs never reach the API.
type catalogueItem struct {
	catalogue.Entry
	State catalogue.InstallState `json:"state"`
}

func registerCatalogue(mux *http.ServeMux, store *catalogue.Store, state func(string) catalogue.InstallState) {
	if state == nil {
		state = disabledState
	}
	item := func(e catalogue.Entry) catalogueItem { return newItem(e, state(e.ID)) }

	mux.HandleFunc("GET /api/v1/catalogue", func(w http.ResponseWriter, r *http.Request) {
		kind := catalogue.Kind(r.URL.Query().Get("kind"))
		if kind != "" && kind != catalogue.KindLocal && kind != catalogue.KindRemote {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "kind must be local or remote")
			return
		}
		items := []catalogueItem{}
		for _, e := range store.Search(r.URL.Query().Get("q"), kind) {
			items = append(items, item(e))
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	})
	mux.HandleFunc("GET /api/v1/catalogue/{id}", func(w http.ResponseWriter, r *http.Request) {
		e, err := store.Get(r.PathValue("id"))
		if errors.Is(err, catalogue.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "no such catalogue entry")
			return
		}
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "catalogue lookup failed")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item(e))
	})
	// See the health route in New: without these the /api/ catch-all answers 404.
	mux.HandleFunc("/api/v1/catalogue", methodNotAllowed)
	mux.HandleFunc("/api/v1/catalogue/{id}", methodNotAllowed)
}

func newItem(e catalogue.Entry, state catalogue.InstallState) catalogueItem {
	// tags and tools are required arrays in the contract: never emit null.
	if e.Tags == nil {
		e.Tags = []string{}
	}
	if e.Tools == nil {
		e.Tools = []catalogue.Tool{}
	}
	return catalogueItem{Entry: e, State: state}
}

func disabledState(id string) catalogue.InstallState {
	return catalogue.InstallState{Status: catalogue.StatusDisabled, ConfigName: catalogue.ConfigName(id)}
}
