package marti

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cnak-us/gateway/audit"
	"software.sslmate.com/src/go-pkcs12"
)

// handleTLSConfig returns the certificate configuration XML that TAK clients
// use to build their CSR with the correct O and OU fields.
func (m *MartiAPI) handleTLSConfig(w http.ResponseWriter, r *http.Request) {
	org := m.cfg.CAOrganization

	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<certificateConfig xmlns="http://bbn.com/marti/xml/config">
  <nameEntries>
    <nameEntry name="O" value="%s"/>
    <nameEntry name="OU" value="tak-gateway"/>
  </nameEntries>
</certificateConfig>`, org)

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(xml))
}

// handleSignClient signs a client CSR and returns a PKCS#12 containing
// the signed certificate and CA certificate as trust store entries.
//
// ATAK enrollment flow:
//  1. ATAK generates a keypair locally and stores the private key
//  2. ATAK sends the CSR (containing its public key) to this endpoint
//  3. We sign the CSR and return a PKCS#12 with certificate-only entries
//  4. ATAK extracts the cert with alias "signedCert" and combines it
//     with its locally-stored private key to create a client identity
//  5. ATAK uses this identity for mTLS streaming on port 8089
//
// Password is always "atakatak" (TAK standard).
func (m *MartiAPI) handleSignClient(w http.ResponseWriter, r *http.Request) {
	// Validate basic auth
	username, ok := validateBasicAuth(r.Header.Get("Authorization"), m.credentials)
	if !ok {
		m.logger.Warn("enrollment auth failed", "remote", r.RemoteAddr)
		m.auditor.Log(audit.AuditEvent{
			Action:       "enroll",
			ResourceType: "certificate",
			IPAddress:    r.RemoteAddr,
			Status:       "failure",
			ErrorMessage: "authentication failed",
		})
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Get client UID from query params
	clientUID := r.URL.Query().Get("clientUID")
	if clientUID == "" {
		clientUID = r.URL.Query().Get("clientUid")
	}

	m.logger.Info("signing client CSR",
		"username", username,
		"clientUID", clientUID,
		"remote", r.RemoteAddr,
	)

	// Read CSR from body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		m.logger.Error("failed to read CSR body", "error", err)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	csrPEM := ensurePEMHeaders(string(body))

	// Sign the CSR - this preserves the client's public key from the CSR,
	// which matches the private key ATAK generated and stored locally.
	signedCertPEM, err := m.ca.SignCSR([]byte(csrPEM))
	if err != nil {
		m.logger.Error("failed to sign CSR", "error", err, "clientUID", clientUID)
		http.Error(w, "failed to sign CSR", http.StatusInternalServerError)
		return
	}

	// Parse the signed certificate
	signedBlock, _ := pem.Decode(signedCertPEM)
	if signedBlock == nil {
		m.logger.Error("failed to decode signed cert PEM")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	signedCert, err := x509.ParseCertificate(signedBlock.Bytes)
	if err != nil {
		m.logger.Error("failed to parse signed cert", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if m.certMappings != nil {
		m.certMappings.Store(signedCert.Subject.CommonName, username, clientUID)
	}

	// Update credential with certificate metadata so the listing shows CN/issued/expires.
	if updater, ok := m.credentials.(CertInfoUpdater); ok {
		if err := updater.UpdateCertInfo(username, signedCert.Subject.CommonName, signedCert.NotBefore, signedCert.NotAfter); err != nil {
			m.logger.Warn("failed to update cert info on credential", "username", username, "error", err)
		}
	}

	// Parse the CA certificate
	caBlock, _ := pem.Decode(m.ca.CACertPEM())
	if caBlock == nil {
		m.logger.Error("failed to decode CA cert PEM")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		m.logger.Error("failed to parse CA cert", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Build PKCS#12 with certificate-only trust store entries.
	// ATAK iterates aliases looking for "signedCert" (client cert)
	// and treats everything else as CA certificates for the trust store.
	// Uses LegacyDES (3DES + SHA-1) for Android Bouncy Castle compatibility.
	entries := []pkcs12.TrustStoreEntry{
		{Cert: signedCert, FriendlyName: "signedCert"},
		{Cert: caCert, FriendlyName: "ca"},
	}
	p12Data, err := pkcs12.LegacyDES.EncodeTrustStoreEntries(entries, "atakatak")
	if err != nil {
		m.logger.Error("failed to encode PKCS#12 response", "error", err)
		http.Error(w, "failed to build enrollment response", http.StatusInternalServerError)
		return
	}

	m.logger.Info("signed client certificate, returning PKCS#12",
		"username", username,
		"clientUID", clientUID,
		"cn", signedCert.Subject.CommonName,
		"p12_size", len(p12Data),
	)
	m.auditor.LogCertEnrolled(username, signedCert.Subject.CommonName, clientUID, r.RemoteAddr)

	w.Header().Set("Content-Type", "application/x-pkcs12")
	w.Write(p12Data)
}

// handleSignClientV2 signs a client CSR and returns the signed certificate
// and CA certificate as base64-encoded DER values.
//
// Content negotiation (matching OpenTAKServer behavior):
//   - Default / Accept: application/json, text/plain, */* → JSON response
//   - Accept: application/xml → XML response
//
// JSON (default for ATAK/iTAK):
//
//	{"signedCert":"<base64 DER>","ca0":"<base64 DER>","ca1":"<base64 DER>"}
//
// XML:
//
//	<enrollment><signedCert>base64 DER</signedCert><ca>base64 DER</ca></enrollment>
func (m *MartiAPI) handleSignClientV2(w http.ResponseWriter, r *http.Request) {
	// Validate basic auth
	username, ok := validateBasicAuth(r.Header.Get("Authorization"), m.credentials)
	if !ok {
		m.logger.Warn("enrollment auth failed", "remote", r.RemoteAddr)
		m.auditor.Log(audit.AuditEvent{
			Action:       "enroll",
			ResourceType: "certificate",
			IPAddress:    r.RemoteAddr,
			Status:       "failure",
			ErrorMessage: "authentication failed",
		})
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Get client UID from query params
	clientUID := r.URL.Query().Get("clientUID")
	if clientUID == "" {
		clientUID = r.URL.Query().Get("clientUid")
	}

	m.logger.Info("signing client CSR (v2)",
		"username", username,
		"clientUID", clientUID,
		"remote", r.RemoteAddr,
	)

	// Read CSR from body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		m.logger.Error("failed to read CSR body", "error", err)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	csrPEM := ensurePEMHeaders(string(body))

	// Sign the CSR
	signedCertPEM, err := m.ca.SignCSR([]byte(csrPEM))
	if err != nil {
		m.logger.Error("failed to sign CSR", "error", err, "clientUID", clientUID)
		http.Error(w, "failed to sign CSR", http.StatusInternalServerError)
		return
	}

	// Extract DER bytes from signed cert and CA cert, then base64 encode.
	// TAK Server's Util.certToPEM(cert, false) returns base64 DER with
	// 64-char line wrapping but NO PEM headers. We match this exactly.
	signedBlock, _ := pem.Decode(signedCertPEM)
	if signedBlock == nil {
		m.logger.Error("failed to decode signed cert PEM")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if signedCert, parseErr := x509.ParseCertificate(signedBlock.Bytes); parseErr == nil {
		if m.certMappings != nil {
			m.certMappings.Store(signedCert.Subject.CommonName, username, clientUID)
		}
		if updater, ok := m.credentials.(CertInfoUpdater); ok {
			if err := updater.UpdateCertInfo(username, signedCert.Subject.CommonName, signedCert.NotBefore, signedCert.NotAfter); err != nil {
				m.logger.Warn("failed to update cert info on credential", "username", username, "error", err)
			}
		}
	}

	signedB64 := base64Wrap(signedBlock.Bytes)

	caBlock, _ := pem.Decode(m.ca.CACertPEM())
	if caBlock == nil {
		m.logger.Error("failed to decode CA cert PEM")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	caB64 := base64Wrap(caBlock.Bytes)

	m.auditor.LogCertEnrolled(username, "v2-"+clientUID, clientUID, r.RemoteAddr)

	// Content negotiation matching OpenTAKServer behavior.
	// ATAK's commo sends no Accept header → default to JSON.
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/xml") {
		m.logger.Info("signed client certificate (v2), returning XML",
			"username", username,
			"clientUID", clientUID,
		)
		xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<enrollment><signedCert>%s</signedCert><ca>%s</ca></enrollment>`, signedB64, caB64)
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(xml))
	} else {
		m.logger.Info("signed client certificate (v2), returning JSON",
			"username", username,
			"clientUID", clientUID,
		)
		json := fmt.Sprintf(`{"signedCert":"%s","ca0":"%s","ca1":"%s"}`, signedB64, caB64, caB64)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(json))
	}
}

