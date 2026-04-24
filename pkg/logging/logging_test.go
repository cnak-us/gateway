package logging

import (
	"log/slog"
	"os"
	"testing"
)

func TestSetup(t *testing.T) {
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("LOG_FORMAT", "json")
	defer func() {
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("LOG_FORMAT")
	}()

	logger := Setup("test-service")
	if logger == nil {
		t.Fatal("Setup returned nil logger")
	}
	if !logger.Enabled(nil, slog.LevelDebug) {
		t.Error("debug level should be enabled")
	}
}

func TestSetupTextFormat(t *testing.T) {
	os.Setenv("LOG_FORMAT", "text")
	defer os.Unsetenv("LOG_FORMAT")

	logger := Setup("test-service")
	if logger == nil {
		t.Fatal("Setup returned nil logger")
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}
	for _, tt := range tests {
		if got := parseLevel(tt.input); got != tt.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestEnableNATSNilConn(t *testing.T) {
	Setup("test-service")
	// Should not panic with nil connection
	EnableNATS("test-service", nil)
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(5)
	// Should allow initial burst
	for i := 0; i < 5; i++ {
		if !rl.allow() {
			t.Errorf("allow() returned false on attempt %d, expected true", i)
		}
	}
	// Should be exhausted
	if rl.allow() {
		t.Error("allow() returned true after exhausting tokens")
	}
}
