package ca

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNewCA(t *testing.T) {
	ca := NewCA()
	if ca == nil {
		t.Fatal("NewCA returned nil")
	}
	if ca.revokedSerials == nil {
		t.Fatal("revokedSerials map not initialized")
	}
	if ca.caCert != nil {
		t.Error("expected nil caCert on new CA")
	}
}

func TestGenerateCA(t *testing.T) {
	t.Run("basic generation", func(t *testing.T) {
		ca := NewCA()
		if err := ca.GenerateCA("TestOrg", "TestOU"); err != nil {
			t.Fatalf("GenerateCA failed: %v", err)
		}

		if ca.caCert == nil {
			t.Fatal("caCert is nil after generation")
		}
		if ca.caKey == nil {
			t.Fatal("caKey is nil after generation")
		}
		if len(ca.caCertPEM) == 0 {
			t.Fatal("caCertPEM is empty")
		}
		if len(ca.caKeyPEM) == 0 {
			t.Fatal("caKeyPEM is empty")
		}

		cert := ca.caCert
		if !cert.IsCA {
			t.Error("certificate is not marked as CA")
		}
		if cert.MaxPathLen != 1 {
			t.Errorf("expected MaxPathLen 1, got %d", cert.MaxPathLen)
		}
		if cert.Subject.Organization[0] != "TestOrg" {
			t.Errorf("expected org TestOrg, got %s", cert.Subject.Organization[0])
		}
		if cert.Subject.OrganizationalUnit[0] != "TestOU" {
			t.Errorf("expected OU TestOU, got %s", cert.Subject.OrganizationalUnit[0])
		}
		if cert.Subject.CommonName != "TestOrg CA" {
			t.Errorf("expected CN 'TestOrg CA', got %s", cert.Subject.CommonName)
		}
		if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
			t.Error("CA cert missing KeyUsageCertSign")
		}
		if cert.KeyUsage&x509.KeyUsageCRLSign == 0 {
			t.Error("CA cert missing KeyUsageCRLSign")
		}
		if time.Until(cert.NotAfter) < 9*365*24*time.Hour {
			t.Error("certificate validity period is too short")
		}
	})

	t.Run("empty org and ou", func(t *testing.T) {
		ca := NewCA()
		if err := ca.GenerateCA("", ""); err != nil {
			t.Fatalf("GenerateCA with empty org/ou failed: %v", err)
		}
		if ca.caCert.Subject.CommonName != " CA" {
			t.Errorf("unexpected CN: %s", ca.caCert.Subject.CommonName)
		}
	})
}

func TestCACertPEM(t *testing.T) {
	t.Run("nil before init", func(t *testing.T) {
		ca := NewCA()
		if got := ca.CACertPEM(); got != nil {
			t.Errorf("expected nil, got %d bytes", len(got))
		}
	})

	t.Run("valid after init", func(t *testing.T) {
		ca := NewCA()
		ca.GenerateCA("Test", "Test")
		certPEM := ca.CACertPEM()
		if len(certPEM) == 0 {
			t.Fatal("CACertPEM returned empty bytes")
		}
		block, _ := pem.Decode(certPEM)
		if block == nil {
			t.Fatal("failed to decode PEM")
		}
		if block.Type != "CERTIFICATE" {
			t.Errorf("expected CERTIFICATE block type, got %s", block.Type)
		}
	})
}

func TestCACertBase64(t *testing.T) {
	t.Run("empty before init", func(t *testing.T) {
		ca := NewCA()
		if got := ca.CACertBase64(); got != "" {
			t.Errorf("expected empty string, got %s", got)
		}
	})

	t.Run("non-empty after init", func(t *testing.T) {
		ca := NewCA()
		ca.GenerateCA("Test", "Test")
		if b64 := ca.CACertBase64(); b64 == "" {
			t.Fatal("CACertBase64 returned empty string")
		}
	})
}

