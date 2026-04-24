package config

import (
	"os"
	"testing"
)

func TestLoadFromEnv(t *testing.T) {
	// Save and restore env vars
	t.Run("store-forward defaults", func(t *testing.T) {
		cfg := LoadFromEnv()
		if !cfg.StoreForwardEnabled {
			t.Error("expected StoreForwardEnabled true by default")
		}
		if cfg.StoreForwardWindow != "5m" {
			t.Errorf("expected StoreForwardWindow '5m', got %q", cfg.StoreForwardWindow)
		}
	})

	t.Run("store-forward env overrides", func(t *testing.T) {
		os.Setenv("STORE_FORWARD_ENABLED", "false")
		os.Setenv("STORE_FORWARD_WINDOW", "10m")
		defer func() {
			os.Unsetenv("STORE_FORWARD_ENABLED")
			os.Unsetenv("STORE_FORWARD_WINDOW")
		}()

		cfg := LoadFromEnv()
		if cfg.StoreForwardEnabled {
			t.Error("expected StoreForwardEnabled false")
		}
		if cfg.StoreForwardWindow != "10m" {
			t.Errorf("expected StoreForwardWindow '10m', got %q", cfg.StoreForwardWindow)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		cfg := LoadFromEnv()

		if cfg.TLSStreamingPort != 8089 {
			t.Errorf("expected TLSStreamingPort 8089, got %d", cfg.TLSStreamingPort)
		}
		if cfg.HTTPSAPIPort != 8446 {
			t.Errorf("expected HTTPSAPIPort 8446, got %d", cfg.HTTPSAPIPort)
		}
		if cfg.TCPStreamingPort != 8088 {
			t.Errorf("expected TCPStreamingPort 8088, got %d", cfg.TCPStreamingPort)
		}
		if cfg.TCPEnabled != false {
			t.Error("expected TCPEnabled false")
		}
		if cfg.NATSURL != "nats://nats-server:4222" {
			t.Errorf("expected default NATS URL, got %q", cfg.NATSURL)
		}
		if cfg.CACertFile != "/app/data/ca.crt" {
			t.Errorf("expected default CACertFile, got %q", cfg.CACertFile)
		}
		if cfg.CAKeyFile != "/app/data/ca.key" {
			t.Errorf("expected default CAKeyFile, got %q", cfg.CAKeyFile)
		}
		if cfg.CAOrganization != "CNAK TAK Gateway" {
			t.Errorf("expected default CAOrganization, got %q", cfg.CAOrganization)
		}
		if cfg.CACertExpiry != 365 {
			t.Errorf("expected default CACertExpiry 365, got %d", cfg.CACertExpiry)
		}
		if cfg.LogLevel != "info" {
			t.Errorf("expected default LogLevel 'info', got %q", cfg.LogLevel)
		}
		if cfg.LogFormat != "json" {
			t.Errorf("expected default LogFormat 'json', got %q", cfg.LogFormat)
		}
		if cfg.ManagementPort != 8090 {
			t.Errorf("expected ManagementPort 8090, got %d", cfg.ManagementPort)
		}
	})

	t.Run("env overrides", func(t *testing.T) {
		os.Setenv("TLS_STREAMING_PORT", "9089")
		os.Setenv("HTTPS_API_PORT", "9446")
		os.Setenv("TCP_ENABLED", "true")
		os.Setenv("NATS_URL", "nats://custom:4222")
		os.Setenv("CA_ORGANIZATION", "CustomOrg")
		os.Setenv("LOG_LEVEL", "debug")
		os.Setenv("SERVER_SANS", "host1.example.com, 10.0.0.1, host2.example.com")
		os.Setenv("BACKEND_URL", "http://backend:8080")
		os.Setenv("POD_NAME", "test-pod")

		defer func() {
			os.Unsetenv("TLS_STREAMING_PORT")
			os.Unsetenv("HTTPS_API_PORT")
			os.Unsetenv("TCP_ENABLED")
			os.Unsetenv("NATS_URL")
			os.Unsetenv("CA_ORGANIZATION")
			os.Unsetenv("LOG_LEVEL")
			os.Unsetenv("SERVER_SANS")
			os.Unsetenv("BACKEND_URL")
			os.Unsetenv("POD_NAME")
		}()

		cfg := LoadFromEnv()

		if cfg.TLSStreamingPort != 9089 {
			t.Errorf("expected TLSStreamingPort 9089, got %d", cfg.TLSStreamingPort)
		}
		if cfg.HTTPSAPIPort != 9446 {
			t.Errorf("expected HTTPSAPIPort 9446, got %d", cfg.HTTPSAPIPort)
		}
		if cfg.TCPEnabled != true {
			t.Error("expected TCPEnabled true")
		}
		if cfg.NATSURL != "nats://custom:4222" {
			t.Errorf("expected custom NATS URL, got %q", cfg.NATSURL)
		}
		if cfg.CAOrganization != "CustomOrg" {
			t.Errorf("expected CustomOrg, got %q", cfg.CAOrganization)
		}
		if cfg.LogLevel != "debug" {
			t.Errorf("expected debug, got %q", cfg.LogLevel)
		}
		if len(cfg.ServerSANs) != 3 {
			t.Errorf("expected 3 SANs, got %d: %v", len(cfg.ServerSANs), cfg.ServerSANs)
		}
		if cfg.BackendURL != "http://backend:8080" {
			t.Errorf("expected backend URL, got %q", cfg.BackendURL)
		}
		if cfg.PodName != "test-pod" {
			t.Errorf("expected pod name, got %q", cfg.PodName)
		}
	})

	t.Run("invalid int defaults to default", func(t *testing.T) {
		os.Setenv("TLS_STREAMING_PORT", "not-a-number")
		defer os.Unsetenv("TLS_STREAMING_PORT")

		cfg := LoadFromEnv()
		if cfg.TLSStreamingPort != 8089 {
			t.Errorf("expected default 8089 for invalid int, got %d", cfg.TLSStreamingPort)
		}
	})
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		def      bool
		expected bool
	}{
		{"true", "true", false, true},
		{"TRUE", "TRUE", false, true},
		{"True", "True", false, true},
		{"1", "1", false, true},
		{"false", "false", true, false},
		{"0", "0", true, false},
		{"random string", "maybe", true, false},
		{"empty uses default true", "", true, true},
		{"empty uses default false", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_BOOL_" + tt.name
			if tt.envValue != "" {
				os.Setenv(key, tt.envValue)
				defer os.Unsetenv(key)
			}
			got := getEnvBool(key, tt.def)
			if got != tt.expected {
				t.Errorf("getEnvBool(%q, %v) = %v, want %v", tt.envValue, tt.def, got, tt.expected)
			}
		})
	}
}

