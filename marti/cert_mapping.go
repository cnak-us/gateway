package marti

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

const certMappingBucket = "TAK_CERT_MAPPING"

// certMapping associates a certificate CN with its enrollment identity.
type certMapping struct {
	Username  string `json:"username"`
	ClientUID string `json:"clientUID"`
}

// CertMappingStore maps TLS certificate Common Names to the username and
// client UID provided during enrollment. Backed by a NATS KV bucket with
// an in-memory fallback when JetStream is unavailable.
type CertMappingStore struct {
	kv     nats.KeyValue
	mu     sync.RWMutex
	local  map[string]certMapping
	logger *slog.Logger
}

// NewCertMappingStore creates a cert-mapping store backed by NATS KV.
// Falls back to in-memory-only mode when JetStream is not available.
func NewCertMappingStore(nc *nats.Conn, logger *slog.Logger) (*CertMappingStore, error) {
	s := &CertMappingStore{
		local:  make(map[string]certMapping),
		logger: logger,
	}

	js, err := nc.JetStream()
	if err != nil {
		logger.Warn("JetStream not available, using in-memory cert mapping store", "error", err)
		return s, nil
	}

	kv, err := js.KeyValue(certMappingBucket)
	if err != nil {
		kv, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket:      certMappingBucket,
			Description: "Certificate CN to username/UID mapping",
			TTL:         365 * 24 * time.Hour, // keep mappings for the cert lifetime
			Storage:     nats.FileStorage,
		})
		if err != nil {
			logger.Warn("JetStream KV not available, using in-memory cert mapping store", "error", err)
			return s, nil
		}
		logger.Info("created NATS KV bucket", "bucket", certMappingBucket)
	}

	s.kv = kv
	return s, nil
}

// Store saves a certificate-CN-to-username mapping.
func (s *CertMappingStore) Store(certCN, username, clientUID string) error {
	m := certMapping{Username: username, ClientUID: clientUID}

	s.mu.Lock()
	s.local[certCN] = m
	s.mu.Unlock()

	if s.kv != nil {
		data, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("marshaling cert mapping: %w", err)
		}
		if _, err := s.kv.Put(certCN, data); err != nil {
			s.logger.Warn("failed to store cert mapping in KV", "certCN", certCN, "error", err)
		}
	}

	s.logger.Info("cert mapping stored", "certCN", certCN, "username", username, "clientUID", clientUID)
	return nil
}

// LookupUsername returns the username associated with a certificate CN.
func (s *CertMappingStore) LookupUsername(certCN string) (string, error) {
	// Try KV first
	if s.kv != nil {
		entry, err := s.kv.Get(certCN)
		if err == nil {
			var m certMapping
			if err := json.Unmarshal(entry.Value(), &m); err != nil {
				return "", fmt.Errorf("unmarshaling cert mapping for %s: %w", certCN, err)
			}
			return m.Username, nil
		}
	}

	// Fall back to local map
	s.mu.RLock()
	m, ok := s.local[certCN]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("no mapping for certCN %s", certCN)
	}
	return m.Username, nil
}