func TestSignCSR(t *testing.T) {
	ca := NewCA()
	ca.GenerateCA("TestOrg", "TestOU")

	t.Run("valid CSR", func(t *testing.T) {
		csrPEM := generateTestCSR(t, "test-client")
		certPEM, err := ca.SignCSR(csrPEM)
		if err != nil {
			t.Fatalf("SignCSR failed: %v", err)
		}
		block, _ := pem.Decode(certPEM)
		if block == nil {
			t.Fatal("failed to decode signed cert PEM")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("failed to parse signed cert: %v", err)
		}
		if cert.Subject.CommonName != "test-client" {
			t.Errorf("expected CN test-client, got %s", cert.Subject.CommonName)
		}
		if cert.IsCA {
			t.Error("signed cert should not be CA")
		}
		if len(cert.ExtKeyUsage) == 0 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
			t.Error("signed cert missing ClientAuth EKU")
		}
		roots := x509.NewCertPool()
		roots.AddCert(ca.caCert)
		if _, err := cert.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
			t.Fatalf("cert verification failed: %v", err)
		}
	})

	t.Run("CA not initialized", func(t *testing.T) {
		uninitCA := NewCA()
		csrPEM := generateTestCSR(t, "test")
		_, err := uninitCA.SignCSR(csrPEM)
		if err == nil {
			t.Fatal("expected error for uninitialized CA")
		}
	})

	t.Run("invalid PEM", func(t *testing.T) {
		_, err := ca.SignCSR([]byte("not a PEM"))
		if err == nil {
			t.Fatal("expected error for invalid PEM")
		}
	})

	t.Run("corrupt CSR data", func(t *testing.T) {
		badPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: []byte("corrupt data")})
		_, err := ca.SignCSR(badPEM)
		if err == nil {
			t.Fatal("expected error for corrupt CSR")
		}
	})

	t.Run("tampered CSR signature", func(t *testing.T) {
		key, _ := rsa.GenerateKey(rand.Reader, 2048)
		template := &x509.CertificateRequest{
			Subject: pkix.Name{CommonName: "tampered"},
		}
		csrDER, _ := x509.CreateCertificateRequest(rand.Reader, template, key)
		csrDER[len(csrDER)-1] ^= 0xFF
		csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
		_, err := ca.SignCSR(csrPEM)
		if err == nil {
			t.Fatal("expected error for tampered CSR signature")
		}
	})
}

func TestGenerateClientCert(t *testing.T) {
	ca := NewCA()
	ca.GenerateCA("TestOrg", "TestOU")

	t.Run("default expiry", func(t *testing.T) {
		certPEM, keyPEM, err := ca.GenerateClientCert("test-client", 0)
		if err != nil {
			t.Fatalf("GenerateClientCert failed: %v", err)
		}
		if len(certPEM) == 0 || len(keyPEM) == 0 {
			t.Fatal("empty cert or key PEM")
		}
		block, _ := pem.Decode(certPEM)
		cert, _ := x509.ParseCertificate(block.Bytes)
		if cert.Subject.CommonName != "test-client" {
			t.Errorf("expected CN test-client, got %s", cert.Subject.CommonName)
		}
	})

	t.Run("custom expiry", func(t *testing.T) {
		certPEM, _, err := ca.GenerateClientCert("short-lived", 24*time.Hour)
		if err != nil {
			t.Fatalf("GenerateClientCert failed: %v", err)
		}
		block, _ := pem.Decode(certPEM)
		cert, _ := x509.ParseCertificate(block.Bytes)
		validity := cert.NotAfter.Sub(cert.NotBefore)
		if validity < 24*time.Hour || validity > 25*time.Hour {
			t.Errorf("unexpected validity period: %v", validity)
		}
	})

	t.Run("CA not initialized", func(t *testing.T) {
		uninitCA := NewCA()
		_, _, err := uninitCA.GenerateClientCert("test", 0)
		if err == nil {
			t.Fatal("expected error for uninitialized CA")
		}
	})

	t.Run("verifies against CA", func(t *testing.T) {
		certPEM, _, _ := ca.GenerateClientCert("verified", 0)
		block, _ := pem.Decode(certPEM)
		cert, _ := x509.ParseCertificate(block.Bytes)
		roots := x509.NewCertPool()
		roots.AddCert(ca.caCert)
		if _, err := cert.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
			t.Fatalf("client cert verification failed: %v", err)
		}
	})
}

func TestGenerateServerCert(t *testing.T) {
	ca := NewCA()
	ca.GenerateCA("TestOrg", "TestOU")

	t.Run("with DNS and IP SANs", func(t *testing.T) {
		certPEM, keyPEM, err := ca.GenerateServerCert("myserver", []string{"example.com", "192.168.1.1", "10.0.0.1", "localhost"}, 0)
		if err != nil {
			t.Fatalf("GenerateServerCert failed: %v", err)
		}
		if len(certPEM) == 0 || len(keyPEM) == 0 {
			t.Fatal("empty cert or key PEM")
		}
		block, _ := pem.Decode(certPEM)
		cert, _ := x509.ParseCertificate(block.Bytes)
		if cert.Subject.CommonName != "myserver" {
			t.Errorf("expected CN myserver, got %s", cert.Subject.CommonName)
		}
		if len(cert.DNSNames) != 2 {
			t.Errorf("expected 2 DNS names, got %d: %v", len(cert.DNSNames), cert.DNSNames)
		}
		if len(cert.IPAddresses) != 2 {
			t.Errorf("expected 2 IP addresses, got %d: %v", len(cert.IPAddresses), cert.IPAddresses)
		}
		if len(cert.ExtKeyUsage) == 0 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
			t.Error("server cert missing ServerAuth EKU")
		}
	})

	t.Run("empty SANs", func(t *testing.T) {
		certPEM, _, err := ca.GenerateServerCert("bare-server", nil, 0)
		if err != nil {
			t.Fatalf("GenerateServerCert failed: %v", err)
		}
		block, _ := pem.Decode(certPEM)
		cert, _ := x509.ParseCertificate(block.Bytes)
		if len(cert.DNSNames) != 0 || len(cert.IPAddresses) != 0 {
			t.Error("expected no SANs for empty input")
		}
	})

	t.Run("empty string SAN filtered", func(t *testing.T) {
		certPEM, _, _ := ca.GenerateServerCert("server", []string{"", "valid.com", ""}, 0)
		block, _ := pem.Decode(certPEM)
		cert, _ := x509.ParseCertificate(block.Bytes)
		if len(cert.DNSNames) != 1 {
			t.Errorf("expected 1 DNS name, got %d: %v", len(cert.DNSNames), cert.DNSNames)
		}
	})

	t.Run("CA not initialized", func(t *testing.T) {
		uninitCA := NewCA()
		_, _, err := uninitCA.GenerateServerCert("test", nil, 0)
		if err == nil {
			t.Fatal("expected error for uninitialized CA")
		}
	})
}

