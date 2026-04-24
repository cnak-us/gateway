package ca

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestSavePEM(t *testing.T) {
	t.Run("basic save", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.pem")
		data := []byte("test PEM data")
		if err := SavePEM(data, path); err != nil {
			t.Fatalf("SavePEM failed: %v", err)
		}
		read, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}
		if string(read) != string(data) {
			t.Errorf("data mismatch: got %q, want %q", read, data)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
		}
	})

	t.Run("creates intermediate directories", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "sub", "dir", "test.pem")
		if err := SavePEM([]byte("data"), path); err != nil {
			t.Fatalf("SavePEM with nested dirs failed: %v", err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("file not created in nested directory")
		}
	})

	t.Run("overwrite existing", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.pem")
		SavePEM([]byte("first"), path)
		SavePEM([]byte("second"), path)
		read, _ := os.ReadFile(path)
		if string(read) != "second" {
			t.Errorf("expected 'second', got %q", read)
		}
	})
}

func TestLoadPEM(t *testing.T) {
	t.Run("valid file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.pem")
		expected := []byte("test data")
		os.WriteFile(path, expected, 0600)
		data, err := LoadPEM(path)
		if err != nil {
			t.Fatalf("LoadPEM failed: %v", err)
		}
		if string(data) != string(expected) {
			t.Errorf("data mismatch")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := LoadPEM("/nonexistent/path/file.pem")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})
}

func TestGenerateTrustStoreP12(t *testing.T) {
	authority := NewCA()
	authority.GenerateCA("TestOrg", "TestOU")

	t.Run("valid truststore", func(t *testing.T) {
		p12, err := GenerateTrustStoreP12(authority.CACertPEM(), "atakatak")
		if err != nil {
			t.Fatalf("GenerateTrustStoreP12 failed: %v", err)
		}
		if len(p12) == 0 {
			t.Fatal("empty P12 data")
		}
	})

	t.Run("invalid PEM", func(t *testing.T) {
		_, err := GenerateTrustStoreP12([]byte("not PEM"), "atakatak")
		if err == nil {
			t.Fatal("expected error for invalid PEM")
		}
	})

	t.Run("corrupt cert data", func(t *testing.T) {
		badPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("corrupt")})
		_, err := GenerateTrustStoreP12(badPEM, "password")
		if err == nil {
			t.Fatal("expected error for corrupt cert")
		}
	})
}

func TestGenerateClientP12(t *testing.T) {
	authority := NewCA()
	authority.GenerateCA("TestOrg", "TestOU")
	certPEM, keyPEM, _ := authority.GenerateClientCert("test-client", 0)

	t.Run("valid client P12", func(t *testing.T) {
		p12, err := GenerateClientP12(certPEM, keyPEM, authority.CACertPEM(), "atakatak")
		if err != nil {
			t.Fatalf("GenerateClientP12 failed: %v", err)
		}
		if len(p12) == 0 {
			t.Fatal("empty P12 data")
		}
	})

	t.Run("invalid cert PEM", func(t *testing.T) {
		_, err := GenerateClientP12([]byte("bad cert"), keyPEM, authority.CACertPEM(), "pw")
		if err == nil {
			t.Fatal("expected error for invalid cert PEM")
		}
	})

	t.Run("invalid key PEM", func(t *testing.T) {
		_, err := GenerateClientP12(certPEM, []byte("bad key"), authority.CACertPEM(), "pw")
		if err == nil {
			t.Fatal("expected error for invalid key PEM")
		}
	})

	t.Run("invalid CA cert PEM", func(t *testing.T) {
		_, err := GenerateClientP12(certPEM, keyPEM, []byte("bad ca"), "pw")
		if err == nil {
			t.Fatal("expected error for invalid CA PEM")
		}
	})

	t.Run("corrupt cert data in PEM", func(t *testing.T) {
		badCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("corrupt")})
		_, err := GenerateClientP12(badCertPEM, keyPEM, authority.CACertPEM(), "pw")
		if err == nil {
			t.Fatal("expected error for corrupt cert data")
		}
	})

	t.Run("corrupt key data in PEM", func(t *testing.T) {
		badKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("corrupt")})
		_, err := GenerateClientP12(certPEM, badKeyPEM, authority.CACertPEM(), "pw")
		if err == nil {
			t.Fatal("expected error for corrupt key data")
		}
	})

	t.Run("mismatched key no panic", func(t *testing.T) {
		otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
		otherKeyPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(otherKey),
		})
		GenerateClientP12(certPEM, otherKeyPEM, authority.CACertPEM(), "pw")
	})
}
