package management

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/cnak-us/cnak/pkg/httputil"
	"github.com/cnak-us/gateway/audit"
	"github.com/cnak-us/gateway/backend"
	"github.com/cnak-us/gateway/ca"
	"github.com/cnak-us/gateway/clients"
	"github.com/cnak-us/gateway/config"
	"github.com/cnak-us/gateway/metrics"
	"github.com/cnak-us/gateway/server"
)

// ManagementAPI serves the internal management REST API.
type ManagementAPI struct {
	router        chi.Router
	ca            *ca.CertificateAuthority
	registry      *clients.Registry
	manager       *clients.Manager
	creds         *CredentialStore
	auditor       *audit.Auditor
	cfg           *config.Config
	logger        *slog.Logger
	backendClient *backend.Client
}

// NewManagementAPI creates a new management API server.
func NewManagementAPI(
	authority *ca.CertificateAuthority,
	registry *clients.Registry,
	manager *clients.Manager,
	creds *CredentialStore,
	auditor *audit.Auditor,
	cfg *config.Config,
	logger *slog.Logger,
	backendClient *backend.Client,
) *ManagementAPI {
	api := &ManagementAPI{
		router:        chi.NewRouter(),
		ca:            authority,
		registry:      registry,
		manager:       manager,
		creds:         creds,
		auditor:       auditor,
		cfg:           cfg,
		logger:        logger,
		backendClient: backendClient,
	}
	api.routes()
	return api
}

// RegisterDocsHandler adds an OpenAPI spec endpoint to the management router.
func (a *ManagementAPI) RegisterDocsHandler(handler http.HandlerFunc) {
	a.router.Get("/api/tak-gateway/docs/openapi.yaml", handler)
}

func (a *ManagementAPI) routes() {
	a.router.Get("/health", a.handleHealth)
	a.router.Handle("/metrics", metrics.Handler())
	a.router.Route("/api/tak-gateway", func(r chi.Router) {
		r.Use(httputil.ServiceAuthMiddleware)
		a.mountGatewayRoutes(r)
	})
}

// GatewayRoutes returns a chi.Router with just the /api/tak-gateway/* routes,
// suitable for mounting on an external router (e.g., when embedded in the backend).
func (a *ManagementAPI) GatewayRoutes() chi.Router {
	r := chi.NewRouter()
	a.mountGatewayRoutes(r)
	return r
}

// mountGatewayRoutes registers all gateway management routes on the given router.
// Routes are registered relative to the router root; when used standalone the
// caller wraps them under /api/tak-gateway, and when embedded the backend mounts
// them at that prefix.
func (a *ManagementAPI) mountGatewayRoutes(r chi.Router) {
	r.Get("/status", a.handleStatus)
	r.Get("/clients", a.handleListClients)
	r.Delete("/clients/{uid}", a.handleKickClient)
	r.Get("/certificates", a.handleListCertificates)
	r.Post("/certificates", a.handleCreateCertificate)
	r.Delete("/certificates/{cn}", a.handleRevokeCertificate)
	r.Get("/ca/truststore", a.handleTrustStore)
	r.Post("/datapackage", a.handleDataPackage)
	r.Post("/enrollment/qrcode", a.handleEnrollmentQRCode)
	r.Get("/config/tls", a.handleGetTLSConfig)
	r.Put("/config/tls", a.handleUpdateTLSConfig)
}

func (a *ManagementAPI) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListenAndServe starts the management HTTP server.
func (a *ManagementAPI) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", a.cfg.ManagementPort)
	srv := &http.Server{
		Addr:         addr,
		Handler:      a.router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	a.logger.Info("management API listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("management API server: %w", err)
	}
	return nil
}

func (a *ManagementAPI) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats := a.manager.GetStats()

	// Use the global client list (NATS KV-backed) for the count so it reflects
	// all pods, not just the one this request happened to hit.
	globalCount := stats.Connected
	if all, err := a.registry.ListAll(); err == nil && all != nil {
		globalCount = len(all)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"healthy":          true,
		"connectedClients": globalCount,
		"pod":              a.cfg.PodName,
		"uptime":           stats.Uptime,
	})
}

func (a *ManagementAPI) handleListClients(w http.ResponseWriter, r *http.Request) {
	all, err := a.registry.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list clients: "+err.Error())
		return
	}
	if all == nil {
		all = []*clients.ClientInfo{}
	}
	writeJSON(w, http.StatusOK, all)
}

func (a *ManagementAPI) handleKickClient(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if err := a.manager.KickClient(uid); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	a.auditor.LogClientKicked("management-api", uid, "", r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]string{"status": "kicked", "uid": uid})
}

func (a *ManagementAPI) handleListCertificates(w http.ResponseWriter, r *http.Request) {
	creds, err := a.creds.ListCredentials()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list credentials: "+err.Error())
		return
	}
	if creds == nil {
		creds = []CredentialInfo{}
	}
	writeJSON(w, http.StatusOK, creds)
}

