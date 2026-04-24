package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/cnak-us/gateway/protocol"
)

const (
	// writeChanSize is the buffer size for outbound messages per client.
	writeChanSize = 64
)

// ClientHandler manages a single TAK client connection.
// It auto-detects the protocol (XML or protobuf), reads inbound CoT events,
// and writes outbound events via a buffered channel.
type ClientHandler struct {
	conn     net.Conn
	certCN   string
	uid      string
	callsign string
	groups   []string

	writeCh   chan []byte
	onMessage func(senderUID string, cotXML []byte)

	done       chan struct{}
	closeOnce  sync.Once
	proto      string // detected protocol: "xml" or "protobuf"
	negotiated bool   // true if protocol was established via TAK negotiation handshake
	negotiate  bool   // true to attempt TAK protocol negotiation before auto-detect
}

// NewClientHandler creates a handler for a connected TAK client.
// onMessage is called for each inbound CoT event with the sender UID and raw XML bytes.
func NewClientHandler(conn net.Conn, certCN string, onMessage func(string, []byte)) *ClientHandler {
	return &ClientHandler{
		conn:      conn,
		certCN:    certCN,
		writeCh:   make(chan []byte, writeChanSize),
		onMessage: onMessage,
		done:      make(chan struct{}),
		negotiate: true,
	}
}

// SetNegotiate controls whether the handler attempts TAK Protocol negotiation
// before falling back to auto-detect. Default is true for TLS connections.
func (ch *ClientHandler) SetNegotiate(v bool) { ch.negotiate = v }

// Negotiated returns true if the protocol was established via the TAK negotiation handshake.
func (ch *ClientHandler) Negotiated() bool { return ch.negotiated }

// Run starts the read and write loops for this client. It blocks until the context
// is cancelled or the connection is closed.
func (ch *ClientHandler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		ch.readLoop(ctx)
	}()

	go func() {
		defer wg.Done()
		ch.writeLoop(ctx)
	}()

	wg.Wait()
	ch.Close()
}

// readLoop optionally performs TAK Protocol negotiation, then detects the protocol
// on the first bytes and reads CoT events in a loop.
func (ch *ClientHandler) readLoop(ctx context.Context) {
	br := bufio.NewReader(ch.conn)

	if ch.negotiate {
		if ch.tryNegotiate(ctx, br) {
			// Negotiation completed — protocol is set, start the appropriate read loop
			switch ch.proto {
			case protocol.ProtocolProtobuf:
				ch.readProtobuf(ctx, br)
			default:
				ch.readXML(ctx, br)
			}
			return
		}
		// Negotiation did not complete (client didn't respond with takp-q) —
		// fall through to auto-detect with the first message already consumed
		// and dispatched in tryNegotiate.
		if ch.proto != "" {
			// proto was set during tryNegotiate (auto-detect path); continue reading
			switch ch.proto {
			case protocol.ProtocolProtobuf:
				ch.readProtobuf(ctx, br)
			default:
				ch.readXML(ctx, br)
			}
			return
		}
	}

	proto, err := protocol.DetectProtocol(br)
	if err != nil {
		slog.Warn("protocol detection failed", "remote", ch.conn.RemoteAddr(), "error", err)
		ch.Close()
		return
	}
	ch.proto = proto
	slog.Info("protocol detected", "remote", ch.conn.RemoteAddr(), "protocol", proto)

	switch proto {
	case protocol.ProtocolXML:
		ch.readXML(ctx, br)
	case protocol.ProtocolProtobuf:
		ch.readProtobuf(ctx, br)
	}
}

