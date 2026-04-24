package server

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/cnak-us/gateway/protocol"
)

// pipeConn wraps a net.Conn from net.Pipe with a RemoteAddr for logging.
type pipeConn struct {
	net.Conn
}

func (c *pipeConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9999}
}

func (c *pipeConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8089}
}

// readAllAvailable reads from r until it blocks or gets enough data.
func readAllAvailable(r io.Reader, timeout time.Duration) ([]byte, error) {
	buf := make([]byte, 8192)
	done := make(chan struct{})
	var n int
	var err error
	go func() {
		n, err = r.Read(buf)
		close(done)
	}()
	select {
	case <-done:
		return buf[:n], err
	case <-time.After(timeout):
		return nil, nil
	}
}

func TestNegotiationFlow_ProtobufSwitch(t *testing.T) {
	// Create a pipe: server side and client side
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	var receivedMessages [][]byte
	var mu sync.Mutex

	ch := NewClientHandler(&pipeConn{serverConn}, "test-cn", func(uid string, data []byte) {
		mu.Lock()
		receivedMessages = append(receivedMessages, append([]byte(nil), data...))
		mu.Unlock()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run the client handler in the background
	done := make(chan struct{})
	go func() {
		ch.Run(ctx)
		close(done)
	}()

	// Step 1: Read the version announcement from the server
	announcement := make([]byte, 4096)
	n, err := clientConn.Read(announcement)
	if err != nil {
		t.Fatalf("failed to read announcement: %v", err)
	}
	announcement = announcement[:n]

	if !bytes.Contains(announcement, []byte(`type="t-x-takp-v"`)) {
		t.Fatalf("expected version announcement, got: %s", announcement)
	}
	if !bytes.Contains(announcement, []byte(`<TakProtocolSupport version="1"/>`)) {
		t.Fatalf("announcement missing protocol version support: %s", announcement)
	}

	// Step 2: Send a protocol request for version 1
	request := []byte(`<event version="2.0" uid="client-test" type="t-x-takp-q" how="h-e" ` +
		`time="2025-01-01T00:00:00Z" start="2025-01-01T00:00:00Z" stale="2025-01-01T01:00:00Z">` +
		`<point lat="0" lon="0" hae="0" ce="999999" le="999999"/>` +
		`<detail><TakControl><TakProtocolSupport version="1"/></TakControl></detail>` +
		`</event>`)
	if _, err := clientConn.Write(request); err != nil {
		t.Fatalf("failed to write protocol request: %v", err)
	}

	// Step 3: Read the protocol response from the server
	response := make([]byte, 4096)
	n, err = clientConn.Read(response)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	response = response[:n]

	if !bytes.Contains(response, []byte(`type="t-x-takp-r"`)) {
		t.Fatalf("expected protocol response, got: %s", response)
	}
	if !bytes.Contains(response, []byte(`status="true"`)) {
		t.Fatalf("expected status=true in response: %s", response)
	}

	// Verify the handler switched to protobuf
	if ch.Proto() != protocol.ProtocolProtobuf {
		t.Errorf("expected protobuf protocol, got %q", ch.Proto())
	}
	if !ch.Negotiated() {
		t.Error("expected negotiated=true")
	}

	// Clean up
	cancel()
	clientConn.Close()
	<-done
}

func TestNegotiationFlow_ClientSkipsNegotiation(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	var receivedMessages [][]byte
	var mu sync.Mutex

	ch := NewClientHandler(&pipeConn{serverConn}, "test-cn", func(uid string, data []byte) {
		mu.Lock()
		receivedMessages = append(receivedMessages, append([]byte(nil), data...))
		mu.Unlock()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		ch.Run(ctx)
		close(done)
	}()

	// Read the version announcement
	announcement := make([]byte, 4096)
	n, err := clientConn.Read(announcement)
	if err != nil {
		t.Fatalf("failed to read announcement: %v", err)
	}
	announcement = announcement[:n]

	if !bytes.Contains(announcement, []byte(`type="t-x-takp-v"`)) {
		t.Fatalf("expected version announcement, got: %s", announcement)
	}

	// Instead of negotiating, send a regular SA event
	saEvent := []byte(`<event version="2.0" uid="my-device" type="a-f-G-U-C" how="m-g" ` +
		`time="2025-01-01T00:00:00Z" start="2025-01-01T00:00:00Z" stale="2025-01-01T01:00:00Z">` +
		`<point lat="38.0" lon="-77.0" hae="0" ce="10" le="10"/>` +
		`<detail><contact callsign="ALPHA-1"/></detail>` +
		`</event>`)
	if _, err := clientConn.Write(saEvent); err != nil {
		t.Fatalf("failed to write SA event: %v", err)
	}

	// Give the handler time to process
	time.Sleep(200 * time.Millisecond)

	// The handler should have auto-detected XML and processed the event
	if ch.Proto() != protocol.ProtocolXML {
		t.Errorf("expected XML protocol, got %q", ch.Proto())
	}
	if ch.Negotiated() {
		t.Error("expected negotiated=false when client skips negotiation")
	}

	mu.Lock()
	if len(receivedMessages) == 0 {
		t.Error("expected the SA event to be processed")
	} else if !bytes.Contains(receivedMessages[0], []byte("my-device")) {
		t.Error("first processed message should be the SA event")
	}
	mu.Unlock()

	if ch.UID() != "my-device" {
		t.Errorf("expected UID 'my-device', got %q", ch.UID())
	}

	// Clean up
	cancel()
	clientConn.Close()
	<-done
}

func TestNegotiationFlow_NegotiateDisabled(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	ch := NewClientHandler(&pipeConn{serverConn}, "test-cn", func(uid string, data []byte) {})
	ch.SetNegotiate(false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		ch.Run(ctx)
		close(done)
	}()

	// With negotiation disabled, the handler should go straight to auto-detect.
	// Send an XML event — no announcement should be sent first.
	saEvent := []byte(`<event version="2.0" uid="no-negotiate" type="a-f-G" how="m-g" ` +
		`time="2025-01-01T00:00:00Z" start="2025-01-01T00:00:00Z" stale="2025-01-01T01:00:00Z">` +
		`<point lat="0" lon="0" hae="0" ce="10" le="10"/>` +
		`<detail><contact callsign="BRAVO"/></detail>` +
		`</event>`)
	if _, err := clientConn.Write(saEvent); err != nil {
		t.Fatalf("failed to write SA event: %v", err)
	}

	// Give the handler time to process
	time.Sleep(200 * time.Millisecond)

	if ch.Proto() != protocol.ProtocolXML {
		t.Errorf("expected XML protocol, got %q", ch.Proto())
	}
	if ch.Negotiated() {
		t.Error("expected negotiated=false when negotiation is disabled")
	}

	cancel()
	clientConn.Close()
	<-done
}

func TestNegotiationFlow_UnsupportedVersion(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	ch := NewClientHandler(&pipeConn{serverConn}, "test-cn", func(uid string, data []byte) {})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		ch.Run(ctx)
		close(done)
	}()

	// Read the version announcement
	announcement := make([]byte, 4096)
	n, err := clientConn.Read(announcement)
	if err != nil {
		t.Fatalf("failed to read announcement: %v", err)
	}
	if !bytes.Contains(announcement[:n], []byte(`type="t-x-takp-v"`)) {
		t.Fatalf("expected version announcement")
	}

	// Request an unsupported version
	request := []byte(`<event version="2.0" uid="client-test" type="t-x-takp-q" how="h-e" ` +
		`time="2025-01-01T00:00:00Z" start="2025-01-01T00:00:00Z" stale="2025-01-01T01:00:00Z">` +
		`<point lat="0" lon="0" hae="0" ce="999999" le="999999"/>` +
		`<detail><TakControl><TakProtocolSupport version="99"/></TakControl></detail>` +
		`</event>`)
	if _, err := clientConn.Write(request); err != nil {
		t.Fatalf("failed to write request: %v", err)
	}

	// Read the response
	response := make([]byte, 4096)
	n, err = clientConn.Read(response)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	response = response[:n]

	if !bytes.Contains(response, []byte(`status="false"`)) {
		t.Fatalf("expected status=false for unsupported version: %s", response)
	}

	// Should stay on XML when version is unsupported
	if ch.Proto() != protocol.ProtocolXML {
		t.Errorf("expected XML protocol for unsupported version, got %q", ch.Proto())
	}
	if !ch.Negotiated() {
		t.Error("expected negotiated=true (handshake completed, just version unsupported)")
	}

	cancel()
	clientConn.Close()
	<-done
}
