package marti

import (
	"encoding/base64"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// CredentialValidator validates enrollment credentials.
// The management.CredentialStore satisfies this interface.
type CredentialValidator interface {
	ValidateCredential(username, password string) bool
}

// CertInfoUpdater updates certificate metadata on an enrollment credential
// after a client has enrolled. Optional — if the credential store implements
// this interface, enrollment handlers will call it to populate CN/issued/expires.
type CertInfoUpdater interface {
	UpdateCertInfo(username, commonName string, issued, expires time.Time) error
}

// InMemoryCredentialStore holds enrollment credentials in memory.
type InMemoryCredentialStore struct {
	creds map[string]string // username -> bcrypt hash
}

// NewInMemoryCredentialStore creates an empty in-memory credential store.
func NewInMemoryCredentialStore() *InMemoryCredentialStore {
	return &InMemoryCredentialStore{
		creds: make(map[string]string),
	}
}

// Add stores a new enrollment credential with bcrypt hashing.
func (cs *InMemoryCredentialStore) Add(username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	cs.creds[username] = string(hash)
	return nil
}

// ValidateCredential checks a username/password pair.
func (cs *InMemoryCredentialStore) ValidateCredential(username, password string) bool {
	hash, ok := cs.creds[username]
	if !ok {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// validateBasicAuth parses the Authorization header and validates the credentials.
func validateBasicAuth(authHeader string, validator CredentialValidator) (string, bool) {
	if authHeader == "" {
		return "", false
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Basic") {
		return "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}

	userPass := strings.SplitN(string(decoded), ":", 2)
	if len(userPass) != 2 {
		return "", false
	}

	username := userPass[0]
	password := userPass[1]

	if validator.ValidateCredential(username, password) {
		return username, true
	}

	return "", false
}
