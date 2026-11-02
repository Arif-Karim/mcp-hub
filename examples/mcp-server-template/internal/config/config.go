// Package config loads the server's environment contract with mcp-hub.
//
// The hub starts every local MCP server with these variables set:
//
//	PORT          (required) TCP port to listen on; the server binds 127.0.0.1 only.
//	HUB_JWKS_URL  (required) URL of the hub's JWKS (public keys for token verification).
//	HUB_AUDIENCE  (required) expected "aud" claim; equals the catalogue entry id.
//	HUB_ISSUER    (optional) expected "iss" claim; defaults to "mcp-hub".
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

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
func Load(getenv func(string) string) (Config, error) {
	get := func(k string) string { return strings.TrimSpace(getenv(k)) }

	port, err := required(get, "PORT")
	if err != nil {
		return Config{}, err
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return Config{}, fmt.Errorf("%w: PORT must be an integer in 1-65535, got %q", ErrInvalid, port)
	}

	jwks, err := required(get, "HUB_JWKS_URL")
	if err != nil {
		return Config{}, err
	}
	if !trustedJWKSURL(jwks) {
		return Config{}, fmt.Errorf("%w: HUB_JWKS_URL must be an https URL or http on a loopback host", ErrInvalid)
	}

	aud, err := required(get, "HUB_AUDIENCE")
	if err != nil {
		return Config{}, err
	}

	iss := get("HUB_ISSUER")
	if iss == "" {
		iss = DefaultIssuer
	}

	return Config{
		Addr:     net.JoinHostPort("127.0.0.1", port),
		JWKSURL:  jwks,
		Audience: aud,
		Issuer:   iss,
	}, nil
}

// trustedJWKSURL allows https anywhere and plain http only to loopback (the
// local hub), so keys cannot be fetched in clear text from the network.
func trustedJWKSURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		h := u.Hostname()
		if h == "localhost" {
			return true
		}
		ip := net.ParseIP(h)
		return ip != nil && ip.IsLoopback()
	default:
		return false
	}
}

func required(get func(string) string, key string) (string, error) {
	v := get(key)
	if v == "" {
		return "", fmt.Errorf("%w: %s is required", ErrInvalid, key)
	}
	return v, nil
}
