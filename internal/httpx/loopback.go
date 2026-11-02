package httpx

import (
	"net"
	"net/url"
	"strings"
)

// IsLoopbackName reports whether a bare hostname is a loopback name.
func IsLoopbackName(h string) bool {
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

// IsLoopbackHost checks a Host header value (with or without port). Used to
// defeat DNS rebinding: a rebound name resolves to 127.0.0.1 but keeps its Host.
func IsLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
	}
	return IsLoopbackName(host)
}

// IsLoopbackOrigin reports whether an Origin header value is an http page
// served from loopback (any port). "null" and other schemes are rejected.
func IsLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "http" && IsLoopbackName(u.Hostname())
}
