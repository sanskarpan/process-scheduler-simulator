// Package middleware provides HTTP middleware: request-ID injection,
// structured logging, Prometheus metrics, panic recovery, and CORS.
package middleware

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/sanskar/scheduler-simulator/internal/logging"
	"github.com/sanskar/scheduler-simulator/internal/metrics"
)

// statusWriter wraps http.ResponseWriter to capture the status code and bytes
// written, for logging and metrics.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Hijack delegates to the underlying ResponseWriter's Hijacker interface so
// that WebSocket upgrades (which require http.Hijacker) work through the
// logging middleware without a 500 "does not implement http.Hijacker" error.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("statusWriter: underlying ResponseWriter does not implement http.Hijacker")
	}
	return h.Hijack()
}

// Flush delegates to the underlying ResponseWriter's Flusher interface for
// streaming responses (SSE, chunked transfers).
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// SecureHeaders sets defensive HTTP response headers on every response.
// It must run before any handler that writes a body so the headers are
// always present even on error responses.
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Narrow CSP: API/WS only serves JSON and static assets; no inline scripts.
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:")
		next.ServeHTTP(w, r)
	})
}

// RequestID injects a request ID into the request context and response
// headers. If the client provides an X-Request-ID, it is reused (after
// truncation and sanitization) to support distributed tracing.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = logging.NextRequestID()
		} else {
			// Sanitize: keep only printable ASCII (0x20–0x7E) to prevent
			// CRLF injection into the reflected response header.
			id = sanitizeHeaderValue(id)
			if id == "" {
				id = logging.NextRequestID()
			}
		}
		if len(id) > 64 {
			id = id[:64]
		}
		ctx := logging.ContextWithRequestID(r.Context(), id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// sanitizeHeaderValue strips any byte outside printable ASCII (0x20–0x7E).
func sanitizeHeaderValue(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b >= 0x20 && b <= 0x7E {
			out = append(out, b)
		}
	}
	return string(out)
}

// LogAndMetrics logs each request and records Prometheus metrics. It must run
// after RequestID so log entries carry the correlation ID.
func LogAndMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 0}
		next.ServeHTTP(sw, r)

		duration := time.Since(start)
		path := routePath(r)
		metrics.ObserveHTTPRequest(r.Method, path, sw.status, duration, r.ContentLength)

		logging.FromContext(r.Context()).Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"route", path,
			"status", sw.status,
			"bytes", sw.bytes,
			"duration_ms", duration.Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

// Recovery catches panics from downstream handlers, logs them with a stack
// trace, and returns a 500 without crashing the process. It is essential for
// the WebSocket handlers, which previously could crash the whole server on a
// malformed message.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logging.FromContext(r.Context()).Error("panic recovered",
					"error", rec,
					"stack", string(debug.Stack()),
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// CORS applies CORS headers for recognized origins. Requests from origins not
// in allowedOrigins receive no ACAO header so browsers block them. It handles
// preflight OPTIONS requests.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && originAllowed(origin, allowedOrigins) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
				w.Header().Set("Access-Control-Max-Age", "300")
			}
			// No else: unrecognised or absent origins get no ACAO header.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func originAllowed(origin string, allowed []string) bool {
	for _, a := range allowed {
		if a == "*" || a == origin {
			return true
		}
	}
	return false
}

// routePath returns the matched route template if available (set by the router),
// otherwise it returns the cleaned URL path. This keeps metrics cardinality
// bounded (e.g. "/api/simulations/{id}" not "/api/simulations/abc123").
func routePath(r *http.Request) string {
	if v := r.PathValue("route"); v != "" {
		return v
	}
	p := r.URL.Path
	// Collapse /api/simulations/<id> to /api/simulations/{id} for metrics.
	if strings.HasPrefix(p, "/api/simulations/") {
		return "/api/simulations/{id}"
	}
	if strings.HasPrefix(p, "/api/simulate") {
		return "/api/simulate"
	}
	return p
}

// Chain composes multiple middlewares: outermost first.
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
