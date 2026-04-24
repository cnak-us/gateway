package marti

import (
	"encoding/base64"
	"testing"
)

func TestInMemoryCredentialStore(t *testing.T) {
	t.Run("add and validate", func(t *testing.T) {
		store := NewInMemoryCredentialStore()
		if err := store.Add("alice", "password123"); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
		if !store.ValidateCredential("alice", "password123") {
			t.Error("valid credential should pass validation")
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		store := NewInMemoryCredentialStore()
		store.Add("alice", "correct")
		if store.ValidateCredential("alice", "wrong") {
			t.Error("wrong password should fail validation")
		}
	})

	t.Run("unknown user", func(t *testing.T) {
		store := NewInMemoryCredentialStore()
		if store.ValidateCredential("nobody", "password") {
			t.Error("unknown user should fail validation")
		}
	})

	t.Run("empty password", func(t *testing.T) {
		store := NewInMemoryCredentialStore()
		store.Add("user", "")
		if !store.ValidateCredential("user", "") {
			t.Error("empty password should validate if stored with empty password")
		}
		if store.ValidateCredential("user", "notempty") {
			t.Error("non-empty password should fail for user with empty password")
		}
	})

	t.Run("multiple users", func(t *testing.T) {
		store := NewInMemoryCredentialStore()
		store.Add("alice", "pass1")
		store.Add("bob", "pass2")
		if !store.ValidateCredential("alice", "pass1") {
			t.Error("alice should validate")
		}
		if !store.ValidateCredential("bob", "pass2") {
			t.Error("bob should validate")
		}
		if store.ValidateCredential("alice", "pass2") {
			t.Error("alice should not validate with bob's password")
		}
	})

	t.Run("overwrite user", func(t *testing.T) {
		store := NewInMemoryCredentialStore()
		store.Add("alice", "old")
		store.Add("alice", "new")
		if store.ValidateCredential("alice", "old") {
			t.Error("old password should fail after overwrite")
		}
		if !store.ValidateCredential("alice", "new") {
			t.Error("new password should validate after overwrite")
		}
	})

	t.Run("special characters in password", func(t *testing.T) {
		store := NewInMemoryCredentialStore()
		pw := `p@$$w0rd!#%^&*()_+{}|:"<>?`
		store.Add("user", pw)
		if !store.ValidateCredential("user", pw) {
			t.Error("special characters password should validate")
		}
	})
}

func TestValidateBasicAuth(t *testing.T) {
	store := NewInMemoryCredentialStore()
	store.Add("admin", "secret")

	encodeBasic := func(user, pass string) string {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	}

	t.Run("valid auth", func(t *testing.T) {
		username, ok := validateBasicAuth(encodeBasic("admin", "secret"), store)
		if !ok {
			t.Error("valid auth should succeed")
		}
		if username != "admin" {
			t.Errorf("expected username 'admin', got %q", username)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		_, ok := validateBasicAuth(encodeBasic("admin", "wrong"), store)
		if ok {
			t.Error("wrong password should fail")
		}
	})

	t.Run("unknown user", func(t *testing.T) {
		_, ok := validateBasicAuth(encodeBasic("nobody", "secret"), store)
		if ok {
			t.Error("unknown user should fail")
		}
	})

	t.Run("empty header", func(t *testing.T) {
		_, ok := validateBasicAuth("", store)
		if ok {
			t.Error("empty header should fail")
		}
	})

	t.Run("non-basic auth", func(t *testing.T) {
		_, ok := validateBasicAuth("Bearer token123", store)
		if ok {
			t.Error("Bearer auth should fail")
		}
	})

	t.Run("invalid base64", func(t *testing.T) {
		_, ok := validateBasicAuth("Basic !!!invalid!!!", store)
		if ok {
			t.Error("invalid base64 should fail")
		}
	})

	t.Run("no colon in decoded", func(t *testing.T) {
		noColon := "Basic " + base64.StdEncoding.EncodeToString([]byte("nocolon"))
		_, ok := validateBasicAuth(noColon, store)
		if ok {
			t.Error("no colon in decoded value should fail")
		}
	})

	t.Run("case insensitive Basic prefix", func(t *testing.T) {
		header := "basic " + base64.StdEncoding.EncodeToString([]byte("admin:secret"))
		username, ok := validateBasicAuth(header, store)
		if !ok {
			t.Error("lowercase 'basic' should work")
		}
		if username != "admin" {
			t.Errorf("expected 'admin', got %q", username)
		}
	})

	t.Run("password with colon", func(t *testing.T) {
		store2 := NewInMemoryCredentialStore()
		store2.Add("user", "pass:with:colons")
		header := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass:with:colons"))
		username, ok := validateBasicAuth(header, store2)
		if !ok {
			t.Error("password with colons should work (SplitN with 2)")
		}
		if username != "user" {
			t.Errorf("expected 'user', got %q", username)
		}
	})

	t.Run("only scheme no value", func(t *testing.T) {
		_, ok := validateBasicAuth("Basic", store)
		if ok {
			t.Error("scheme only should fail")
		}
	})
}