func TestGetEnvSlice(t *testing.T) {
	t.Run("comma separated", func(t *testing.T) {
		os.Setenv("TEST_SLICE", "a,b,c")
		defer os.Unsetenv("TEST_SLICE")

		got := getEnvSlice("TEST_SLICE", nil)
		if len(got) != 3 {
			t.Errorf("expected 3 elements, got %d: %v", len(got), got)
		}
	})

	t.Run("with spaces", func(t *testing.T) {
		os.Setenv("TEST_SLICE2", "  a , b , c  ")
		defer os.Unsetenv("TEST_SLICE2")

		got := getEnvSlice("TEST_SLICE2", nil)
		if len(got) != 3 {
			t.Errorf("expected 3 elements, got %d: %v", len(got), got)
		}
		if got[0] != "a" || got[1] != "b" || got[2] != "c" {
			t.Errorf("expected trimmed values, got %v", got)
		}
	})

	t.Run("empty entries filtered", func(t *testing.T) {
		os.Setenv("TEST_SLICE3", "a,,b,  ,c")
		defer os.Unsetenv("TEST_SLICE3")

		got := getEnvSlice("TEST_SLICE3", nil)
		if len(got) != 3 {
			t.Errorf("expected 3 elements (empty filtered), got %d: %v", len(got), got)
		}
	})

	t.Run("unset returns default", func(t *testing.T) {
		def := []string{"default"}
		got := getEnvSlice("TEST_SLICE_UNSET", def)
		if len(got) != 1 || got[0] != "default" {
			t.Errorf("expected default value, got %v", got)
		}
	})

	t.Run("empty value returns default", func(t *testing.T) {
		os.Setenv("TEST_SLICE_EMPTY", "")
		defer os.Unsetenv("TEST_SLICE_EMPTY")

		def := []string{"fallback"}
		got := getEnvSlice("TEST_SLICE_EMPTY", def)
		if len(got) != 1 || got[0] != "fallback" {
			t.Errorf("expected default for empty value, got %v", got)
		}
	})
}
