package server

import (
	"bytes"
	"net"
	"testing"
	"time"
)

// mockConn implements net.Conn for testing.
type mockConn struct {
	net.Conn
}

func (c *mockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

func (c *mockConn) Close() error { return nil }

func (c *mockConn) SetDeadline(t time.Time) error      { return nil }
func (c *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *mockConn) SetWriteDeadline(t time.Time) error { return nil }
func (c *mockConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8089}
}
func (c *mockConn) Read(b []byte) (int, error)  { return 0, nil }
func (c *mockConn) Write(b []byte) (int, error) { return len(b), nil }

func TestExtractAttr(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		element  string
		attr     string
		expected string
	}{
		{
			name:     "event uid",
			data:     `<event version="2.0" uid="test-uid" type="a-f-G"><point/></event>`,
			element:  "event",
			attr:     "uid",
			expected: "test-uid",
		},
		{
			name:     "event type",
			data:     `<event version="2.0" uid="test-uid" type="a-f-G"><point/></event>`,
			element:  "event",
			attr:     "type",
			expected: "a-f-G",
		},
		{
			name:     "contact callsign",
			data:     `<event uid="1"><detail><contact callsign="ALPHA-1"/></detail></event>`,
			element:  "contact",
			attr:     "callsign",
			expected: "ALPHA-1",
		},
		{
			name:     "nonexistent element",
			data:     `<event uid="1"><point/></event>`,
			element:  "contact",
			attr:     "callsign",
			expected: "",
		},
		{
			name:     "nonexistent attribute",
			data:     `<event uid="1"><point/></event>`,
			element:  "event",
			attr:     "nonexistent",
			expected: "",
		},
		{
			name:     "empty input",
			data:     "",
			element:  "event",
			attr:     "uid",
			expected: "",
		},
		{
			name:     "malformed XML",
			data:     "not xml at all",
			element:  "event",
			attr:     "uid",
			expected: "",
		},
		{
			name:     "nested element",
			data:     `<event uid="1"><detail><track course="90.0" speed="5.5"/></detail></event>`,
			element:  "track",
			attr:     "course",
			expected: "90.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAttr([]byte(tt.data), tt.element, tt.attr)
			if got != tt.expected {
				t.Errorf("extractAttr(%q, %q) = %q, want %q", tt.element, tt.attr, got, tt.expected)
			}
		})
	}
}

func TestClientHandlerProcessEvent(t *testing.T) {
	t.Run("normal event", func(t *testing.T) {
		var receivedUID string
		var receivedData []byte

		ch := &ClientHandler{
			conn: &mockConn{},
			onMessage: func(uid string, data []byte) {
				receivedUID = uid
				receivedData = data
			},
			done: make(chan struct{}),
		}

		event := []byte(`<event uid="test-uid" type="a-f-G"><point/><detail><contact callsign="ALPHA"/></detail></event>`)
		ch.processEvent(event)

		if ch.uid != "test-uid" {
			t.Errorf("expected uid 'test-uid', got %q", ch.uid)
		}
		if ch.callsign != "ALPHA" {
			t.Errorf("expected callsign 'ALPHA', got %q", ch.callsign)
		}
		if receivedUID != "test-uid" {
			t.Errorf("callback received uid %q", receivedUID)
		}
		if !bytes.Contains(receivedData, []byte("test-uid")) {
			t.Error("callback should receive the event data")
		}
	})

	t.Run("event with leading auth element", func(t *testing.T) {
		var receivedData []byte

		ch := &ClientHandler{
			conn: &mockConn{},
			onMessage: func(uid string, data []byte) {
				receivedData = data
			},
			done: make(chan struct{}),
		}

		// ATAK sends <auth> before the first <event>
		data := []byte(`<auth username="admin"/><event uid="uid-1" type="a-f-G"><point/></event>`)
		ch.processEvent(data)

		if ch.uid != "uid-1" {
			t.Errorf("expected uid 'uid-1', got %q", ch.uid)
		}
		// Received data should start with <event, not <auth
		if !bytes.HasPrefix(receivedData, []byte("<event")) {
			t.Error("leading non-event XML should be stripped")
		}
	})

	t.Run("no event element", func(t *testing.T) {
		callbackCalled := false
		ch := &ClientHandler{
			conn: &mockConn{},
			onMessage: func(uid string, data []byte) {
				callbackCalled = true
			},
			done: make(chan struct{}),
		}

		ch.processEvent([]byte(`<auth username="admin"/>`))
		if callbackCalled {
			t.Error("callback should not be called for non-event data")
		}
	})

	t.Run("nil onMessage callback", func(t *testing.T) {
		ch := &ClientHandler{
			conn: &mockConn{},
			done: make(chan struct{}),
		}
		// Should not panic
		ch.processEvent([]byte(`<event uid="test" type="a"><point/></event>`))
	})

	t.Run("uid set only once", func(t *testing.T) {
		ch := &ClientHandler{
			conn:      &mockConn{},
			onMessage: func(string, []byte) {},
			done:      make(chan struct{}),
		}

		ch.processEvent([]byte(`<event uid="first" type="a"><point/></event>`))
		ch.processEvent([]byte(`<event uid="second" type="a"><point/></event>`))

		if ch.uid != "first" {
			t.Errorf("uid should be set from first event only, got %q", ch.uid)
		}
	})

	t.Run("callsign set only once", func(t *testing.T) {
		ch := &ClientHandler{
			conn:      &mockConn{},
			onMessage: func(string, []byte) {},
			done:      make(chan struct{}),
		}

		ch.processEvent([]byte(`<event uid="1" type="a"><detail><contact callsign="FIRST"/></detail></event>`))
		ch.processEvent([]byte(`<event uid="1" type="a"><detail><contact callsign="SECOND"/></detail></event>`))

		if ch.callsign != "FIRST" {
			t.Errorf("callsign should be set from first event only, got %q", ch.callsign)
		}
	})
}