func (a *ManagementAPI) handleCreateCertificate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string   `json:"username"`
		Password string   `json:"password"`
		Groups   []string `json:"groups,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	if err := a.creds.CreateCredential(req.Username, req.Password); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create credential: "+err.Error())
		return
	}

	// Create corresponding user in the backend and assign groups
	if a.backendClient != nil {
		userID, err := a.backendClient.CreateUser(req.Username)
		if err != nil {
			a.logger.Warn("failed to create backend user", "username", req.Username, "error", err)
		} else if len(req.Groups) > 0 && userID != "" {
			if err := a.backendClient.AddUserToGroups(userID, req.Groups); err != nil {
				a.logger.Warn("failed to assign groups to user", "username", req.Username, "groups", req.Groups, "error", err)
			}
		}
	}

	a.auditor.LogCertCreated("management-api", req.Username, r.RemoteAddr)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "username": req.Username})
}

func (a *ManagementAPI) handleRevokeCertificate(w http.ResponseWriter, r *http.Request) {
	cn := chi.URLParam(r, "cn")

	if err := a.creds.DeleteCredential(cn); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	a.auditor.LogCertRevoked("management-api", cn, r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked", "cn": cn})
}

func (a *ManagementAPI) handleTrustStore(w http.ResponseWriter, r *http.Request) {
	caCertPEM := a.ca.CACertPEM()
	if caCertPEM == nil {
		writeError(w, http.StatusServiceUnavailable, "CA not initialized")
		return
	}

	p12, err := ca.GenerateTrustStoreP12(caCertPEM, "atakatak")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate trust store: "+err.Error())
		return
	}

	a.auditor.LogTrustStoreDownloaded("management-api", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/x-pkcs12")
	w.Header().Set("Content-Disposition", `attachment; filename="truststore-root.p12"`)
	w.Write(p12)
}

func (a *ManagementAPI) handleDataPackage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hostname string `json:"hostname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Hostname == "" {
		writeError(w, http.StatusBadRequest, "hostname is required")
		return
	}

	caCertPEM := a.ca.CACertPEM()
	if caCertPEM == nil {
		writeError(w, http.StatusServiceUnavailable, "CA not initialized")
		return
	}

	data, err := GenerateDataPackage(req.Hostname, caCertPEM, "atakatak")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate data package: "+err.Error())
		return
	}

	a.auditor.LogDataPackageGenerated("management-api", req.Hostname, r.RemoteAddr)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="CNAK-Connection.zip"`)
	w.Write(data)
}

// TLSConfigResponse represents the current TLS configuration for the gateway.
type TLSConfigResponse struct {
	KeyType              string   `json:"key_type"`
	TLSMinVersion        string   `json:"tls_min_version"`
	TLSCipherSuites      []string `json:"tls_cipher_suites"`
	SupportedCipherNames []string `json:"supported_cipher_names"`
	RestartRequired      bool     `json:"restart_required"`
}

func (a *ManagementAPI) handleGetTLSConfig(w http.ResponseWriter, r *http.Request) {
	keyType := a.cfg.KeyType
	if keyType == "" {
		keyType = "ecdsa"
	}
	minVer := a.cfg.TLSMinVersion
	if minVer == "" {
		minVer = "1.2"
	}

	resp := TLSConfigResponse{
		KeyType:              keyType,
		TLSMinVersion:        minVer,
		TLSCipherSuites:      a.cfg.TLSCipherSuites,
		SupportedCipherNames: server.SupportedCipherSuites(),
		RestartRequired:      false,
	}
	if resp.TLSCipherSuites == nil {
		resp.TLSCipherSuites = []string{}
	}
	writeJSON(w, http.StatusOK, resp)
}

// TLSConfigUpdate is the request body for updating TLS settings.
type TLSConfigUpdate struct {
	KeyType         *string  `json:"key_type,omitempty"`
	TLSMinVersion   *string  `json:"tls_min_version,omitempty"`
	TLSCipherSuites []string `json:"tls_cipher_suites,omitempty"`
}

func (a *ManagementAPI) handleUpdateTLSConfig(w http.ResponseWriter, r *http.Request) {
	var req TLSConfigUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.KeyType != nil {
		kt := *req.KeyType
		if kt != "rsa" && kt != "ecdsa" {
			writeError(w, http.StatusBadRequest, "key_type must be 'rsa' or 'ecdsa'")
			return
		}
		a.cfg.KeyType = kt
	}

	if req.TLSMinVersion != nil {
		v := *req.TLSMinVersion
		switch v {
		case "1.0", "1.1", "1.2", "1.3":
			a.cfg.TLSMinVersion = v
		default:
			writeError(w, http.StatusBadRequest, "tls_min_version must be '1.0', '1.1', '1.2', or '1.3'")
			return
		}
	}

	if req.TLSCipherSuites != nil {
		// Validate all cipher suite names
		for _, name := range req.TLSCipherSuites {
			ids := server.ParseCipherSuites([]string{name})
			if len(ids) == 0 {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown cipher suite: %s", name))
				return
			}
		}
		a.cfg.TLSCipherSuites = req.TLSCipherSuites
	}

	a.logger.Info("TLS configuration updated",
		"key_type", a.cfg.KeyType,
		"tls_min_version", a.cfg.TLSMinVersion,
		"tls_cipher_suites", a.cfg.TLSCipherSuites,
	)

	// Changes take effect on new connections / after restart for cert key type.
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "updated",
		"restart_required": req.KeyType != nil,
		"message":          "TLS settings updated. Key type changes require a restart to regenerate certificates.",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