// handleEnrollmentProfile handles the device profile request that ATAK makes
// after certificate enrollment. ATAK expects a Mission Package (ZIP) or an
// empty 204 response if no profile is available. Returning 204 tells ATAK
// to skip profile import and proceed directly to reconnectStreams().
func (m *MartiAPI) handleEnrollmentProfile(w http.ResponseWriter, r *http.Request) {
	clientUID := r.URL.Query().Get("clientUid")
	m.logger.Info("enrollment profile requested", "clientUID", clientUID, "remote", r.RemoteAddr)
	w.WriteHeader(http.StatusNoContent)
}

// base64Wrap encodes raw bytes as base64 with 64-character line wrapping,
// matching TAK Server's Util.toPEM(bytes, false, ...) output format.
func base64Wrap(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	var wrapped strings.Builder
	for i := 0; i < len(encoded); i += 64 {
		end := i + 64
		if end > len(encoded) {
			end = len(encoded)
		}
		wrapped.WriteString(encoded[i:end])
		if end < len(encoded) {
			wrapped.WriteByte('\n')
		}
	}
	return wrapped.String()
}

// ensurePEMHeaders adds PEM headers to a CSR if they are missing.
func ensurePEMHeaders(csr string) string {
	csr = strings.TrimSpace(csr)

	if strings.Contains(csr, "BEGIN CERTIFICATE REQUEST") {
		return csr
	}

	// Raw base64 CSR without PEM headers
	if !strings.HasSuffix(csr, "\n") {
		csr += "\n"
	}
	return "-----BEGIN CERTIFICATE REQUEST-----\n" + csr + "-----END CERTIFICATE REQUEST-----"
}