func TestLoadOrCreate(t *testing.T) {
	t.Run("create new", func(t *testing.T) {
		dir := t.TempDir()
		certFile := filepath.Join(dir, "ca.crt")
		keyFile := filepath.Join(dir, "ca.key")
		ca := NewCA()
		if err := ca.LoadOrCreate(certFile, keyFile, "TestOrg"); err != nil {
			t.Fatalf("LoadOrCreate failed: %v", err)
		}
		if ca.caCert == nil {
			t.Fatal("CA cert not generated")
		}
		if _, err := os.Stat(certFile); os.IsNotExist(err) {
			t.Error("cert file not created")
		}
		if _, err := os.Stat(keyFile); os.IsNotExist(err) {
			t.Error("key file not created")
		}
	})

	t.Run("load existing", func(t *testing.T) {
		dir := t.TempDir()
		certFile := filepath.Join(dir, "ca.crt")
		keyFile := filepath.Join(dir, "ca.key")
		ca1 := NewCA()
		ca1.LoadOrCreate(certFile, keyFile, "TestOrg")
		origSerial := ca1.caCert.SerialNumber

		ca2 := NewCA()
		if err := ca2.LoadOrCreate(certFile, keyFile, "TestOrg"); err != nil {
			t.Fatalf("LoadOrCreate (reload) failed: %v", err)
		}
		if ca2.caCert.SerialNumber.Cmp(origSerial) != 0 {
			t.Error("loaded CA has different serial number")
		}
	})
}

func TestLoadFromPEM(t *testing.T) {
	t.Run("valid PKCS1 key", func(t *testing.T) {
		ca := NewCA()
		ca.GenerateCA("Test", "Test")
		ca2 := NewCA()
		if err := ca2.loadFromPEM(ca.caCertPEM, ca.caKeyPEM); err != nil {
			t.Fatalf("loadFromPEM failed: %v", err)
		}
	})

	t.Run("invalid cert PEM", func(t *testing.T) {
		ca := NewCA()
		if err := ca.loadFromPEM([]byte("not a cert"), []byte("not a key")); err == nil {
			t.Fatal("expected error for invalid cert PEM")
		}
	})

	t.Run("valid cert but invalid key PEM", func(t *testing.T) {
		ca1 := NewCA()
		ca1.GenerateCA("Test", "Test")
		ca2 := NewCA()
		if err := ca2.loadFromPEM(ca1.caCertPEM, []byte("not a key")); err == nil {
			t.Fatal("expected error for invalid key PEM")
		}
	})

	t.Run("corrupt key data in PEM", func(t *testing.T) {
		ca1 := NewCA()
		ca1.GenerateCA("Test", "Test")
		badKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("corrupt")})
		ca2 := NewCA()
		if err := ca2.loadFromPEM(ca1.caCertPEM, badKeyPEM); err == nil {
			t.Fatal("expected error for corrupt key data")
		}
	})
}

func TestConcurrentAccess(t *testing.T) {
	ca := NewCA()
	ca.GenerateCA("ConcurrencyTest", "Test")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 4 {
			case 0:
				ca.CACertPEM()
			case 1:
				ca.CACertBase64()
			case 2:
				ca.GenerateClientCert("concurrent-client", 0)
			case 3:
				csrPEM := generateTestCSR(t, "concurrent-csr")
				ca.SignCSR(csrPEM)
			}
		}(i)
	}
	wg.Wait()
}

func generateTestCSR(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	template := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: cn},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		t.Fatalf("failed to create CSR: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
}

func TestRandomSerial(t *testing.T) {
	serials := make(map[string]bool)
	for i := 0; i < 100; i++ {
		s, err := randomSerial()
		if err != nil {
			t.Fatalf("randomSerial failed: %v", err)
		}
		if s.Cmp(big.NewInt(0)) <= 0 {
			t.Error("serial should be positive")
		}
		key := s.String()
		if serials[key] {
			t.Errorf("duplicate serial number: %s", key)
		}
		serials[key] = true
	}
}
