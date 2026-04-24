package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"
	"github.com/cnak-us/cnak/pkg/logging"
	"github.com/cnak-us/gateway/audit"
	"github.com/cnak-us/gateway/backend"
	"github.com/cnak-us/gateway/bridge"
	"github.com/cnak-us/gateway/ca"
	"github.com/cnak-us/gateway/clients"
	"github.com/cnak-us/gateway/config"
	"github.com/cnak-us/gateway/management"
	"github.com/cnak-us/gateway/marti"
	"github.com/cnak-us/gateway/metrics"
	"github.com/cnak-us/gateway/server"
)

// Service wraps the TAK Gateway for embedding in the backend binary.
type Service struct {
	cfg     *config.Config
	cancel  context.CancelFunc
	mgmtAPI *management.ManagementAPI
}

// New creates a new gateway service with the given configuration.
func New(cfg *config.Config) *Service {
	return &Service{cfg: cfg}
}

// Start initializes and starts the gateway service using the provided NATS connection.
// The caller owns the NATS connection — this service will not close it.
func (s *Service) Start(ctx context.Context, nc *nats.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	cfg := s.cfg
	logger := slog.Default()

	slog.Info("[embedded] TAK Gateway starting",
		"tls_port", cfg.TLSStreamingPort,
		"https_port", cfg.HTTPSAPIPort,
		"tcp_enabled", cfg.TCPEnabled,
	)

	logging.EnableNATS("tak-gateway", nc)

	// Initialize Certificate Authority
	authority := ca.NewCA()
	if err := authority.LoadOrCreate(cfg.CACertFile, cfg.CAKeyFile, cfg.CAOrganization); err != nil {
		return fmt.Errorf("failed to initialize CA: %w", err)
	}

	// Generate server certificate for TLS streaming
	serverSANs := []string{"tak-gateway", "localhost", "127.0.0.1"}
	if cfg.PodName != "" {
		serverSANs = append(serverSANs, cfg.PodName)
	}
	serverSANs = append(serverSANs, cfg.ServerSANs...)
	keyType := ca.KeyType(cfg.KeyType)
	if keyType != ca.KeyTypeECDSA && keyType != ca.KeyTypeRSA {
		keyType = ca.KeyTypeECDSA
	}
	serverCertPEM, serverKeyPEM, err := authority.GenerateServerCertWithKeyType("tak-gateway", serverSANs, 0, keyType)
	if err != nil {
		return fmt.Errorf("failed to generate server certificate: %w", err)
	}

	// Initialize client registry
	registry, err := clients.NewRegistry(nc, cfg.PodName, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize client registry: %w", err)
	}

	// Initialize client manager
	manager := clients.NewManager(registry, logger)

	// Initialize credential store
	creds, err := management.NewCredentialStore(nc, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize credential store: %w", err)
	}

	// Initialize cert mapping store
	certMappings, err := marti.NewCertMappingStore(nc, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize cert mapping store: %w", err)
	}

	// Initialize backend client
	var backendClient *backend.Client
	if cfg.BackendURL != "" {
		backendClient = backend.NewClient(cfg.BackendURL, cfg.ServiceToken)
	}

	// Initialize audit logger
	auditor := audit.NewAuditor(nc, logger)

	// Initialize NATS bridge
	natsBridge := bridge.NewBridge(nc, logger)

	// Track connected client handlers
	var clientsMu sync.RWMutex
	clientHandlers := make(map[string]*server.ClientHandler)

	natsBridge.SetClientProvider(func() []bridge.ClientWriter {
		clientsMu.RLock()
		defer clientsMu.RUnlock()
		writers := make([]bridge.ClientWriter, 0, len(clientHandlers))
		for _, ch := range clientHandlers {
			writers = append(writers, ch)
		}
		return writers
	})

	// Start NATS relay
	if err := natsBridge.StartRelay(ctx); err != nil {
		return fmt.Errorf("failed to start NATS relay: %w", err)
	}

	// Seed bridge cache from backend
	if backendClient != nil {
		go func() {
			data, err := backendClient.GetAllPoints()
			if err != nil {
				slog.Warn("[embedded] failed to fetch points for cache seed", "error", err)
				return
			}
			var points []*bridge.Point
			if err := json.Unmarshal(data, &points); err != nil {
				slog.Warn("[embedded] failed to unmarshal points for cache seed", "error", err)
				return
			}
			seeded := natsBridge.SeedCache(points)
			slog.Info("[embedded] seeded bridge cache", "points", seeded)
		}()
	}

	// NOTE: No NATS reconnect goroutine — embedded NATS doesn't need it.
	// NOTE: No signal handling — parent process handles signals.

	// registerClient polls for a client's UID then registers in the shared registry.
	registerClient := func(ch *server.ClientHandler, remoteAddr, certCN string) {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.After(30 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-timeout:
				slog.Warn("client did not identify within timeout", "remote", remoteAddr, "cn", certCN)
				return
			case <-ticker.C:
				uid := ch.UID()
				if uid == "" {
					continue
				}
				clientsMu.Lock()
				clientHandlers[uid] = ch
				clientsMu.Unlock()
				info := &clients.ClientInfo{
					UID:             uid,
					Callsign:        ch.Callsign(),
					IP:              remoteAddr,
					CertCN:          certCN,
					PodName:         cfg.PodName,
					ConnectedAt:     time.Now(),
					LastSeen:        time.Now(),
					ProtocolVersion: "xml",
				}
				if err := registry.Register(info); err != nil {
					slog.Warn("failed to register client", "error", err, "uid", uid)
				}
				go natsBridge.DumpCacheTo(ch)
				if certCN != "" && certMappings != nil {
					if username, err := certMappings.LookupUsername(certCN); err == nil && username != "" {
						if backendClient != nil {
							if groups, err := backendClient.GetUserGroups(username); err == nil && len(groups) > 0 {
								ch.SetGroups(groups)
								info.Groups = groups
								registry.Register(info)
							}
						}
					}
				}
				auditor.LogClientConnected(certCN, uid, remoteAddr)
				return
			}
		}
	}

	// handleConnection manages a single TAK client connection lifecycle.
	handleConnection := func(conn net.Conn, certCN string) {
		remoteAddr := conn.RemoteAddr().String()
		ch := server.NewClientHandler(conn, certCN, func(senderUID string, cotXML []byte) {
			start := time.Now()
			metrics.COTMessagesReceived.Inc()
			if err := natsBridge.PublishCoT(cotXML, senderUID); err != nil {
				slog.Warn("failed to publish CoT", "error", err, "uid", senderUID)
			}
			messageGroup := bridge.ExtractGroupFromCoT(cotXML)
			clientsMu.RLock()
			for _, c := range clientHandlers {
				if !bridge.ShouldRelay(messageGroup, c.Groups()) {
					continue
				}
				c.Send(cotXML)
			}
			clientsMu.RUnlock()
			if senderUID != "" {
				registry.UpdateLastSeen(senderUID)
			}
			metrics.MessageLatency.Observe(time.Since(start).Seconds())
		})
		metrics.ConnectionsTotal.Inc()
		metrics.ConnectedClients.Inc()
		go registerClient(ch, remoteAddr, certCN)
		ch.Run(ctx)
		metrics.ConnectedClients.Dec()
		uid := ch.UID()
		if uid != "" {
			clientsMu.Lock()
			delete(clientHandlers, uid)
			clientsMu.Unlock()
			registry.Deregister(uid)
			auditor.LogClientDisconnected(certCN, uid, "connection closed")
		}
	}

	// Start TLS streaming server
	tlsServer, err := server.NewTLSServer(cfg, authority.CACertPEM(), serverCertPEM, serverKeyPEM)
	if err != nil {
		return fmt.Errorf("failed to create TLS server: %w", err)
	}
	go func() {
		if err := tlsServer.ListenAndServe(ctx, handleConnection); err != nil {
			slog.Error("[embedded] TLS server error", "error", err)
		}
	}()

	// Start TCP streaming server if enabled
	if cfg.TCPEnabled {
		tcpServer := server.NewTCPServer(cfg)
		go func() {
			if err := tcpServer.ListenAndServe(ctx, handleConnection); err != nil {
				slog.Error("[embedded] TCP server error", "error", err)
			}
		}()
	}

	// Start Marti HTTPS API
	martiAPI := marti.NewMartiAPI(cfg, authority, creds, registry, auditor, certMappings, logger)
	go func() {
		if err := martiAPI.ListenAndServeTLS(ctx); err != nil {
			slog.Error("[embedded] Marti API error", "error", err)
		}
	}()

	// Create management API (but don't start its HTTP server — routes will be mounted on parent router)
	mgmtAPI := management.NewManagementAPI(authority, registry, manager, creds, auditor, cfg, logger, backendClient)
	s.mgmtAPI = mgmtAPI

	// Handle kick requests
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case uid := <-manager.KickChan():
				clientsMu.RLock()
				ch, ok := clientHandlers[uid]
				clientsMu.RUnlock()
				if ok {
					slog.Info("kicking client", "uid", uid)
					ch.Close()
				}
			}
		}
	}()

	slog.Info("[embedded] TAK Gateway ready",
		"tls", cfg.TLSStreamingPort,
		"https", cfg.HTTPSAPIPort,
		"tcp_enabled", cfg.TCPEnabled,
	)

	return nil
}

// Stop gracefully shuts down the gateway service.
// Does NOT close or drain the NATS connection — the caller owns it.
func (s *Service) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	// Give servers time to drain
	time.Sleep(2 * time.Second)
	slog.Info("[embedded] TAK Gateway stopped")
	return nil
}

// Routes returns a chi.Router with the gateway management API routes,
// suitable for mounting on the backend's router.
func (s *Service) Routes() chi.Router {
	if s.mgmtAPI == nil {
		return chi.NewRouter()
	}
	return s.mgmtAPI.GatewayRoutes()
}
