package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/cnak-us/gateway/config"
)

// TCPServer listens for plain TCP connections (no TLS). Intended for testing only.
type TCPServer struct {
	listener  net.Listener
	cfg       *config.Config
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// NewTCPServer creates a plain TCP server.
func NewTCPServer(cfg *config.Config) *TCPServer {
	return &TCPServer{cfg: cfg}
}

// ListenAndServe starts accepting TCP connections on the configured TCP streaming port.
// The certCN argument to the handler will always be empty since there is no TLS.
func (s *TCPServer) ListenAndServe(ctx context.Context, handler func(conn net.Conn, certCN string)) error {
	addr := fmt.Sprintf(":%d", s.cfg.TCPStreamingPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("TCP listen on %s: %w", addr, err)
	}
	s.listener = ln

	slog.Info("TCP streaming server listening", "addr", addr)

	go func() {
		<-ctx.Done()
		s.Shutdown()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				slog.Error("TCP accept error", "error", err)
				continue
			}
		}

		slog.Info("TCP client connected", "remote", conn.RemoteAddr())

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			handler(conn, "")
		}()
	}
}

// Shutdown closes the listener and waits for active connections to finish.
func (s *TCPServer) Shutdown() {
	s.closeOnce.Do(func() {
		if s.listener != nil {
			s.listener.Close()
		}
	})
	s.wg.Wait()
}
