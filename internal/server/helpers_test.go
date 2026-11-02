package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
)

// localReq builds a request as Claude Code / the UI would send it: Host is the
// loopback daemon. Use it instead of httptest.NewRequest, whose default Host
// (example.com) is rejected by the server-wide loopback guard.
func localReq(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.Host = "127.0.0.1:7420"
	return r
}