// tryNegotiate sends a TAK Protocol version announcement and waits for the client's
// first message. If the client responds with a protocol negotiation request (t-x-takp-q),
// the handshake completes and the function returns true. If the client sends a regular
// message (e.g., an SA event), it is processed normally and the function returns false,
// allowing the caller to continue with auto-detected protocol.
func (ch *ClientHandler) tryNegotiate(ctx context.Context, br *bufio.Reader) bool {
	// Send version announcement (always XML, negotiation hasn't happened yet)
	announcement := protocol.BuildVersionAnnouncement()
	if err := protocol.WriteEvent(ch.conn, announcement); err != nil {
		slog.Warn("failed to send protocol announcement", "remote", ch.conn.RemoteAddr(), "error", err)
		ch.Close()
		return false
	}
	slog.Debug("sent TAK protocol version announcement", "remote", ch.conn.RemoteAddr())

	// Set a deadline so we don't wait forever for the client's response.
	// Some clients may never respond to the announcement and just start sending data.
	ch.conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Peek at the first byte to determine if the client is responding with XML or protobuf
	firstByte, err := br.Peek(1)
	if err != nil {
		// Timeout or error — clear deadline and fall back to auto-detect on next read
		ch.conn.SetReadDeadline(time.Time{})
		slog.Debug("no client response to protocol announcement, falling back to auto-detect",
			"remote", ch.conn.RemoteAddr(), "error", err)
		return false
	}

	// Clear the deadline for the actual read
	ch.conn.SetReadDeadline(time.Time{})

	if firstByte[0] == protocol.MagicByte {
		// Client is speaking protobuf without negotiation — auto-detect path
		ch.proto = protocol.ProtocolProtobuf
		slog.Info("client sent protobuf without negotiation", "remote", ch.conn.RemoteAddr())
		return false
	}

	if firstByte[0] != '<' {
		slog.Warn("unexpected first byte after announcement", "remote", ch.conn.RemoteAddr(),
			"byte", fmt.Sprintf("0x%02X", firstByte[0]))
		ch.Close()
		return false
	}

	// Read the first XML event
	codec := protocol.NewXMLCodec(br)
	data, err := codec.ReadEvent()
	if err != nil {
		slog.Warn("failed to read first event after announcement", "remote", ch.conn.RemoteAddr(), "error", err)
		ch.Close()
		return false
	}

	// Check if it's a negotiation request
	eventType := protocol.IsNegotiationEvent(data)
	if eventType == protocol.TypeTAKPRequest {
		// Client wants to negotiate protocol
		requestedVersion := protocol.ExtractRequestedVersion(data)
		supported := requestedVersion == "1"

		// Set protocol state before writing the response, because the write may
		// block until the peer reads (e.g. net.Pipe) and we want the state to be
		// visible as soon as the response is delivered.
		if supported {
			ch.proto = protocol.ProtocolProtobuf
		} else {
			ch.proto = protocol.ProtocolXML
		}
		ch.negotiated = true

		response := protocol.BuildVersionResponse(requestedVersion, supported)
		if err := protocol.WriteEvent(ch.conn, response); err != nil {
			slog.Warn("failed to send protocol response", "remote", ch.conn.RemoteAddr(), "error", err)
			ch.Close()
			return false
		}

		slog.Info("TAK protocol negotiated", "remote", ch.conn.RemoteAddr(),
			"version", requestedVersion, "protocol", ch.proto)
		return true
	}

	// Not a negotiation message — it's a regular event. Process it and fall back
	// to XML auto-detect since the client sent XML without negotiating.
	ch.proto = protocol.ProtocolXML
	slog.Info("client skipped negotiation, auto-detected XML", "remote", ch.conn.RemoteAddr())
	ch.processEvent(data)
	return false
}

// readXML reads XML CoT events from the stream.
func (ch *ClientHandler) readXML(ctx context.Context, br *bufio.Reader) {
	codec := protocol.NewXMLCodec(br)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch.done:
			return
		default:
		}

		data, err := codec.ReadEvent()
		if err != nil {
			slog.Debug("XML read error", "remote", ch.conn.RemoteAddr(), "error", err)
			return
		}

		ch.processEvent(data)
	}
}

