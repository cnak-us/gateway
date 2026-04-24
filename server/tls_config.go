package server

import (
	"crypto/tls"
	"fmt"
	"log/slog"

	"github.com/cnak-us/gateway/config"
)

// ParseTLSMinVersion converts a version string ("1.0", "1.1", "1.2", "1.3")
// to the corresponding crypto/tls constant. Defaults to TLS 1.2.
func ParseTLSMinVersion(version string) uint16 {
	switch version {
	case "1.0":
		return tls.VersionTLS10
	case "1.1":
		return tls.VersionTLS11
	case "1.3":
		return tls.VersionTLS13
	default:
		return tls.VersionTLS12
	}
}

// TLSVersionName returns the human-readable name for a TLS version constant.
func TLSVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "1.0"
	case tls.VersionTLS11:
		return "1.1"
	case tls.VersionTLS12:
		return "1.2"
	case tls.VersionTLS13:
		return "1.3"
	default:
		return "1.2"
	}
}

// cipherSuiteByName returns the TLS cipher suite ID for the given IANA name.
// Returns 0 if not found.
func cipherSuiteByName(name string) uint16 {
	for _, s := range tls.CipherSuites() {
		if s.Name == name {
			return s.ID
		}
	}
	// Also check insecure suites for compatibility
	for _, s := range tls.InsecureCipherSuites() {
		if s.Name == name {
			return s.ID
		}
	}
	return 0
}

// ParseCipherSuites converts a list of cipher suite names to their numeric IDs.
// Unknown names are logged and skipped.
func ParseCipherSuites(names []string) []uint16 {
	if len(names) == 0 {
		return nil
	}
	var ids []uint16
	for _, name := range names {
		id := cipherSuiteByName(name)
		if id == 0 {
			slog.Warn("unknown TLS cipher suite, skipping", "name", name)
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// CipherSuiteNames returns the IANA names for a list of cipher suite IDs.
func CipherSuiteNames(ids []uint16) []string {
	lookup := make(map[uint16]string)
	for _, s := range tls.CipherSuites() {
		lookup[s.ID] = s.Name
	}
	for _, s := range tls.InsecureCipherSuites() {
		lookup[s.ID] = s.Name
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := lookup[id]; ok {
			names = append(names, name)
		} else {
			names = append(names, fmt.Sprintf("0x%04x", id))
		}
	}
	return names
}

// ApplyTLSConfig applies gateway config TLS settings (min version, cipher suites)
// to an existing tls.Config.
func ApplyTLSConfig(tlsCfg *tls.Config, cfg *config.Config) {
	tlsCfg.MinVersion = ParseTLSMinVersion(cfg.TLSMinVersion)

	if suites := ParseCipherSuites(cfg.TLSCipherSuites); len(suites) > 0 {
		tlsCfg.CipherSuites = suites
	}

	slog.Info("TLS config applied",
		"min_version", TLSVersionName(tlsCfg.MinVersion),
		"cipher_suites_override", len(cfg.TLSCipherSuites) > 0,
		"key_type", cfg.KeyType,
	)
}

// SupportedCipherSuites returns all cipher suites that Go supports, for UI display.
func SupportedCipherSuites() []string {
	var names []string
	for _, s := range tls.CipherSuites() {
		names = append(names, s.Name)
	}
	return names
}
