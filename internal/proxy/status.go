package proxy

import "net/http"

// statusWriter captures the response status for auditing while keeping
// streaming working: it implements http.Flusher and exposes Unwrap for
// http.ResponseController.
type statusWriter struct {
	http.ResponseWriter
	status int
	// auditStatus, when set, is recorded instead of the status sent to the
	// client (upstream 401/403 mapped to 502, or 499 on client disconnect).
	auditStatus int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (s *statusWriter) code() int {
	if s.status == 0 {
		return http.StatusOK
	}
	return s.status
}
