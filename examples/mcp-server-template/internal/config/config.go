// Package config loads the server's environment contract with mcp-hub.
//
// The hub starts every local MCP server with these variables set:
//
//	PORT          (required) TCP port to listen on; the server binds 127.0.0.1 only.
//	HUB_JWKS_URL  (required) URL of the hub's JWKS (public keys for token verification).
//	HUB_AUDIENCE  (required) expected "aud" claim; equals the catalogue entry id.
//	HUB_ISSUER    (optional) expected "iss" claim; defaults to "mcp-hub".
package config

import "errors"

// DefaultIssuer is the "iss" claim the hub puts in tokens unless HUB_ISSUER overrides it.
const DefaultIssuer = "mcp-hub"

// ErrInvalid is wrapped by every configuration error.
var ErrInvalid = errors.New("invalid configuration")

// Config is the validated runtime configuration.
type Config struct {
	Addr     string // always 127.0.0.1:PORT
	JWKSURL  string
	Audience string
	Issuer   string
}

// Load reads and validates the configuration using getenv (os.Getenv in production).
func Load(_ func(string) string) (Config, error) {
	return Config{}, ErrInvalid
}
