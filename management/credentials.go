package management

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"golang.org/x/crypto/bcrypt"
)

const credentialBucket = "TAK_ENROLLMENT"

// CredentialInfo is the public view of a stored enrollment credential.
type CredentialInfo struct {
	Username   string    `json:"username"`
	CommonName string    `json:"common_name,omitempty"`
	Issued     time.Time `json:"issued,omitempty"`
	Expires    time.Time `json:"expires,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// storedCredential is the internal representation stored in NATS KV.
type storedCredential struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"passwordHash"`
	CommonName   string    `json:"commonName,omitempty"`
	Issued       time.Time `json:"issued,omitempty"`
	Expires      time.Time `json:"expires,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// CredentialStore manages enrollment credentials in a NATS KV bucket.
// Falls back to in-memory storage when JetStream is not available.
type CredentialStore struct {
	kv     nats.KeyValue // nil when JetStream unavailable
	mu     sync.RWMutex
	mem    map[string]*storedCredential // in-memory fallback
	logger *slog.Logger
}

// NewCredentialStore creates a new credential store backed by NATS KV.
// If JetStream is not available, falls back to in-memory storage.
func NewCredentialStore(nc *nats.Conn, logger *slog.Logger) (*CredentialStore, error) {
	cs := &CredentialStore{
		mem:    make(map[string]*storedCredential),
		logger: logger,
	}

	js, err := nc.JetStream()
	if err != nil {
		logger.Warn("JetStream not available, using in-memory credential store", "error", err)
		return cs, nil
	}

	kv, err := js.KeyValue(credentialBucket)
	if err != nil {
		kv, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket:      credentialBucket,
			Description: "TAK enrollment credentials",
			Storage:     nats.FileStorage,
		})
		if err != nil {
			logger.Warn("JetStream KV not available, using in-memory credential store", "error", err)
			return cs, nil
		}
		logger.Info("created NATS KV bucket", "bucket", credentialBucket)
	}

	cs.kv = kv
	return cs, nil
}

// CreateCredential stores a new enrollment credential with a bcrypt-hashed password.
func (cs *CredentialStore) CreateCredential(username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	cred := &storedCredential{
		Username:     username,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	}

	if cs.kv != nil {
		data, err := json.Marshal(cred)
		if err != nil {
			return fmt.Errorf("marshaling credential: %w", err)
		}
		if _, err := cs.kv.Put(username, data); err != nil {
			return fmt.Errorf("storing credential for %s: %w", username, err)
		}
	} else {
		cs.mu.Lock()
		cs.mem[username] = cred
		cs.mu.Unlock()
	}

	cs.logger.Info("enrollment credential created", "username", username)
	return nil
}

// UpdateCertInfo updates the certificate metadata for an existing credential
// after a client has enrolled and received a signed certificate.
func (cs *CredentialStore) UpdateCertInfo(username, commonName string, issued, expires time.Time) error {
	if cs.kv != nil {
		entry, err := cs.kv.Get(username)
		if err != nil {
			return fmt.Errorf("credential %s not found: %w", username, err)
		}
		var cred storedCredential
		if err := json.Unmarshal(entry.Value(), &cred); err != nil {
			return fmt.Errorf("unmarshaling credential: %w", err)
		}
		cred.CommonName = commonName
		cred.Issued = issued
		cred.Expires = expires
		data, err := json.Marshal(cred)
		if err != nil {
			return fmt.Errorf("marshaling credential: %w", err)
		}
		if _, err := cs.kv.Put(username, data); err != nil {
			return fmt.Errorf("updating credential %s: %w", username, err)
		}
	} else {
		cs.mu.Lock()
		if cred, ok := cs.mem[username]; ok {
			cred.CommonName = commonName
			cred.Issued = issued
			cred.Expires = expires
		}
		cs.mu.Unlock()
	}
	cs.logger.Info("credential cert info updated", "username", username, "cn", commonName)
	return nil
}

// ValidateCredential checks a username/password pair against stored credentials.
func (cs *CredentialStore) ValidateCredential(username, password string) bool {
	if cs.kv != nil {
		entry, err := cs.kv.Get(username)
		if err != nil {
			return false
		}
		var cred storedCredential
		if err := json.Unmarshal(entry.Value(), &cred); err != nil {
			return false
		}
		return bcrypt.CompareHashAndPassword([]byte(cred.PasswordHash), []byte(password)) == nil
	}

	// In-memory fallback
	cs.mu.RLock()
	cred, ok := cs.mem[username]
	cs.mu.RUnlock()
	if !ok {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(cred.PasswordHash), []byte(password)) == nil
}

// ListCredentials returns all stored enrollment credentials (without passwords).
func (cs *CredentialStore) ListCredentials() ([]CredentialInfo, error) {
	if cs.kv != nil {
		keys, err := cs.kv.Keys()
		if err != nil {
			if err == nats.ErrNoKeysFound {
				return nil, nil
			}
			return nil, fmt.Errorf("listing credential keys: %w", err)
		}

		creds := make([]CredentialInfo, 0, len(keys))
		for _, key := range keys {
			entry, err := cs.kv.Get(key)
			if err != nil {
				cs.logger.Warn("failed to get credential", "key", key, "error", err)
				continue
			}
			var stored storedCredential
			if err := json.Unmarshal(entry.Value(), &stored); err != nil {
				cs.logger.Warn("failed to unmarshal credential", "key", key, "error", err)
				continue
			}
			creds = append(creds, credentialToInfo(stored))
		}
		return creds, nil
	}

	// In-memory fallback
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	creds := make([]CredentialInfo, 0, len(cs.mem))
	for _, stored := range cs.mem {
		creds = append(creds, credentialToInfo(*stored))
	}
	return creds, nil
}

// DeleteCredential removes an enrollment credential.
func (cs *CredentialStore) DeleteCredential(username string) error {
	if cs.kv != nil {
		if err := cs.kv.Delete(username); err != nil {
			if err == nats.ErrKeyNotFound {
				return fmt.Errorf("credential %s not found", username)
			}
			return fmt.Errorf("deleting credential %s: %w", username, err)
		}
	} else {
		cs.mu.Lock()
		if _, ok := cs.mem[username]; !ok {
			cs.mu.Unlock()
			return fmt.Errorf("credential %s not found", username)
		}
		delete(cs.mem, username)
		cs.mu.Unlock()
	}

	cs.logger.Info("enrollment credential deleted", "username", username)
	return nil
}

func credentialToInfo(s storedCredential) CredentialInfo {
	return CredentialInfo{
		Username:   s.Username,
		CommonName: s.CommonName,
		Issued:     s.Issued,
		Expires:    s.Expires,
		CreatedAt:  s.CreatedAt,
	}
}
