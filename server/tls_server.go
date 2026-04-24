package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/cnak-us/gateway/config"
)

// TLSServer listens for TLS TCP connections with mutual TLS (client cert) authentication.
// It also supports auto-detecting plain TCP connections (for TAK clients that connect
// without TLS) and routing them to the same handler.
type TLSServer struct {
	listener  net.Listener
	cfg       *config.Config
	tlsCfg    *tls.Config
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// NewTLSServer creates a TLS server configured for mutual TLS authentication.
// caCertPEM is used to verify client certificates, serverCertPEM/serverKeyPEM are the server identity.
func NewTLSServer(cfg *config.Config, caCertPEM []byte, serverCertPEM, serverKeyPEM []byte) (*TLSServer, error) {
	serverCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("loading server certificate: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}
	ApplyTLSConfig(tlsCfg, cfg)

	return &TLSServer{
		cfg:    cfg,
		tlsCfg: tlsCfg,
	}, nil
}

// ListenAndServe starts accepting connections on the configured streaming port.
// It auto-detects whether each connection is TLS or plain TCP by peeking at the
// first byte (0x16 = TLS handshake record). TLS connections get full mTLS;
// plain TCP connections are handled without encryption.
func (s *TLSServer) ListenAndServe(ctx context.Context, handler func(conn net.Conn, certCN string)) error {
	addr := fmt.Sprintf(":%d", s.cfg.TLSStreamingPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	s.listener = ln

	slog.Info("TLS streaming server listening", "addr", addr)

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
				slog.Error("accept error", "error", err)
				continue
			}
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(ctx, conn, handler)
		}()
	}
}

// handleConn peeks at the first byte to determine if the connection is TLS or plain TCP.
func (s *TLSServer) handleConn(ctx context.Context, conn net.Conn, handler func(net.Conn, string)) {
	br := bufio.NewReader(conn)

	// Peek at first byte to detect TLS vs plain TCP.
	// TLS records start with content type 0x16 (Handshake).
	first, err := br.Peek(1)
	if err != nil {
		slog.Debug("connection peek failed", "remote", conn.RemoteAddr(), "error", err)
		conn.Close()
		return
	}

	if first[0] == 0x16 {
		// TLS handshake - wrap connection and do mTLS
		s.handleTLS(ctx, conn, br, handler)
	} else {
		// Not TLS - log what's being sent for debugging and handle as plain TCP
		preview, _ := br.Peek(min(br.Buffered()+1, 64))
		slog.Warn("non-TLS connection on streaming port, treating as plain TCP",
			"remote", conn.RemoteAddr(),
			"first_bytes_hex", hex.EncodeToString(preview),
			"first_bytes_ascii", sanitizeASCII(preview),
		)
		// Wrap the buffered reader so the peeked bytes aren't lost
		wrappedConn := &bufferedConn{Conn: conn, br: br}
		handler(wrappedConn, "")
	}
}

// handleTLS performs the TLS handshake with mTLS and invokes the handler.
func (s *TLSServer) handleTLS(ctx context.Context, conn net.Conn, br *bufio.Reader, handler func(net.Conn, string)) {
	// Wrap the raw conn + buffered reader into a TLS server connection
	wrappedConn := &bufferedConn{Conn: conn, br: br}
	tlsConn := tls.Server(wrappedConn, s.tlsCfg)

	if err := tlsConn.HandshakeContext(ctx); err != nil {
		slog.Warn("TLS handshake failed", "remote", conn.RemoteAddr(), "error", err)
		conn.Close()
		return
	}

	certCN := ""
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) > 0 {
		certCN = state.PeerCertificates[0].Subject.CommonName
	}

	slog.Info("TLS client connected", "remote", conn.RemoteAddr(), "cn", certCN)

	handler(tlsConn, certCN)
}

// Shutdown closes the listener and waits for active connections to finish.
func (s *TLSServer) Shutdown() {
	s.closeOnce.Do(func() {
		if s.listener != nil {
			s.listener.Close()
		}
	})
	s.wg.Wait()
}

// bufferedConn wraps a net.Conn with a bufio.Reader so that peeked bytes
// are returned on the next Read call instead of being discarded.
type bufferedConn struct {
	net.Conn
	br *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) {
	return c.br.Read(b)
}

// sanitizeASCII returns a string with non-printable bytes replaced by dots.
func sanitizeASCII(data []byte) string {
	out := make([]byte, len(data))
	for i, b := range data {
		if b >= 0x20 && b < 0x7f {
			out[i] = b
		} else {
			out[i] = '.'
		}
	}
	return string(out)
}
