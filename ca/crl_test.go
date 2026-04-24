package ca

import (
	"crypto/x509"
	"math/big"
	"sync"
	"testing"
)

func TestRevokeSerial(t *testing.T) {
	ca := NewCA()
	ca.GenerateCA("Test", "Test")
	serial := big.NewInt(12345)
	ca.RevokeSerial(serial)
	if !ca.IsRevoked(serial) {
		t.Error("serial should be revoked after RevokeSerial")
	}
}

func TestIsRevoked(t *testing.T) {
	ca := NewCA()
	ca.GenerateCA("Test", "Test")

	t.Run("not revoked", func(t *testing.T) {
		if ca.IsRevoked(big.NewInt(99999)) {
			t.Error("serial should not be revoked")
		}
	})

	t.Run("revoked", func(t *testing.T) {
		serial := big.NewInt(11111)
		ca.RevokeSerial(serial)
		if !ca.IsRevoked(serial) {
			t.Error("serial should be revoked")
		}
	})

	t.Run("multiple serials", func(t *testing.T) {
		s1 := big.NewInt(1)
		s2 := big.NewInt(2)
		s3 := big.NewInt(3)
		ca.RevokeSerial(s1)
		ca.RevokeSerial(s3)
		if !ca.IsRevoked(s1) {
			t.Error("s1 should be revoked")
		}
		if ca.IsRevoked(s2) {
			t.Error("s2 should not be revoked")
		}
		if !ca.IsRevoked(s3) {
			t.Error("s3 should be revoked")
		}
	})

	t.Run("large serial number", func(t *testing.T) {
		large := new(big.Int)
		large.SetString("340282366920938463463374607431768211455", 10)
		ca.RevokeSerial(large)
		if !ca.IsRevoked(large) {
			t.Error("large serial should be revoked")
		}
	})
}

func TestGenerateCRL(t *testing.T) {
	t.Run("empty CRL", func(t *testing.T) {
		ca := NewCA()
		ca.GenerateCA("Test", "Test")
		crlDER, err := ca.GenerateCRL()
		if err != nil {
			t.Fatalf("GenerateCRL failed: %v", err)
		}
		if len(crlDER) == 0 {
			t.Fatal("empty CRL data")
		}
		crl, err := x509.ParseRevocationList(crlDER)
		if err != nil {
			t.Fatalf("failed to parse CRL: %v", err)
		}
		if len(crl.RevokedCertificateEntries) != 0 {
			t.Errorf("expected 0 revoked certs, got %d", len(crl.RevokedCertificateEntries))
		}
	})

	t.Run("CRL with revoked certs", func(t *testing.T) {
		ca := NewCA()
		ca.GenerateCA("Test", "Test")
		ca.RevokeSerial(big.NewInt(100))
		ca.RevokeSerial(big.NewInt(200))
		ca.RevokeSerial(big.NewInt(300))

		crlDER, err := ca.GenerateCRL()
		if err != nil {
			t.Fatalf("GenerateCRL failed: %v", err)
		}
		crl, err := x509.ParseRevocationList(crlDER)
		if err != nil {
			t.Fatalf("failed to parse CRL: %v", err)
		}
		if len(crl.RevokedCertificateEntries) != 3 {
			t.Errorf("expected 3 revoked certs, got %d", len(crl.RevokedCertificateEntries))
		}
		if err := crl.CheckSignatureFrom(ca.caCert); err != nil {
			t.Fatalf("CRL signature verification failed: %v", err)
		}
	})

	t.Run("CA not initialized", func(t *testing.T) {
		ca := NewCA()
		_, err := ca.GenerateCRL()
		if err == nil {
			t.Fatal("expected error for uninitialized CA")
		}
	})

	t.Run("CRL validity period", func(t *testing.T) {
		ca := NewCA()
		ca.GenerateCA("Test", "Test")
		crlDER, _ := ca.GenerateCRL()
		crl, _ := x509.ParseRevocationList(crlDER)
		if crl.NextUpdate.Before(crl.ThisUpdate) {
			t.Error("NextUpdate should be after ThisUpdate")
		}
	})
}

func TestCRLConcurrentAccess(t *testing.T) {
	ca := NewCA()
	ca.GenerateCA("ConcurrencyTest", "Test")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ca.RevokeSerial(big.NewInt(int64(i)))
		}(i)
	}
	wg.Wait()

	for i := 0; i < 20; i++ {
		if !ca.IsRevoked(big.NewInt(int64(i))) {
			t.Errorf("serial %d should be revoked", i)
		}
	}

	crlDER, err := ca.GenerateCRL()
	if err != nil {
		t.Fatalf("GenerateCRL failed: %v", err)
	}
	crl, _ := x509.ParseRevocationList(crlDER)
	if len(crl.RevokedCertificateEntries) != 20 {
		t.Errorf("expected 20 revoked certs in CRL, got %d", len(crl.RevokedCertificateEntries))
	}
}
