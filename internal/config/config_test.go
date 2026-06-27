package config

import (
	"strings"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	c := Default()
	if c.Port != ":8082" {
		t.Errorf("Port = %q, want :8082", c.Port)
	}
	if c.StaticDir != "./web/static" {
		t.Errorf("StaticDir = %q", c.StaticDir)
	}
	if c.LogLevel != "info" {
		t.Errorf("LogLevel = %q", c.LogLevel)
	}
	if c.LogFormat != "json" {
		t.Errorf("LogFormat = %q", c.LogFormat)
	}
	if c.DefaultSpeed != 100 {
		t.Errorf("DefaultSpeed = %d", c.DefaultSpeed)
	}
	if c.DefaultTimeQuantum != 4 {
		t.Errorf("DefaultTimeQuantum = %d", c.DefaultTimeQuantum)
	}
	if c.BroadcastBufferSize != 64 {
		t.Errorf("BroadcastBufferSize = %d", c.BroadcastBufferSize)
	}
	if c.MaxClients != 0 {
		t.Errorf("MaxClients = %d", c.MaxClients)
	}
	if c.WSReadLimit != 4*1024 {
		t.Errorf("WSReadLimit = %d", c.WSReadLimit)
	}
	if c.WSWriteWait != 10*time.Second {
		t.Errorf("WSWriteWait = %v", c.WSWriteWait)
	}
	if c.WSPongWait != 60*time.Second {
		t.Errorf("WSPongWait = %v", c.WSPongWait)
	}
	if c.WSPingPeriod != 30*time.Second {
		t.Errorf("WSPingPeriod = %v", c.WSPingPeriod)
	}
	if !c.EnableMetrics {
		t.Errorf("EnableMetrics = false")
	}
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("LOG_LEVEL", "DEBUG")
	t.Setenv("STATIC_DIR", "/var/static")
	t.Setenv("DEFAULT_SPEED", "50")
	t.Setenv("BROADCAST_BUFFER_SIZE", "128")
	t.Setenv("MAX_CLIENTS", "10")
	t.Setenv("ENABLE_METRICS", "false")
	t.Setenv("ALLOWED_ORIGINS", "https://a.example, https://b.example")

	c := FromEnv()
	if c.Port != ":9090" {
		t.Errorf("Port = %q, want :9090", c.Port)
	}
	if c.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", c.LogLevel)
	}
	if c.StaticDir != "/var/static" {
		t.Errorf("StaticDir = %q", c.StaticDir)
	}
	if c.DefaultSpeed != 50 {
		t.Errorf("DefaultSpeed = %d, want 50", c.DefaultSpeed)
	}
	if c.BroadcastBufferSize != 128 {
		t.Errorf("BroadcastBufferSize = %d, want 128", c.BroadcastBufferSize)
	}
	if c.MaxClients != 10 {
		t.Errorf("MaxClients = %d, want 10", c.MaxClients)
	}
	if c.EnableMetrics {
		t.Errorf("EnableMetrics = true, want false")
	}
	want := []string{"https://a.example", "https://b.example"}
	if len(c.WSOriginAllow) != len(want) {
		t.Fatalf("WSOriginAllow = %v, want %v", c.WSOriginAllow, want)
	}
	for i, o := range want {
		if c.WSOriginAllow[i] != o {
			t.Errorf("WSOriginAllow[%d] = %q, want %q", i, c.WSOriginAllow[i], o)
		}
	}
}

func TestValidateValidConfig(t *testing.T) {
	if _, err := Default().Validate(); err != nil {
		t.Fatalf("Default().Validate() returned error: %v", err)
	}
}

func TestValidateInvalidPort(t *testing.T) {
	c := Default()
	c.Port = ":abc"
	if _, err := c.Validate(); err == nil {
		t.Fatal("expected error for non-numeric port, got nil")
	} else if !strings.Contains(err.Error(), "port") {
		t.Fatalf("expected port error, got: %v", err)
	}
}

func TestValidateInvalidLogLevel(t *testing.T) {
	c := Default()
	c.LogLevel = "bogus"
	if _, err := c.Validate(); err == nil {
		t.Fatal("expected error for bogus log level, got nil")
	} else if !strings.Contains(err.Error(), "log_level") {
		t.Fatalf("expected log_level error, got: %v", err)
	}
}

func TestValidatePingPong(t *testing.T) {
	c := Default()
	c.WSPingPeriod = c.WSPongWait // ping >= pong -> invalid
	if _, err := c.Validate(); err == nil {
		t.Fatal("expected error when WSPingPeriod >= WSPongWait, got nil")
	}
}
