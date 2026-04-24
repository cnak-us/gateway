package marti

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/cnak-us/gateway/audit"
	"github.com/cnak-us/gateway/ca"
	"github.com/cnak-us/gateway/clients"
	"github.com/cnak-us/gateway/config"
	"github.com/cnak-us/gateway/server"
)

// MartiAPI serves the HTTPS Marti API that TAK clients use for enrollment,
// configuration, and contact discovery.
type MartiAPI struct {
	router       chi.Router
	ca           *ca.CertificateAuthority
	logger       *slog.Logger
	cfg          *config.Config
	credentials  CredentialValidator
	registry     *clients.Registry
	auditor      *audit.Auditor
	certMappings *CertMappingStore
}

// NewMartiAPI creates a new MartiAPI with all routes registered.
func NewMartiAPI(cfg *config.Config, authority *ca.CertificateAuthority, creds CredentialValidator, registry *clients.Registry, auditor *audit.Auditor, certMappings *CertMappingStore, logger *slog.Logger) *MartiAPI {
	m := &MartiAPI{
		router:       chi.NewRouter(),
		ca:           authority,
		logger:       logger.With("component", "marti-api"),
		cfg:          cfg,
		credentials:  creds,
		registry:     registry,
		auditor:      auditor,
		certMappings: certMappings,
	}
	m.registerRoutes()
	return m
}

func (m *MartiAPI) registerRoutes() {
	r := m.router

	// Certificate enrollment
	r.Get("/Marti/api/tls/config", m.handleTLSConfig)
	r.Post("/Marti/api/tls/signClient", m.handleSignClient)
	r.Post("/Marti/api/tls/signClient/", m.handleSignClient)
	r.Post("/Marti/api/tls/signClient/v2", m.handleSignClientV2)
	r.Get("/Marti/api/tls/profile/enrollment", m.handleEnrollmentProfile)

	// Server config
	r.Get("/Marti/api/version/config", m.handleVersionConfig)

	// Contacts
	r.Get("/Marti/api/contacts/all", m.handleContactsAll)

	// Client endpoints
	r.Get("/Marti/api/clientEndPoints", m.handleClientEndPoints)
}

// ListenAndServeTLS starts the HTTPS server using the CA's server certificate.
// It uses server-side TLS only (not mutual TLS) so that enrollment works
// without a client certificate.
func (m *MartiAPI) ListenAndServeTLS(ctx context.Context) error {
	// SANs must include every hostname/IP that TAK clients use to connect.
	sans := []string{"tak-gateway", "localhost", "10.0.0.127", "73.34.184.79"}
	sans = append(sans, m.cfg.ServerSANs...)

	// Use configured key type (ECDSA enables ECDHE_ECDSA suites for TAK Server compat).
	keyType := ca.KeyType(m.cfg.KeyType)
	if keyType != ca.KeyTypeECDSA && keyType != ca.KeyTypeRSA {
		keyType = ca.KeyTypeECDSA
	}
	serverCertPEM, serverKeyPEM, err := m.ca.GenerateServerCertWithKeyType(
		"tak-gateway",
		sans,
		0,
		keyType,
	)
	if err != nil {
		return fmt.Errorf("generating server cert: %w", err)
	}

	tlsCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		return fmt.Errorf("loading server TLS keypair: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates:           []tls.Certificate{tlsCert},
		MinVersion:             tls.VersionTLS12,
		SessionTicketsDisabled: true, // Prevent "bad record MAC" from session resumption after enrollment
	}
	server.ApplyTLSConfig(tlsConfig, m.cfg)

	addr := fmt.Sprintf(":%d", m.cfg.HTTPSAPIPort)
	srv := &http.Server{
		Addr:      addr,
		Handler:   m.router,
		TLSConfig: tlsConfig,
	}

	go func() {
		<-ctx.Done()
		m.logger.Info("shutting down Marti HTTPS API")
		srv.Close()
	}()

	m.logger.Info("starting Marti HTTPS API", "addr", addr)
	ln, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serving Marti API: %w", err)
	}
	return nil
}
