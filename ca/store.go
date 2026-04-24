package ca

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"software.sslmate.com/src/go-pkcs12"
)

// SavePEM writes PEM-encoded data to a file with restricted permissions.
func SavePEM(data []byte, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// LoadPEM reads PEM-encoded data from a file.
func LoadPEM(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return data, nil
}

// GenerateTrustStoreP12 creates a PKCS#12 file containing only the CA certificate.
// TAK clients use this as their truststore to verify server certificates.
func GenerateTrustStoreP12(caCertPEM []byte, password string) ([]byte, error) {
	block, _ := pem.Decode(caCertPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode CA certificate PEM")
	}

	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing CA certificate: %w", err)
	}

	p12, err := pkcs12.LegacyDES.EncodeTrustStoreEntries([]pkcs12.TrustStoreEntry{
		{Cert: caCert, FriendlyName: "truststore-root"},
	}, password)
	if err != nil {
		return nil, fmt.Errorf("encoding truststore P12: %w", err)
	}

	return p12, nil
}

// GenerateClientP12 creates a PKCS#12 file containing a client certificate,
// its private key, and the CA certificate chain.
func GenerateClientP12(certPEM, keyPEM, caCertPEM []byte, password string) ([]byte, error) {
	// Parse client certificate.
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, fmt.Errorf("failed to decode client certificate PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing client certificate: %w", err)
	}

	// Parse private key.
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("failed to decode private key PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}

	// Parse CA certificate.
	caBlock, _ := pem.Decode(caCertPEM)
	if caBlock == nil {
		return nil, fmt.Errorf("failed to decode CA certificate PEM")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing CA certificate: %w", err)
	}

	p12, err := pkcs12.LegacyDES.Encode(key, cert, []*x509.Certificate{caCert}, password)
	if err != nil {
		return nil, fmt.Errorf("encoding client P12: %w", err)
	}

	return p12, nil
}