func TestClientHandlerSend(t *testing.T) {
	ch := &ClientHandler{
		conn:    &mockConn{},
		writeCh: make(chan []byte, writeChanSize),
		done:    make(chan struct{}),
	}

	t.Run("send message", func(t *testing.T) {
		ch.Send([]byte("test data"))

		select {
		case data := <-ch.writeCh:
			if string(data) != "test data" {
				t.Errorf("expected 'test data', got %q", data)
			}
		default:
			t.Error("no message on write channel")
		}
	})

	t.Run("send when channel full", func(t *testing.T) {
		// Fill the channel
		for i := 0; i < writeChanSize; i++ {
			ch.Send([]byte("fill"))
		}

		// This should be dropped, not block
		ch.Send([]byte("overflow"))

		// Verify we have writeChanSize messages, not writeChanSize+1
		count := len(ch.writeCh)
		if count != writeChanSize {
			t.Errorf("expected %d messages, got %d", writeChanSize, count)
		}
	})
}

func TestClientHandlerAccessors(t *testing.T) {
	ch := &ClientHandler{
		uid:      "test-uid",
		callsign: "ALPHA",
		certCN:   "test-cn",
		groups:   []string{"ALPHA", "BRAVO"},
		done:     make(chan struct{}),
	}

	if ch.UID() != "test-uid" {
		t.Errorf("expected UID 'test-uid', got %q", ch.UID())
	}
	if ch.Callsign() != "ALPHA" {
		t.Errorf("expected Callsign 'ALPHA', got %q", ch.Callsign())
	}
	if ch.CertCN() != "test-cn" {
		t.Errorf("expected CertCN 'test-cn', got %q", ch.CertCN())
	}
	if len(ch.Groups()) != 2 {
		t.Errorf("expected 2 groups, got %d", len(ch.Groups()))
	}
}

func TestClientHandlerSetGroups(t *testing.T) {
	ch := &ClientHandler{done: make(chan struct{})}

	ch.SetGroups([]string{"CHARLIE"})
	if len(ch.Groups()) != 1 || ch.Groups()[0] != "CHARLIE" {
		t.Errorf("expected [CHARLIE], got %v", ch.Groups())
	}

	ch.SetGroups(nil)
	if ch.Groups() != nil {
		t.Errorf("expected nil groups, got %v", ch.Groups())
	}
}

func TestSanitizeASCII(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "printable ASCII",
			input:    []byte("Hello World!"),
			expected: "Hello World!",
		},
		{
			name:     "with control chars",
			input:    []byte{0x00, 0x41, 0x01, 0x42, 0x1F},
			expected: ".A.B.",
		},
		{
			name:     "with high bytes",
			input:    []byte{0x7F, 0x80, 0xFF},
			expected: "...",
		},
		{
			name:     "empty",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "all printable range",
			input:    []byte{0x20, 0x7E}, // space and ~
			expected: " ~",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeASCII(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeASCII = %q, want %q", got, tt.expected)
			}
		})
	}
}
