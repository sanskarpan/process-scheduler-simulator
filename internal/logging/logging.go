// Package logging provides a structured logger built on log/slog, with
// request-scoped correlation IDs and redaction of sensitive values.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
)

// Logger is the application-wide structured logger.
var Logger *slog.Logger

// requestIDCounter is a per-process monotonic counter used to generate unique
// request IDs when the client does not provide one. It wraps around at 2^31.
var requestIDCounter uint32

// contextKey is a private type to avoid colliding with other context keys.
type contextKey struct{ name string }

var (
	requestIDKey = contextKey{"requestID"}
)

// Init initializes the global Logger from the given level/format strings.
// level: debug|info|warn|error. format: json|text.
func Init(level, format string) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level:     lvl,
		AddSource: true,
	}
	switch strings.ToLower(format) {
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	default:
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	Logger = slog.New(handler)
	slog.SetDefault(Logger)
}

// FromContext returns the global logger enriched with any request ID stored in
// the context. If none is present, the base logger is returned.
func FromContext(ctx context.Context) *slog.Logger {
	if Logger == nil {
		Init("info", "json")
	}
	if id, ok := ctx.Value(requestIDKey).(string); ok && id != "" {
		return Logger.With("request_id", id)
	}
	return Logger
}

// ContextWithRequestID returns a new context carrying the given request ID.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext extracts the request ID from a context, if present.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// NextRequestID returns a short unique request ID. Uniqueness is best-effort
// and monotonic per process; it wraps at 2^31. It is suitable for log
// correlation, not for security-sensitive tracking.
func NextRequestID() string {
	n := atomic.AddUint32(&requestIDCounter, 1)
	return fmtUint32Base36(n)
}

// fmtUint32Base36 encodes n as a compact base-36 string.
func fmtUint32Base36(n uint32) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if n == 0 {
		return "0"
	}
	var buf [7]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%36]
		n /= 36
	}
	return string(buf[i:])
}
