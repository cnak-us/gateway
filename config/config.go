// Package config provides configuration management for the TAK Gateway service
package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration for the TAK Gateway service
type Config struct {
	// TLS streaming port (TAK clients connect here with TLS client certs)
	TLSStreamingPort int

	// HTTPS API port (Marti API for enrollment/config)
	HTTPSAPIPort int

	// TCP streaming port (unencrypted, for testing only)
	TCPStreamingPort int

	// Whether TCP streaming is enabled
	TCPEnabled bool

	// NATS settings
	NATSURL       string
	NATSCredsFile string
	NATSAuthToken string

	// CA settings for client certificate issuance
	CACertFile     string
	CAKeyFile      string
	CAOrganization string
	CACertExpiry   int // days

	// Logging
	LogLevel  string
	LogFormat string

	// Management API port (health, metrics)
	ManagementPort int

	// Additional Subject Alternative Names for server certificates.
	// Comma-separated list of hostnames and/or IPs that TAK clients connect to.
	ServerSANs []string

	// Backend URL for direct HTTP communication (fallback when NATS is unavailable)
	BackendURL string

	// Service token for authenticating with the backend API
	ServiceToken string

	// Pod name for Kubernetes (injected via downward API)
	PodName string

	// TLS settings for TAK Server interoperability.

	// KeyType controls the server certificate key algorithm: "rsa" (default) or "ecdsa".
	// ECDSA P-256 keys enable ECDHE_ECDSA cipher suites required by TAK Server (Marti).
	KeyType string

	// TLSMinVersion sets the minimum TLS version: "1.0", "1.1", "1.2" (default), "1.3".
	// Note: FIPS mode enforces TLS 1.2 minimum regardless of this setting.
	TLSMinVersion string

	// TLSCipherSuites is an optional explicit list of TLS 1.2 cipher suite names.
	// When empty, Go's default (or FIPS-restricted) suite list is used.
	// Example: "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"
	TLSCipherSuites []string

	// Store-and-forward: replay missed JetStream messages on client reconnect.
	StoreForwardEnabled bool
	StoreForwardWindow  string // duration string, e.g. "5m"
}

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() *Config {
	return &Config{
		TLSStreamingPort: getEnvInt("TLS_STREAMING_PORT", 8089),
		HTTPSAPIPort:     getEnvInt("HTTPS_API_PORT", 8446),
		TCPStreamingPort: getEnvInt("TCP_STREAMING_PORT", 8088),
		TCPEnabled:       getEnvBool("TCP_ENABLED", false),
		NATSURL:          getEnv("NATS_URL", "nats://nats-server:4222"),
		NATSCredsFile:    getEnv("NATS_CREDENTIALS_FILE", ""),
		NATSAuthToken:    getEnv("NATS_AUTH_TOKEN", ""),
		CACertFile:       getEnv("CA_CERT_FILE", "/app/data/ca.crt"),
		CAKeyFile:        getEnv("CA_KEY_FILE", "/app/data/ca.key"),
		CAOrganization:   getEnv("CA_ORGANIZATION", "CNAK TAK Gateway"),
		CACertExpiry:     getEnvInt("CA_CERT_EXPIRY_DAYS", 365),
		LogLevel:         getEnv("LOG_LEVEL", "info"),
		LogFormat:        getEnv("LOG_FORMAT", "json"),
		ManagementPort:   getEnvInt("MANAGEMENT_PORT", 8090),
		ServerSANs:      getEnvSlice("SERVER_SANS", nil),
		BackendURL:      getEnv("BACKEND_URL", ""),
		ServiceToken:    getEnv("SERVICE_TOKEN", ""),
		PodName:         getEnv("POD_NAME", ""),
		KeyType:         getEnv("KEY_TYPE", "ecdsa"),
		TLSMinVersion:   getEnv("TLS_MIN_VERSION", "1.2"),
		TLSCipherSuites:     getEnvSlice("TLS_CIPHER_SUITES", nil),
		StoreForwardEnabled: getEnvBool("STORE_FORWARD_ENABLED", true),
		StoreForwardWindow:  getEnv("STORE_FORWARD_WINDOW", "5m"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return strings.ToLower(value) == "true" || value == "1"
	}
	return defaultValue
}

func getEnvSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		parts := strings.Split(value, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return defaultValue
}
