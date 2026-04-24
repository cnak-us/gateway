package clients

import (
	"log/slog"
	"testing"
	"time"
)

func newTestRegistry() *Registry {
	return &Registry{
		local:  make(map[string]*ClientInfo),
		logger: slog.Default(),
	}
}

func TestManagerKickClient(t *testing.T) {
	reg := newTestRegistry()
	mgr := NewManager(reg, slog.Default())

	// Register a client
	reg.Register(&ClientInfo{UID: "client-1", Callsign: "ALPHA"})

	t.Run("kick existing client", func(t *testing.T) {
		err := mgr.KickClient("client-1")
		if err != nil {
			t.Fatalf("KickClient failed: %v", err)
		}

		// Verify kick was sent on channel
		select {
		case uid := <-mgr.KickChan():
			if uid != "client-1" {
				t.Errorf("expected uid 'client-1', got %q", uid)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("no kick received on channel")
		}
	})

	t.Run("kick nonexistent client", func(t *testing.T) {
		err := mgr.KickClient("nobody")
		if err == nil {
			t.Error("expected error for nonexistent client")
		}
	})

	t.Run("kick channel full", func(t *testing.T) {
		// Fill the channel
		for i := 0; i < 16; i++ {
			reg.Register(&ClientInfo{UID: "fill-client", Callsign: "FILL"})
			mgr.KickClient("fill-client")
		}

		// Next kick should fail
		reg.Register(&ClientInfo{UID: "overflow-client", Callsign: "OVER"})
		err := mgr.KickClient("overflow-client")
		if err == nil {
			t.Error("expected error when kick channel is full")
		}
	})
}

func TestManagerGetStats(t *testing.T) {
	reg := newTestRegistry()
	mgr := NewManager(reg, slog.Default())

	stats := mgr.GetStats()
	if stats.Connected != 0 {
		t.Errorf("expected 0 connected, got %d", stats.Connected)
	}
	if stats.StartedAt.IsZero() {
		t.Error("StartedAt should not be zero")
	}
	if stats.Uptime == "" {
		t.Error("Uptime should not be empty")
	}

	// Add some clients
	reg.Register(&ClientInfo{UID: "c1"})
	reg.Register(&ClientInfo{UID: "c2"})

	stats = mgr.GetStats()
	if stats.Connected != 2 {
		t.Errorf("expected 2 connected, got %d", stats.Connected)
	}
}

func TestManagerKickChan(t *testing.T) {
	reg := newTestRegistry()
	mgr := NewManager(reg, slog.Default())

	ch := mgr.KickChan()
	if ch == nil {
		t.Fatal("KickChan returned nil")
	}
}