// readProtobuf reads protobuf-framed messages from the stream.
// Note: protobuf payloads are passed through as-is; the caller is responsible
// for any protobuf-to-XML conversion if needed.
func (ch *ClientHandler) readProtobuf(ctx context.Context, br *bufio.Reader) {
	codec := protocol.NewProtobufCodec(br)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch.done:
			return
		default:
		}

		data, err := codec.ReadMessage()
		if err != nil {
			slog.Debug("protobuf read error", "remote", ch.conn.RemoteAddr(), "error", err)
			return
		}

		// Protobuf messages are forwarded as raw bytes; higher-level code
		// handles conversion to CoT XML if needed.
		if ch.onMessage != nil {
			ch.onMessage(ch.uid, data)
		}
	}
}

// processEvent extracts UID and callsign from a raw CoT XML event and invokes the callback.
// Non-event elements (e.g. <auth> sent by ATAK on connect) are silently skipped.
func (ch *ClientHandler) processEvent(data []byte) {
	// The XML codec accumulates bytes until </event>, so non-event elements
	// like <auth> get prepended to the next event. Find the actual <event start.
	eventIdx := bytes.Index(data, []byte("<event"))
	if eventIdx < 0 {
		// No <event> element at all — skip silently
		return
	}
	if eventIdx > 0 {
		// Strip leading non-event XML (e.g. <auth .../>)
		data = data[eventIdx:]
	}

	// Extract UID from event uid attribute
	uid := extractAttr(data, "event", "uid")
	if uid != "" && ch.uid == "" {
		ch.uid = uid
		slog.Info("client identified", "remote", ch.conn.RemoteAddr(), "uid", uid)
	}

	// Extract callsign from <contact callsign="..."/>
	cs := extractAttr(data, "contact", "callsign")
	if cs != "" && ch.callsign == "" {
		ch.callsign = cs
		slog.Info("client callsign", "remote", ch.conn.RemoteAddr(), "callsign", cs)
	}

	if ch.onMessage != nil {
		ch.onMessage(ch.uid, data)
	}
}

// writeLoop sends queued outbound messages to the client connection.
func (ch *ClientHandler) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch.done:
			return
		case data := <-ch.writeCh:
			var err error
			switch ch.proto {
			case protocol.ProtocolProtobuf:
				err = protocol.WriteMessage(ch.conn, data)
			default:
				err = protocol.WriteEvent(ch.conn, data)
			}
			if err != nil {
				slog.Debug("write error", "remote", ch.conn.RemoteAddr(), "error", err)
				return
			}
		}
	}
}

// Send queues a message for delivery to this client. Non-blocking; drops the message
// if the write channel is full.
func (ch *ClientHandler) Send(data []byte) {
	select {
	case ch.writeCh <- data:
	default:
		slog.Warn("write channel full, dropping message", "remote", ch.conn.RemoteAddr())
	}
}

// Close shuts down the client handler and its connection.
func (ch *ClientHandler) Close() {
	ch.closeOnce.Do(func() {
		close(ch.done)
		ch.conn.Close()
	})
}

// UID returns the client's CoT UID (extracted from the first SA message).
func (ch *ClientHandler) UID() string { return ch.uid }

// Callsign returns the client's callsign (extracted from contact element).
func (ch *ClientHandler) Callsign() string { return ch.callsign }

// CertCN returns the client certificate Common Name (empty for TCP connections).
func (ch *ClientHandler) CertCN() string { return ch.certCN }

// Proto returns the detected or negotiated protocol ("xml" or "protobuf").
// Returns "xml" if protocol detection has not yet occurred.
func (ch *ClientHandler) Proto() string {
	if ch.proto == "" {
		return protocol.ProtocolXML
	}
	return ch.proto
}

// Groups returns the client's group memberships.
func (ch *ClientHandler) Groups() []string { return ch.groups }

// SetGroups sets the client's group memberships.
func (ch *ClientHandler) SetGroups(groups []string) { ch.groups = groups }

// extractAttr extracts an XML attribute value from a raw XML byte slice.
// It looks for the given element and attribute name using the standard XML decoder.
func extractAttr(data []byte, element, attr string) string {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := decoder.Token()
		if err != nil {
			return ""
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == element {
			for _, a := range se.Attr {
				if a.Name.Local == attr {
					return a.Value
				}
			}
			return ""
		}
	}
}
