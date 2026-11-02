package httpx

import "strings"

// LogSafe strips CR/LF from request-derived values before they are logged.
// slog's JSON handler already escapes them, but stripping keeps logs safe for
// any handler (e.g. text) and makes the intent explicit (CodeQL go/log-injection).
func LogSafe(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	return strings.ReplaceAll(s, "\r", "")
}
