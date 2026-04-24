package ca

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"sort"
	"sync"
	"time"
)

// CertificateAuthority manages X.509 certificates for TAK client authentication.
type CertificateAuthority struct {
	caCert         *x509.Certificate
	caKey          crypto.PrivateKey
	caCertPEM      []byte
	caKeyPEM       []byte
	mu             sync.RWMutex
	revokedSerials map[string]time.Time
}

// NewCA returns a new CertificateAuthority instance.
func NewCA() *CertificateAuthority {
	return &CertificateAuthority{
		revokedSerials: make(map[string]time.Time),
	}
}

// LoadOrCreate loads an existing CA certificate and key from disk, or generates
// a new self-signed CA if the files do not exist.
func (ca *CertificateAuthority) LoadOrCreate(certFile, keyFile, org string) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	// Try to load existing files.
	certPEM, certErr := os.ReadFile(certFile)
	keyPEM, keyErr := os.ReadFile(keyFile)

	if certErr == nil && keyErr == nil {
		return ca.loadFromPEM(certPEM, keyPEM)
	}

	// Generate a new CA.
	if err := ca.generateCA(org, "Certificate Authority"); err != nil {
		return fmt.Errorf("generating CA: %w", err)
	}

	// Persist to disk.
	if err := SavePEM(ca.caCertPEM, certFile); err != nil {
		return fmt.Errorf("saving CA cert: %w", err)
	}
	if err := SavePEM(ca.caKeyPEM, keyFile); err != nil {
		return fmt.Errorf("saving CA key: %w", err)
	}

	return nil
}

// GenerateCA creates a self-signed CA certificate with RSA-4096 and 10-year validity.
func (ca *CertificateAuthority) GenerateCA(org, ou string) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	return ca.generateCA(org, ou)
}

func (ca *CertificateAuthority) generateCA(org, ou string) error {
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("generating RSA key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization:       []string{org},
			OrganizationalUnit: []string{ou},
			CommonName:         org + " CA",
		},
		NotBefore:             time.Now().Add(-5 * time.Minute),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("creating CA certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return fmt.Errorf("parsing CA certificate: %w", err)
	}

	ca.caCert = cert
	ca.caKey = key
	ca.caCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	ca.caKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	return nil
}

