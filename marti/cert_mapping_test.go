package marti

import (
	"log/slog"
	"testing"
)

func TestCertMappingStoreLocal(t *testing.T) {
	// Create a store with no KV (nil nc would panic, so test local-only mode directly)
	store := &CertMappingStore{
		local:  make(map[string]certMapping),
		logger: slog.Default(),
	}

	t.Run("store and lookup", func(t *testing.T) {
		err := store.Store("cert-cn-1", "admin", "client-uid-1")
		if err != nil {
			t.Fatalf("Store failed: %v", err)
		}

		username, err := store.LookupUsername("cert-cn-1")
		if err != nil {
			t.Fatalf("LookupUsername failed: %v", err)
		}
		if username != "admin" {
			t.Errorf("expected username 'admin', got %q", username)
		}
	})

	t.Run("lookup not found", func(t *testing.T) {
		_, err := store.LookupUsername("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent mapping")
		}
	})

	t.Run("overwrite mapping", func(t *testing.T) {
		store.Store("cert-cn-2", "user1", "uid-1")
		store.Store("cert-cn-2", "user2", "uid-2")

		username, err := store.LookupUsername("cert-cn-2")
		if err != nil {
			t.Fatalf("LookupUsername failed: %v", err)
		}
		if username != "user2" {
			t.Errorf("expected 'user2' after overwrite, got %q", username)
		}
	})

	t.Run("multiple mappings", func(t *testing.T) {
		store.Store("cn-a", "alice", "uid-a")
		store.Store("cn-b", "bob", "uid-b")

		uA, _ := store.LookupUsername("cn-a")
		uB, _ := store.LookupUsername("cn-b")

		if uA != "alice" {
			t.Errorf("expected 'alice', got %q", uA)
		}
		if uB != "bob" {
			t.Errorf("expected 'bob', got %q", uB)
		}
	})

	t.Run("empty cert CN", func(t *testing.T) {
		err := store.Store("", "user", "uid")
		if err != nil {
			t.Fatalf("Store with empty CN failed: %v", err)
		}

		username, err := store.LookupUsername("")
		if err != nil {
			t.Fatalf("LookupUsername with empty CN failed: %v", err)
		}
		if username != "user" {
			t.Errorf("expected 'user', got %q", username)
		}
	})
}
