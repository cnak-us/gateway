package ca

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"fmt"
	"math/big"
	"time"
)

// RevokeSerial adds a certificate serial number to the revocation list.
func (ca *CertificateAuthority) RevokeSerial(serial *big.Int) {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	ca.revokedSerials[serial.String()] = time.Now()
}

// IsRevoked checks whether a certificate serial number has been revoked.
func (ca *CertificateAuthority) IsRevoked(serial *big.Int) bool {
	ca.mu.RLock()
	defer ca.mu.RUnlock()
	_, revoked := ca.revokedSerials[serial.String()]
	return revoked
}

// GenerateCRL creates a DER-encoded X.509 certificate revocation list
// containing all revoked serial numbers.
func (ca *CertificateAuthority) GenerateCRL() ([]byte, error) {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	if ca.caCert == nil {
		return nil, fmt.Errorf("CA not initialized")
	}

	template := &x509.RevocationList{
		RevokedCertificateEntries: make([]x509.RevocationListEntry, 0, len(ca.revokedSerials)),
		Number:                    big.NewInt(1),
		ThisUpdate:                time.Now(),
		NextUpdate:                time.Now().Add(24 * time.Hour),
	}

	for serialStr, revokedAt := range ca.revokedSerials {
		serial := new(big.Int)
		serial.SetString(serialStr, 10)
		template.RevokedCertificateEntries = append(template.RevokedCertificateEntries, x509.RevocationListEntry{
			SerialNumber:   serial,
			RevocationTime: revokedAt,
		})
	}

	signer, ok := ca.caKey.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("CA key does not implement crypto.Signer")
	}

	crlDER, err := x509.CreateRevocationList(rand.Reader, template, ca.caCert, signer)
	if err != nil {
		return nil, fmt.Errorf("creating CRL: %w", err)
	}

	return crlDER, nil
}
