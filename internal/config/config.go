// Package config provides environment-based configuration with defaults and
// validation. It centralizes all runtime knobs so the rest of the codebase
// depends on a single Config struct instead of scattered os.Getenv calls.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all server runtime configuration.
type Config struct {
	// HTTP
	Port           string        // listen port, e.g. ":8082"
	ReadTimeout    time.Duration // header read timeout
	WriteTimeout   time.Duration // write timeout (0 = no deadline, needed for WS)
	IdleTimeout    time.Duration // keep-alive idle timeout
	ShutdownTimeout time.Duration

	// Static files
	StaticDir string // absolute or relative path to web/static

	// Logging
	LogLevel  string // debug, info, warn, error
	LogFormat string // json, text

	// Simulation defaults
	DefaultSpeed      int // ms per tick
	DefaultTimeQuantum int // default RR quantum

	// Broadcast
	BroadcastBufferSize int // WS broadcast channel size
	MaxClients          int // 0 = unlimited

	// WebSocket
	WSReadLimit     int64         // max inbound message bytes
	WSWriteWait     time.Duration // write deadline
	WSPongWait      time.Duration // read deadline (pong)
	WSPingPeriod    time.Duration // ping interval
	WSOriginAllow   []string      // allowed origins beyond same-origin

	// Feature flags
	EnableMetrics bool // expose /metrics
}

// Default returns a Config with sensible production defaults.
func Default() Config {
	return Config{
		Port:                ":8082",
		ReadTimeout:         5 * time.Second,
		WriteTimeout:        0,
		IdleTimeout:         120 * time.Second,
		ShutdownTimeout:     5 * time.Second,
		StaticDir:           "./web/static",
		LogLevel:            "info",
		LogFormat:           "json",
		DefaultSpeed:        100,
		DefaultTimeQuantum:  4,
		BroadcastBufferSize: 64,
		MaxClients:          0,
		WSReadLimit:         4 * 1024,
		WSWriteWait:         10 * time.Second,
		WSPongWait:          60 * time.Second,
		WSPingPeriod:        30 * time.Second,
		WSOriginAllow:       nil,
		EnableMetrics:       true,
	}
}

// FromEnv loads configuration from environment variables, starting from
// Default() and overriding each value found in the environment.
func FromEnv() Config {
	c := Default()

	if v := os.Getenv("PORT"); v != "" {
		c.Port = normalizePort(v)
	}
	if v := os.Getenv("STATIC_DIR"); v != "" {
		c.StaticDir = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		c.LogLevel = strings.ToLower(v)
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		c.LogFormat = strings.ToLower(v)
	}
	if v := os.Getenv("DEFAULT_SPEED"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.DefaultSpeed = n
		}
	}
	if v := os.Getenv("DEFAULT_TIME_QUANTUM"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.DefaultTimeQuantum = n
		}
	}
	if v := os.Getenv("BROADCAST_BUFFER_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.BroadcastBufferSize = n
		}
	}
	if v := os.Getenv("MAX_CLIENTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			c.MaxClients = n
		}
	}
	if v := os.Getenv("WS_READ_LIMIT_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			c.WSReadLimit = n
		}
	}
	if v := os.Getenv("ENABLE_METRICS"); v != "" {
		c.EnableMetrics = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("ALLOWED_ORIGINS"); v != "" {
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				c.WSOriginAllow = append(c.WSOriginAllow, o)
			}
		}
	}
	return c
}

// Validate checks the configuration for errors and returns a sanitized copy.
func (c Config) Validate() (Config, error) {
	if c.Port == "" {
		return c, fmt.Errorf("port must not be empty")
	}
	if !strings.HasPrefix(c.Port, ":") {
		return c, fmt.Errorf("port must start with ':' got %q", c.Port)
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(c.Port, ":")); err != nil {
		return c, fmt.Errorf("port must be numeric: %w", err)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return c, fmt.Errorf("invalid log_level %q (want debug|info|warn|error)", c.LogLevel)
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		return c, fmt.Errorf("invalid log_format %q (want json|text)", c.LogFormat)
	}
	if c.ReadTimeout <= 0 {
		return c, fmt.Errorf("read_timeout must be positive")
	}
	if c.IdleTimeout <= 0 {
		return c, fmt.Errorf("idle_timeout must be positive")
	}
	if c.WSPongPeriodValid() != nil {
		return c, c.WSPongPeriodValid()
	}
	return c, nil
}

// WSPongPeriodValid returns nil if the ping/pong config is internally
// consistent.
func (c Config) WSPongPeriodValid() error {
	if c.WSPingPeriod >= c.WSPongWait {
		return fmt.Errorf("ws_ping_period (%s) must be < ws_pong_wait (%s)", c.WSPingPeriod, c.WSPongWait)
	}
	return nil
}

func normalizePort(v string) string {
	if !strings.HasPrefix(v, ":") {
		return ":" + v
	}
	return v
}