// SignCSR signs a PEM-encoded certificate signing request and returns a PEM-encoded
// client certificate (RSA 2048, 1 year validity, client auth).
func (ca *CertificateAuthority) SignCSR(csrPEM []byte) ([]byte, error) {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	if ca.caCert == nil {
		return nil, fmt.Errorf("CA not initialized")
	}

	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode CSR PEM")
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("CSR signature invalid: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      csr.Subject,
		NotBefore:    time.Now().Add(-5 * time.Minute),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.caCert, csr.PublicKey, ca.caKey)
	if err != nil {
		return nil, fmt.Errorf("signing certificate: %w", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), nil
}

// GenerateClientCert creates a client certificate and private key signed by the CA.
func (ca *CertificateAuthority) GenerateClientCert(cn string, expiry time.Duration) (certPEM, keyPEM []byte, err error) {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	if ca.caCert == nil {
		return nil, nil, fmt.Errorf("CA not initialized")
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generating client key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}

	if expiry == 0 {
		expiry = 365 * 24 * time.Hour
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: cn,
		},
		NotBefore:   time.Now().Add(-5 * time.Minute),
		NotAfter:    time.Now().Add(expiry),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.caCert, &key.PublicKey, ca.caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("creating client certificate: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	return certPEM, keyPEM, nil
}

// GenerateServerCert creates a server certificate with SANs, signed by the CA.
// It uses RSA-2048 keys by default. Use GenerateServerCertWithKeyType for ECDSA.
func (ca *CertificateAuthority) GenerateServerCert(cn string, sans []string, expiry time.Duration) (certPEM, keyPEM []byte, err error) {
	return ca.GenerateServerCertWithKeyType(cn, sans, expiry, KeyTypeRSA)
}

// GenerateServerCertWithKeyType creates a server certificate using the specified key type.
// KeyTypeECDSA generates P-256 keys which enable ECDHE_ECDSA cipher suites required
// by TAK Server (Marti) interoperability.
func (ca *CertificateAuthority) GenerateServerCertWithKeyType(cn string, sans []string, expiry time.Duration, keyType KeyType) (certPEM, keyPEM []byte, err error) {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	if ca.caCert == nil {
		return nil, nil, fmt.Errorf("CA not initialized")
	}

	privKey, pubKey, kPEM, err := generateKeyPair(keyType)
	if err != nil {
		return nil, nil, fmt.Errorf("generating server key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}

	if expiry == 0 {
		expiry = 365 * 24 * time.Hour
	}

	// Separate DNS names from IP addresses for proper x509 SAN handling.
	var dnsNames []string
	var ipAddrs []net.IP
	for _, san := range sans {
		if ip := net.ParseIP(san); ip != nil {
			ipAddrs = append(ipAddrs, ip)
		} else if san != "" {
			dnsNames = append(dnsNames, san)
		}
	}

	// Sort SANs for deterministic certificate output.
	sort.Strings(dnsNames)
	sort.Slice(ipAddrs, func(i, j int) bool {
		return ipAddrs[i].String() < ipAddrs[j].String()
	})

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: cn,
		},
		DNSNames:    dnsNames,
		IPAddresses: ipAddrs,
		NotBefore:   time.Now().Add(-5 * time.Minute),
		NotAfter:    time.Now().Add(expiry),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.caCert, pubKey, ca.caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("creating server certificate: %w", err)
	}

	_ = privKey // used indirectly via pubKey; retained for clarity
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	return certPEM, kPEM, nil
}

// CACertPEM returns the CA certificate in PEM format.
func (ca *CertificateAuthority) CACertPEM() []byte {
	ca.mu.RLock()
	defer ca.mu.RUnlock()
	return ca.caCertPEM
}

// CACertBase64 returns the CA certificate as base64 (DER bytes, no PEM headers).
// This is the format used in Marti API responses.
func (ca *CertificateAuthority) CACertBase64() string {
	ca.mu.RLock()
	defer ca.mu.RUnlock()
	if ca.caCert == nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(ca.caCert.Raw)
}

func (ca *CertificateAuthority) loadFromPEM(certPEM, keyPEM []byte) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("failed to decode CA certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parsing CA certificate: %w", err)
	}

	block, _ = pem.Decode(keyPEM)
	if block == nil {
		return fmt.Errorf("failed to decode CA key PEM")
	}

	// Try PKCS#8 first (more common in modern tools), then fall back to PKCS#1.
	var key crypto.PrivateKey
	key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		var pkcs1Key *rsa.PrivateKey
		pkcs1Key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return fmt.Errorf("parsing CA key (tried PKCS#8 and PKCS#1): %w", err)
		}
		key = pkcs1Key
	}

	ca.caCert = cert
	ca.caKey = key
	ca.caCertPEM = certPEM
	ca.caKeyPEM = keyPEM

	return nil
}

// KeyType controls whether certificates use RSA or ECDSA keys.
type KeyType string

const (
	KeyTypeRSA   KeyType = "rsa"
	KeyTypeECDSA KeyType = "ecdsa"
)

// generateKeyPair generates a private key of the given type and returns the
// key, its public key, and PEM-encoded private key bytes.
func generateKeyPair(keyType KeyType) (crypto.PrivateKey, crypto.PublicKey, []byte, error) {
	switch keyType {
	case KeyTypeECDSA:
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("generating ECDSA key: %w", err)
		}
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("marshaling ECDSA key: %w", err)
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
		return key, &key.PublicKey, keyPEM, nil
	default: // RSA
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("generating RSA key: %w", err)
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		return key, &key.PublicKey, keyPEM, nil
	}
}

func randomSerial() (*big.Int, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, fmt.Errorf("generating serial number: %w", err)
	}
	return serial, nil
}
