// Package httpx holds the JSON response helpers shared by every /api/v1 handler,
// so all errors follow the contract's {"error":{"code","message"}} shape.
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Error codes from api/openapi.yaml.
const (
	CodeNotFound            = "not_found"
	CodeNotApproved         = "not_approved"
	CodeChecksumMismatch    = "checksum_mismatch"
	CodeUnsupportedPlatform = "unsupported_platform"
	CodeRegistryUnavailable = "registry_unavailable"
	CodeStartFailed         = "start_failed"
	CodeConfigConflict      = "config_conflict"
	CodeConfigInvalid       = "config_invalid"
	CodeBadRequest          = "bad_request"
	CodeInternal            = "internal"
)

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response", "err", err)
	}
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// WriteError writes a contract-shaped error.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	var b errorBody
	b.Error.Code = code
	b.Error.Message = message
	WriteJSON(w, status, b)
}
