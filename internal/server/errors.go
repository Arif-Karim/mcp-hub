package server

import (
	"errors"
	"net/http"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/httpx"
	"github.com/Arif-Karim/mcp-hub/internal/registry"
)

// ErrorStatus maps catalogue/registry sentinel errors to the contract's HTTP
// status and error code (api/openapi.yaml). Unknown errors are 500 internal.
func ErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, catalogue.ErrNotFound):
		return http.StatusNotFound, httpx.CodeNotFound
	case errors.Is(err, catalogue.ErrNotApproved):
		return http.StatusForbidden, httpx.CodeNotApproved
	case errors.Is(err, registry.ErrChecksumMismatch):
		return http.StatusUnprocessableEntity, httpx.CodeChecksumMismatch
	case errors.Is(err, registry.ErrArtifactRejected):
		return http.StatusUnprocessableEntity, httpx.CodeArtifactRejected
	case errors.Is(err, registry.ErrUnsupportedPlatform):
		return http.StatusUnprocessableEntity, httpx.CodeUnsupportedPlatform
	case errors.Is(err, registry.ErrRegistryUnavailable):
		return http.StatusBadGateway, httpx.CodeRegistryUnavailable
	default:
		return http.StatusInternalServerError, httpx.CodeInternal
	}
}
