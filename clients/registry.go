package clients

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	kvBucket           = "TAK_CLIENTS"
	kvDisconnectBucket = "TAK_DISCONNECTS"
)

// ClientInfo describes a connected TAK client.
type ClientInfo struct {
	UID             string    `json:"uid"`
	Callsign        string    `json:"callsign"`
	IP              string    `json:"ip"`
	CertCN          string    `json:"certCN"`
	PodName         string    `json:"podName"`
	ConnectedAt     time.Time `json:"connectedAt"`
	LastSeen        time.Time `json:"lastSeen"`
	ProtocolVersion string    `json:"protocolVersion"`
	Groups          []string  `json:"groups,omitempty"`
}

// Registry tracks connected TAK clients using a NATS KV bucket for cross-pod visibility.
// Falls back to in-memory-only mode when JetStream is not available.
type Registry struct {
	kv          nats.KeyValue // nil when JetStream unavailable
	disconnects nats.KeyValue // tracks last disconnect time per UID
	podName     string
	mu          sync.RWMutex
	local       map[string]*ClientInfo // local clients on this pod
	logger      *slog.Logger
}

// NewRegistry creates a new client registry backed by NATS KV.
// If JetStream is not available, falls back to in-memory-only mode.
func NewRegistry(nc *nats.Conn, podName string, logger *slog.Logger) (*Registry, error) {
	r := &Registry{
		podName: podName,
		local:   make(map[string]*ClientInfo),
		logger:  logger,
	}

	js, err := nc.JetStream()
	if err != nil {
		logger.Warn("JetStream not available, using in-memory client registry", "error", err)
		return r, nil
	}

	kv, err := js.KeyValue(kvBucket)
	if err != nil {
		kv, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket:      kvBucket,
			Description: "Connected TAK client registry",
			TTL:         5 * time.Minute, // auto-expire stale entries
			Storage:     nats.MemoryStorage,
		})
		if err != nil {
			logger.Warn("JetStream KV not available, using in-memory client registry", "error", err)
			return r, nil
		}
		logger.Info("created NATS KV bucket", "bucket", kvBucket)
	}

	r.kv = kv

	// Create a separate bucket for disconnect timestamps (longer TTL for replay window)
	dkv, err := js.KeyValue(kvDisconnectBucket)
	if err != nil {
		dkv, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket:      kvDisconnectBucket,
			Description: "TAK client last disconnect timestamps",
			TTL:         30 * time.Minute, // keep disconnect times for replay window
			Storage:     nats.MemoryStorage,
		})
		if err != nil {
			logger.Warn("failed to create disconnect KV bucket", "error", err)
		} else {
			logger.Info("created NATS KV bucket", "bucket", kvDisconnectBucket)
		}
	}
	r.disconnects = dkv

	return r, nil
}

// Register adds a client to the registry.
func (r *Registry) Register(info *ClientInfo) error {
	info.PodName = r.podName

	r.mu.Lock()
	r.local[info.UID] = info
	r.mu.Unlock()

	if r.kv != nil {
		data, err := json.Marshal(info)
		if err != nil {
			return fmt.Errorf("marshaling client info: %w", err)
		}
		if _, err := r.kv.Put(info.UID, data); err != nil {
			r.logger.Warn("failed to store client in KV", "uid", info.UID, "error", err)
		}
	}

	r.logger.Info("client registered", "uid", info.UID, "callsign", info.Callsign, "ip", info.IP)
	return nil
}

// Deregister removes a client from the registry and records the disconnect time.
func (r *Registry) Deregister(uid string) error {
	r.mu.Lock()
	delete(r.local, uid)
	r.mu.Unlock()

	if r.kv != nil {
		if err := r.kv.Delete(uid); err != nil && err != nats.ErrKeyNotFound {
			return fmt.Errorf("deleting client %s from KV: %w", uid, err)
		}
	}

	// Record disconnect time for store-and-forward replay
	if r.disconnects != nil {
		ts := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := r.disconnects.Put(uid, []byte(ts)); err != nil {
			r.logger.Warn("failed to record disconnect time", "uid", uid, "error", err)
		}
	}

	r.logger.Info("client deregistered", "uid", uid)
	return nil
}

// GetDisconnectTime returns the last disconnect time for a UID, if known.
// Returns zero time and false if the UID has no recorded disconnect.
func (r *Registry) GetDisconnectTime(uid string) (time.Time, bool) {
	if r.disconnects == nil {
		return time.Time{}, false
	}
	entry, err := r.disconnects.Get(uid)
	if err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, string(entry.Value()))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// ClearDisconnectTime removes the disconnect record for a UID after successful replay.
func (r *Registry) ClearDisconnectTime(uid string) {
	if r.disconnects == nil {
		return
	}
	_ = r.disconnects.Delete(uid)
}

// UpdateLastSeen updates the last seen timestamp for a client.
func (r *Registry) UpdateLastSeen(uid string) error {
	r.mu.Lock()
	info, ok := r.local[uid]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("client %s not found locally", uid)
	}
	info.LastSeen = time.Now()
	r.mu.Unlock()

	if r.kv != nil {
		data, err := json.Marshal(info)
		if err != nil {
			return fmt.Errorf("marshaling client info: %w", err)
		}
		if _, err := r.kv.Put(uid, data); err != nil {
			r.logger.Warn("failed to update client in KV", "uid", uid, "error", err)
		}
	}

	return nil
}

// GetClient retrieves a client by UID. Uses KV store if available, falls back to local.
func (r *Registry) GetClient(uid string) (*ClientInfo, error) {
	if r.kv != nil {
		entry, err := r.kv.Get(uid)
		if err == nil {
			var info ClientInfo
			if err := json.Unmarshal(entry.Value(), &info); err != nil {
				return nil, fmt.Errorf("unmarshaling client %s: %w", uid, err)
			}
			return &info, nil
		}
	}

	// Fall back to local map
	r.mu.RLock()
	info, ok := r.local[uid]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("client %s not found", uid)
	}
	return info, nil
}

// ListAll returns all clients. Uses KV store across all pods if available, falls back to local.
func (r *Registry) ListAll() ([]*ClientInfo, error) {
	if r.kv != nil {
		keys, err := r.kv.Keys()
		if err != nil {
			if err == nats.ErrNoKeysFound {
				return nil, nil
			}
			// KV error — fall through to local
			r.logger.Warn("failed to list KV keys, falling back to local", "error", err)
		} else {
			clients := make([]*ClientInfo, 0, len(keys))
			for _, key := range keys {
				entry, err := r.kv.Get(key)
				if err != nil {
					r.logger.Warn("failed to get client from KV", "key", key, "error", err)
					continue
				}
				var info ClientInfo
				if err := json.Unmarshal(entry.Value(), &info); err != nil {
					r.logger.Warn("failed to unmarshal client", "key", key, "error", err)
					continue
				}
				clients = append(clients, &info)
			}
			return clients, nil
		}
	}

	// In-memory fallback
	return r.ListLocal(), nil
}

// ListLocal returns only the clients connected to this pod.
func (r *Registry) ListLocal() []*ClientInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clients := make([]*ClientInfo, 0, len(r.local))
	for _, info := range r.local {
		clients = append(clients, info)
	}
	return clients
}

// Count returns the number of local clients on this pod.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.local)
}
