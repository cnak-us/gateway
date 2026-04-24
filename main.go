// TAK Gateway Service
//
// Provides a TAK-compatible server for ATAK/WinTAK/iTAK clients.
// Handles TLS client certificate enrollment, streaming connections,
// and bridges CoT messages to/from NATS.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

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

var (
	// natsReconnect signals the manual reconnect goroutine when the NATS
	// connection is permanently closed (e.g., after a server restart or auth change).
	natsReconnect = make(chan struct{}, 1)

	// activeNC holds the current NATS connection so the shutdown path can drain
	// whichever connection is live (initial or a replacement from manual reconnect).
	activeNC atomic.Pointer[nats.Conn]
)

func main() {
	// Set up structured logging via shared pkg
	logging.Setup("tak-gateway")
	var logger *slog.Logger

	slog.Info("=== TAK Gateway Service Starting ===")

	// Load configuration
	cfg := config.LoadFromEnv()

	slog.Info("configuration loaded",
		"tls_port", cfg.TLSStreamingPort,
		"https_port", cfg.HTTPSAPIPort,
		"tcp_port", cfg.TCPStreamingPort,
		"tcp_enabled", cfg.TCPEnabled,
		"mgmt_port", cfg.ManagementPort,
		"nats_url", cfg.NATSURL,
		"pod", cfg.PodName,
	)

	// Connect to NATS with retry
	slog.Info("connecting to NATS", "url", cfg.NATSURL)
	natsOpts := []nats.Option{
		nats.Name("tak-gateway"),
		nats.MaxReconnects(-1),              // unlimited built-in reconnects
		nats.ReconnectWait(2 * time.Second), // wait between reconnect attempts
		nats.DisconnectErrHandler(func(c *nats.Conn, err error) {
			slog.Warn("NATS disconnected",
				"error", err,
				"status", c.Status(),
				"last_error", c.LastError(),
			)
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			slog.Info("NATS reconnected")
		}),
		nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, err error) {
			subj := ""
			if sub != nil {
				subj = sub.Subject
			}
			slog.Error("NATS async error", "error", err, "subject", subj)
		}),
		nats.ClosedHandler(func(c *nats.Conn) {
			// Connection permanently closed — built-in reconnect gave up or
			// the server rejected us. Signal the manual reconnect goroutine
			// to create a brand-new connection.
			slog.Warn("NATS connection closed permanently, scheduling reconnection",
				"last_error", c.LastError(),
			)
			select {
			case natsReconnect <- struct{}{}:
			default:
			}
		}),
	}
	// Auth priority: creds file (JWT mode) → token → URL credentials (implicit)
	if cfg.NATSCredsFile != "" {
		natsOpts = append(natsOpts, nats.UserCredentials(cfg.NATSCredsFile))
		slog.Info("NATS auth: credentials file", "file", cfg.NATSCredsFile)
	} else if cfg.NATSAuthToken != "" {
		natsOpts = append(natsOpts, nats.Token(cfg.NATSAuthToken))
		slog.Info("NATS auth: token")
	}

	var nc *nats.Conn
	for i := 0; i < 30; i++ {
		var err error
		nc, err = nats.Connect(cfg.NATSURL, natsOpts...)
		if err == nil {
			break
		}
		slog.Warn("failed to connect to NATS, retrying in 5s", "error", err, "attempt", i+1)
		time.Sleep(5 * time.Second)
	}
	if nc == nil {
		slog.Error("failed to connect to NATS after retries", "url", cfg.NATSURL)
		os.Exit(1)
	}
	defer nc.Close()
	activeNC.Store(nc)
	slog.Info("connected to NATS", "url", cfg.NATSURL)

	logging.EnableNATS("tak-gateway", nc)
	logger = slog.Default() // Use NATS-enabled handler for all component loggers

	// Initialize Certificate Authority
	authority := ca.NewCA()
	if err := authority.LoadOrCreate(cfg.CACertFile, cfg.CAKeyFile, cfg.CAOrganization); err != nil {
		slog.Error("failed to initialize CA", "error", err)
		os.Exit(1)
	}
	slog.Info("certificate authority initialized",
		"cert", cfg.CACertFile,
		"org", cfg.CAOrganization,
	)

	// Generate server certificate for TLS streaming.
	// SANs must include every hostname/IP that TAK clients use to connect.
	serverSANs := []string{"tak-gateway", "localhost", "10.0.0.127", "73.34.184.79"}
	if cfg.PodName != "" {
		serverSANs = append(serverSANs, cfg.PodName)
	}
	serverSANs = append(serverSANs, cfg.ServerSANs...)
	keyType := ca.KeyType(cfg.KeyType)
	if keyType != ca.KeyTypeECDSA && keyType != ca.KeyTypeRSA {
		keyType = ca.KeyTypeECDSA
	}
	serverCertPEM, serverKeyPEM, err := authority.GenerateServerCertWithKeyType(
		"tak-gateway",
		serverSANs,
		0,
		keyType,
	)
	if err != nil {
		slog.Error("failed to generate server certificate", "error", err)
		os.Exit(1)
	}
	slog.Info("server certificate generated for TLS streaming")

	// Initialize client registry (NATS KV backed, falls back to in-memory)
	registry, err := clients.NewRegistry(nc, cfg.PodName, logger)
	if err != nil {
		slog.Error("failed to initialize client registry", "error", err)
		os.Exit(1)
	}

	// Initialize client manager
	manager := clients.NewManager(registry, logger)

	// Initialize credential store (NATS KV backed, falls back to in-memory)
	creds, err := management.NewCredentialStore(nc, logger)
	if err != nil {
		slog.Error("failed to initialize credential store", "error", err)
		os.Exit(1)
	}

	// Initialize cert mapping store (NATS KV backed, falls back to in-memory)
	certMappings, err := marti.NewCertMappingStore(nc, logger)
	if err != nil {
		slog.Error("failed to initialize cert mapping store", "error", err)
		os.Exit(1)
	}

	// Initialize backend client for user/group lookups
	var backendClient *backend.Client
	if cfg.BackendURL != "" {
		backendClient = backend.NewClient(cfg.BackendURL, cfg.ServiceToken)
		slog.Info("backend client configured", "url", cfg.BackendURL)
	}

	// Initialize audit logger
	auditor := audit.NewAuditor(nc, logger)

	// Parse store-and-forward config
	replayCfg := bridge.ReplayConfig{
		Enabled: cfg.StoreForwardEnabled,
		Window:  bridge.ParseReplayWindow(cfg.StoreForwardWindow, 5*time.Minute, logger),
	}
	if replayCfg.Enabled {
		slog.Info("store-and-forward replay enabled", "window", replayCfg.Window)
	}

	// Initialize NATS bridge
	natsBridge := bridge.NewBridge(nc, logger)

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Track connected client handlers for bridge relay and kick management
	var clientsMu sync.RWMutex
	clientHandlers := make(map[string]*server.ClientHandler)

	// Provide the bridge with a way to get connected clients for relay
	natsBridge.SetClientProvider(func() []bridge.ClientWriter {
		clientsMu.RLock()
		defer clientsMu.RUnlock()
		writers := make([]bridge.ClientWriter, 0, len(clientHandlers))
		for _, ch := range clientHandlers {
			writers = append(writers, ch)
		}
		return writers
	})

	// Start NATS relay (subscribes to tracks.broadcast and tracks.*.update)
	if err := natsBridge.StartRelay(ctx); err != nil {
		slog.Error("failed to start NATS relay", "error", err)
		os.Exit(1)
	}
	slog.Info("NATS relay started")

	// Seed bridge cache from backend API so existing tracks are
	// available for new TAK clients immediately.
	if backendClient != nil {
		go func() {
			data, err := backendClient.GetAllPoints()
			if err != nil {
				slog.Warn("failed to fetch points from backend for cache seed", "error", err)
				return
			}
			var points []*bridge.Point
			if err := json.Unmarshal(data, &points); err != nil {
				slog.Warn("failed to unmarshal backend points for cache seed", "error", err)
				return
			}
			seeded := natsBridge.SeedCache(points)
			slog.Info("seeded bridge cache from backend", "points", seeded)
		}()
	}

	// Monitor NATS connection and create a new one if permanently closed.
	// This handles cases where the backend restarts NATS or auth changes occur.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-natsReconnect:
				for {
					select {
					case <-ctx.Done():
						return
					case <-time.After(5 * time.Second):
					}
					slog.Info("attempting to create new NATS connection...")
					newNC, err := nats.Connect(cfg.NATSURL, natsOpts...)
					if err != nil {
						slog.Warn("NATS reconnection failed, retrying in 5s", "error", err)
						continue
					}
					slog.Info("new NATS connection established")
					activeNC.Store(newNC)
					logging.EnableNATS("tak-gateway", newNC)
					natsBridge.UpdateConn(newNC)
					if err := natsBridge.StartRelay(ctx); err != nil {
						slog.Error("failed to restart NATS relay", "error", err)
						newNC.Close()
						continue
					}
					slog.Info("NATS relay restarted on new connection")
					break
				}
			}
		}
	}()

	// registerClient polls for a client's UID to be set (extracted from their
	// first CoT SA message) and then registers them in the shared registry.
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
					ProtocolVersion: ch.Proto(),
				}
				if err := registry.Register(info); err != nil {
					slog.Warn("failed to register client", "error", err, "uid", uid)
				}

				// Check for previous disconnect (reconnection scenario)
				disconnectTime, wasConnected := registry.GetDisconnectTime(uid)

				go func() {
					natsBridge.DumpCacheTo(ch)

					// After cache dump, replay missed JetStream messages if this is a reconnect
					if wasConnected && replayCfg.Enabled {
						replayed, err := natsBridge.ReplayMissedMessages(ch, disconnectTime, replayCfg)
						if err != nil {
							slog.Warn("store-and-forward replay failed", "uid", uid, "error", err)
						} else if replayed > 0 {
							slog.Info("store-and-forward replay sent", "uid", uid, "messages", replayed)
						}
						registry.ClearDisconnectTime(uid)
					}
				}()

				// Resolve client groups: certCN -> username -> groups
				if certCN != "" && certMappings != nil {
					if username, err := certMappings.LookupUsername(certCN); err == nil && username != "" {
						if backendClient != nil {
							if groups, err := backendClient.GetUserGroups(username); err == nil && len(groups) > 0 {
								ch.SetGroups(groups)
								info.Groups = groups
								registry.Register(info) // re-register with groups
								slog.Info("client groups resolved", "uid", uid, "username", username, "groups", groups)
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

			// Local fan-out: echo received CoT to all connected clients.
			// TAK Server echoes messages back to all clients including the
			// sender — this doubles as a keepalive (commo disconnects after
			// 25 seconds of server silence).
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

		// Poll for UID in background and register once identified
		go registerClient(ch, remoteAddr, certCN)

		// Run blocks until disconnect
		ch.Run(ctx)

		// Cleanup on disconnect
		metrics.ConnectedClients.Dec()

		uid := ch.UID()
		if uid != "" {
			clientsMu.Lock()
			delete(clientHandlers, uid)
			clientsMu.Unlock()

			// Broadcast t-x-d delete event so other clients remove this contact
			deleteXML := bridge.BuildDeleteCoT(uid)

			// Send to all local clients on this pod
			clientsMu.RLock()
			for _, c := range clientHandlers {
				c.Send(deleteXML)
			}
			clientsMu.RUnlock()

			// Publish to NATS so clients on other pods also get the delete
			if err := natsBridge.PublishDisconnect(uid); err != nil {
				slog.Warn("failed to publish disconnect event", "error", err, "uid", uid)
			}

			// Remove the disconnected client from the bridge point cache
			natsBridge.RemoveFromCache(uid)

			registry.Deregister(uid)
			auditor.LogClientDisconnected(certCN, uid, "connection closed")
			slog.Info("client disconnected", "uid", uid, "callsign", ch.Callsign(), "remote", remoteAddr)
		}
	}

	// Start TLS streaming server (port 8089)
	tlsServer, err := server.NewTLSServer(cfg, authority.CACertPEM(), serverCertPEM, serverKeyPEM)
	if err != nil {
		slog.Error("failed to create TLS server", "error", err)
		os.Exit(1)
	}
	go func() {
		if err := tlsServer.ListenAndServe(ctx, handleConnection); err != nil {
			slog.Error("TLS server error", "error", err)
		}
	}()

	// Start TCP streaming server if enabled (port 8088)
	if cfg.TCPEnabled {
		tcpServer := server.NewTCPServer(cfg)
		go func() {
			if err := tcpServer.ListenAndServe(ctx, handleConnection); err != nil {
				slog.Error("TCP server error", "error", err)
			}
		}()
	}

	// Start Marti HTTPS API for certificate enrollment (port 8443)
	martiAPI := marti.NewMartiAPI(cfg, authority, creds, registry, auditor, certMappings, logger)
	go func() {
		if err := martiAPI.ListenAndServeTLS(ctx); err != nil {
			slog.Error("Marti API error", "error", err)
		}
	}()

	// Start Management API with /health, /metrics, and /api/tak-gateway/* (port 8090)
	mgmtAPI := management.NewManagementAPI(authority, registry, manager, creds, auditor, cfg, logger, backendClient)
	mgmtAPI.RegisterDocsHandler(serveOpenAPISpec)
	go func() {
		if err := mgmtAPI.ListenAndServe(ctx); err != nil {
			slog.Error("management API error", "error", err)
		}
	}()

	// Handle kick requests from the management API
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

	slog.Info("=== TAK Gateway Service Ready ===",
		"tls", cfg.TLSStreamingPort,
		"https", cfg.HTTPSAPIPort,
		"mgmt", cfg.ManagementPort,
		"tcp_enabled", cfg.TCPEnabled,
	)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	slog.Info("shutdown signal received, draining connections...")

	// Cancel context to stop all servers
	cancel()

	// Give servers time to drain
	time.Sleep(2 * time.Second)

	if c := activeNC.Load(); c != nil && !c.IsClosed() {
		c.Drain()
	}
	slog.Info("shutdown complete")
}
