package clients

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestRegistryRegisterDeregister(t *testing.T) {
	reg := newTestRegistry()

	t.Run("register client", func(t *testing.T) {
		info := &ClientInfo{
			UID:      "uid-1",
			Callsign: "ALPHA-1",
			IP:       "192.168.1.1",
		}
		if err := reg.Register(info); err != nil {
			t.Fatalf("Register failed: %v", err)
		}

		if reg.Count() != 1 {
			t.Errorf("expected count 1, got %d", reg.Count())
		}
	})

	t.Run("get registered client", func(t *testing.T) {
		info, err := reg.GetClient("uid-1")
		if err != nil {
			t.Fatalf("GetClient failed: %v", err)
		}
		if info.Callsign != "ALPHA-1" {
			t.Errorf("expected callsign 'ALPHA-1', got %q", info.Callsign)
		}
	})

	t.Run("deregister client", func(t *testing.T) {
		if err := reg.Deregister("uid-1"); err != nil {
			t.Fatalf("Deregister failed: %v", err)
		}

		if reg.Count() != 0 {
			t.Errorf("expected count 0 after deregister, got %d", reg.Count())
		}
	})

	t.Run("get nonexistent client", func(t *testing.T) {
		_, err := reg.GetClient("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent client")
		}
	})

	t.Run("deregister nonexistent client (no error for local)", func(t *testing.T) {
		err := reg.Deregister("nonexistent")
		// For local-only registry, delete from map is a no-op
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestRegistryPodName(t *testing.T) {
	reg := &Registry{
		podName: "pod-abc",
		local:   make(map[string]*ClientInfo),
		logger:  slog.Default(),
	}

	info := &ClientInfo{UID: "uid-1"}
	reg.Register(info)

	got, _ := reg.GetClient("uid-1")
	if got.PodName != "pod-abc" {
		t.Errorf("expected PodName 'pod-abc', got %q", got.PodName)
	}
}

func TestRegistryUpdateLastSeen(t *testing.T) {
	reg := newTestRegistry()

	info := &ClientInfo{
		UID:      "uid-1",
		LastSeen: time.Now().Add(-1 * time.Hour),
	}
	reg.Register(info)

	before := info.LastSeen

	if err := reg.UpdateLastSeen("uid-1"); err != nil {
		t.Fatalf("UpdateLastSeen failed: %v", err)
	}

	got, _ := reg.GetClient("uid-1")
	if !got.LastSeen.After(before) {
		t.Error("LastSeen should be updated to a more recent time")
	}
}

func TestRegistryUpdateLastSeenNotFound(t *testing.T) {
	reg := newTestRegistry()

	err := reg.UpdateLastSeen("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent client")
	}
}

func TestRegistryListAll(t *testing.T) {
	reg := newTestRegistry()

	// Empty list
	all, err := reg.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected 0 clients, got %d", len(all))
	}

	// Add clients
	reg.Register(&ClientInfo{UID: "uid-1", Callsign: "A"})
	reg.Register(&ClientInfo{UID: "uid-2", Callsign: "B"})
	reg.Register(&ClientInfo{UID: "uid-3", Callsign: "C"})

	all, err = reg.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 clients, got %d", len(all))
	}
}

func TestRegistryListLocal(t *testing.T) {
	reg := newTestRegistry()

	reg.Register(&ClientInfo{UID: "uid-1"})
	reg.Register(&ClientInfo{UID: "uid-2"})

	local := reg.ListLocal()
	if len(local) != 2 {
		t.Errorf("expected 2 local clients, got %d", len(local))
	}
}

func TestRegistryCount(t *testing.T) {
	reg := newTestRegistry()

	if reg.Count() != 0 {
		t.Errorf("expected 0, got %d", reg.Count())
	}

	reg.Register(&ClientInfo{UID: "uid-1"})
	if reg.Count() != 1 {
		t.Errorf("expected 1, got %d", reg.Count())
	}

	reg.Register(&ClientInfo{UID: "uid-2"})
	if reg.Count() != 2 {
		t.Errorf("expected 2, got %d", reg.Count())
	}

	reg.Deregister("uid-1")
	if reg.Count() != 1 {
		t.Errorf("expected 1 after deregister, got %d", reg.Count())
	}
}

func TestRegistryDisconnectTimeNoKV(t *testing.T) {
	// Without KV, GetDisconnectTime always returns false
	reg := newTestRegistry()

	reg.Register(&ClientInfo{UID: "uid-1"})
	reg.Deregister("uid-1")

	_, ok := reg.GetDisconnectTime("uid-1")
	if ok {
		t.Error("expected no disconnect time without KV backing")
	}
}

func TestRegistryClearDisconnectTimeNoKV(t *testing.T) {
	// Should not panic without KV
	reg := newTestRegistry()
	reg.ClearDisconnectTime("uid-1") // no-op, just ensure no panic
}

func TestRegistryConcurrentAccess(t *testing.T) {
	reg := newTestRegistry()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			uid := "uid-" + time.Now().String() + "-" + string(rune(i))
			info := &ClientInfo{UID: uid, Callsign: "Concurrent"}
			reg.Register(info)
			reg.GetClient(uid)
			reg.UpdateLastSeen(uid)
			reg.ListLocal()
			reg.ListAll()
			reg.Count()
			reg.Deregister(uid)
		}(i)
	}
	wg.Wait()
}
