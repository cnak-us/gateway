package bridge

import (
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockClientWriter implements ClientWriter for testing.
type mockClientWriter struct {
	uid      string
	groups   []string
	mu       sync.Mutex
	messages [][]byte
}

func (m *mockClientWriter) Send(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, data)
}

func (m *mockClientWriter) UID() string { return m.uid }

func (m *mockClientWriter) Groups() []string { return m.groups }

func (m *mockClientWriter) received() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages)
}

func testBridge() *Bridge {
	return &Bridge{
		pointCache: make(map[string]*Point),
		logger:     slog.Default(),
	}
}

func TestBridgeCachePoint(t *testing.T) {
	b := testBridge()

	t.Run("cache normal point", func(t *testing.T) {
		p := &Point{ID: "uid-1", Latitude: 38.0, Longitude: -77.0}
		b.cachePoint(p)

		b.cacheMu.RLock()
		cached, ok := b.pointCache["uid-1"]
		b.cacheMu.RUnlock()

		if !ok {
			t.Fatal("point not found in cache")
		}
		if cached.Latitude != 38.0 {
			t.Errorf("expected lat 38.0, got %f", cached.Latitude)
		}
	})

	t.Run("skip stale point", func(t *testing.T) {
		staleTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		p := &Point{ID: "stale-uid", Stale: staleTime}
		b.cachePoint(p)

		b.cacheMu.RLock()
		_, ok := b.pointCache["stale-uid"]
		b.cacheMu.RUnlock()

		if ok {
			t.Error("stale point should not be cached")
		}
	})

	t.Run("cache future stale point", func(t *testing.T) {
		futureStale := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
		p := &Point{ID: "future-uid", Stale: futureStale}
		b.cachePoint(p)

		b.cacheMu.RLock()
		_, ok := b.pointCache["future-uid"]
		b.cacheMu.RUnlock()

		if !ok {
			t.Error("future stale point should be cached")
		}
	})

	t.Run("TrackID used as key", func(t *testing.T) {
		p := &Point{ID: "composite-id-123", TrackID: "track-uid"}
		b.cachePoint(p)

		b.cacheMu.RLock()
		_, ok := b.pointCache["track-uid"]
		b.cacheMu.RUnlock()

		if !ok {
			t.Error("point should be cached under TrackID")
		}
		// The ID should be normalized to TrackID
		b.cacheMu.RLock()
		cached := b.pointCache["track-uid"]
		b.cacheMu.RUnlock()
		if cached.ID != "track-uid" {
			t.Errorf("ID should be normalized to TrackID, got %q", cached.ID)
		}
	})
}

func TestBridgeSeedCache(t *testing.T) {
	b := testBridge()

	points := []*Point{
		{ID: "uid-1", Latitude: 1.0},
		{ID: "uid-2", Latitude: 2.0},
		nil,
		{ID: "", Latitude: 3.0}, // empty ID should be skipped
		{ID: "uid-3", Latitude: 4.0},
	}

	seeded := b.SeedCache(points)
	if seeded != 3 {
		t.Errorf("expected 3 seeded, got %d", seeded)
	}

	b.cacheMu.RLock()
	if len(b.pointCache) != 3 {
		t.Errorf("expected 3 points in cache, got %d", len(b.pointCache))
	}
	b.cacheMu.RUnlock()
}

func TestBridgePruneStaleCacheEntries(t *testing.T) {
	b := testBridge()

	staleTime := time.Now().Add(-1 * time.Minute).Format(time.RFC3339)
	futureTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	b.pointCache["stale"] = &Point{ID: "stale", Stale: staleTime}
	b.pointCache["fresh"] = &Point{ID: "fresh", Stale: futureTime}
	b.pointCache["no-stale"] = &Point{ID: "no-stale"} // no Stale field

	b.PruneStaleCacheEntries()

	b.cacheMu.RLock()
	defer b.cacheMu.RUnlock()

	if _, ok := b.pointCache["stale"]; ok {
		t.Error("stale point should be pruned")
	}
	if _, ok := b.pointCache["fresh"]; !ok {
		t.Error("fresh point should remain")
	}
	if _, ok := b.pointCache["no-stale"]; !ok {
		t.Error("point without stale should remain")
	}
}

func TestBridgeDumpCacheTo(t *testing.T) {
	b := testBridge()

	// Add some points to cache
	b.pointCache["uid-1"] = &Point{
		ID:        "uid-1",
		Latitude:  38.0,
		Longitude: -77.0,
		Type:      "a-f-G",
		Group:     "ALPHA",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.pointCache["uid-2"] = &Point{
		ID:        "uid-2",
		Latitude:  39.0,
		Longitude: -78.0,
		Type:      "a-f-G",
		Group:     "BRAVO",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.pointCache["uid-3"] = &Point{
		ID:        "uid-3",
		Latitude:  40.0,
		Longitude: -79.0,
		Type:      "a-f-G",
		Group:     DefaultGroup,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	t.Run("dump all to client with no groups", func(t *testing.T) {
		client := &mockClientWriter{uid: "client-1", groups: nil}
		sent := b.DumpCacheTo(client)
		// nil groups = relay all
		if sent != 3 {
			t.Errorf("expected 3 points sent, got %d", sent)
		}
	})

	t.Run("dump filtered by group", func(t *testing.T) {
		client := &mockClientWriter{uid: "client-2", groups: []string{"ALPHA"}}
		sent := b.DumpCacheTo(client)
		// Should get ALPHA point + ANON point
		if sent != 2 {
			t.Errorf("expected 2 points sent (ALPHA + ANON), got %d", sent)
		}
	})

	t.Run("dump to client with no matching group", func(t *testing.T) {
		client := &mockClientWriter{uid: "client-3", groups: []string{"CHARLIE"}}
		sent := b.DumpCacheTo(client)
		// Should get only ANON point
		if sent != 1 {
			t.Errorf("expected 1 point sent (ANON only), got %d", sent)
		}
	})
}

func TestBridgeRelayToClients(t *testing.T) {
	t.Run("skip sender", func(t *testing.T) {
		sender := &mockClientWriter{uid: "sender-uid"}
		receiver := &mockClientWriter{uid: "receiver-uid"}

		b := testBridge()
		b.clientFn = func() []ClientWriter { return []ClientWriter{sender, receiver} }

		b.relayToClients([]byte("test data"), "sender-uid", "")

		if sender.received() != 0 {
			t.Error("sender should not receive its own message")
		}
		if receiver.received() != 1 {
			t.Errorf("receiver should get 1 message, got %d", receiver.received())
		}
	})

	t.Run("group filtering", func(t *testing.T) {
		alpha := &mockClientWriter{uid: "alpha-client", groups: []string{"ALPHA"}}
		bravo := &mockClientWriter{uid: "bravo-client", groups: []string{"BRAVO"}}

		b := testBridge()
		b.clientFn = func() []ClientWriter { return []ClientWriter{alpha, bravo} }

		b.relayToClients([]byte("alpha msg"), "", "ALPHA")

		if alpha.received() != 1 {
			t.Error("alpha client should receive ALPHA group message")
		}
		if bravo.received() != 0 {
			t.Error("bravo client should not receive ALPHA group message")
		}
	})

	t.Run("nil client provider", func(t *testing.T) {
		b := testBridge()
		// Should not panic
		b.relayToClients([]byte("test"), "", "")
	})
}

func TestBridgeSetClientProvider(t *testing.T) {
	b := testBridge()

	called := false
	b.SetClientProvider(func() []ClientWriter {
		called = true
		return nil
	})

	b.relayToClients([]byte("test"), "", "")
	if !called {
		t.Error("client provider should have been called")
	}
}

func TestBridgeUpdateConn(t *testing.T) {
	b := testBridge()

	// Should not panic with nil
	b.UpdateConn(nil)

	b.mu.RLock()
	if b.nc != nil {
		t.Error("expected nil nc after UpdateConn(nil)")
	}
	b.mu.RUnlock()
}

func TestBuildDeleteCoT(t *testing.T) {
	t.Run("contains required elements", func(t *testing.T) {
		uid := "test-client-uid"
		xml := string(BuildDeleteCoT(uid))

		if !strings.Contains(xml, `uid="test-client-uid"`) {
			t.Error("missing client UID in event")
		}
		if !strings.Contains(xml, `type="t-x-d"`) {
			t.Error("missing t-x-d type")
		}
		if !strings.Contains(xml, `how="h-g-i-g-o"`) {
			t.Error("missing how attribute")
		}
		if !strings.Contains(xml, `ce="999999"`) {
			t.Error("missing ce=999999")
		}
		if !strings.Contains(xml, `le="999999"`) {
			t.Error("missing le=999999")
		}
		if !strings.Contains(xml, `<link uid="test-client-uid"`) {
			t.Error("missing link element with client UID")
		}
		if !strings.Contains(xml, `relation="p-p"`) {
			t.Error("missing p-p relation")
		}
		if !strings.HasPrefix(xml, "<event") {
			t.Error("should start with <event")
		}
		if !strings.HasSuffix(xml, "</event>") {
			t.Error("should end with </event>")
		}
	})

	t.Run("escapes special characters", func(t *testing.T) {
		uid := `uid&"special`
		xml := string(BuildDeleteCoT(uid))

		if strings.Contains(xml, `uid="uid&"special"`) {
			t.Error("UID should be XML-escaped")
		}
		if !strings.Contains(xml, "&amp;") {
			t.Error("ampersand should be escaped")
		}
	})

	t.Run("stale is after time", func(t *testing.T) {
		before := time.Now().UTC()
		xml := string(BuildDeleteCoT("uid-1"))
		after := time.Now().UTC()

		// Extract time and stale from XML
		timeStr := extractXMLAttr(xml, "time")
		staleStr := extractXMLAttr(xml, "stale")

		timeVal, err := time.Parse(time.RFC3339, timeStr)
		if err != nil {
			t.Fatalf("failed to parse time: %v", err)
		}
		staleVal, err := time.Parse(time.RFC3339, staleStr)
		if err != nil {
			t.Fatalf("failed to parse stale: %v", err)
		}

		if timeVal.Before(before.Add(-1*time.Second)) || timeVal.After(after.Add(1*time.Second)) {
			t.Error("time should be approximately now")
		}

		staleDiff := staleVal.Sub(timeVal)
		if staleDiff < 19*time.Second || staleDiff > 21*time.Second {
			t.Errorf("stale should be ~20s after time, got %v", staleDiff)
		}
	})
}

func TestBridgeRemoveFromCache(t *testing.T) {
	b := testBridge()

	b.pointCache["uid-1"] = &Point{ID: "uid-1", Latitude: 38.0}
	b.pointCache["uid-2"] = &Point{ID: "uid-2", Latitude: 39.0}

	b.RemoveFromCache("uid-1")

	b.cacheMu.RLock()
	defer b.cacheMu.RUnlock()

	if _, ok := b.pointCache["uid-1"]; ok {
		t.Error("uid-1 should be removed from cache")
	}
	if _, ok := b.pointCache["uid-2"]; !ok {
		t.Error("uid-2 should still be in cache")
	}
}

func TestBridgeRemoveFromCacheNonexistent(t *testing.T) {
	b := testBridge()
	b.pointCache["uid-1"] = &Point{ID: "uid-1"}

	// Should not panic
	b.RemoveFromCache("nonexistent")

	b.cacheMu.RLock()
	if len(b.pointCache) != 1 {
		t.Error("cache should still have 1 entry")
	}
	b.cacheMu.RUnlock()
}

func TestPublishDisconnectNilConn(t *testing.T) {
	b := testBridge()

	// Should return nil silently when NATS is not connected
	err := b.PublishDisconnect("test-uid")
	if err != nil {
		t.Errorf("expected nil error for nil connection, got: %v", err)
	}
}

func TestDisconnectBroadcastToLocalClients(t *testing.T) {
	// Simulate the disconnect broadcast logic from main.go:
	// Build delete CoT and send to all remaining local clients.
	client1 := &mockClientWriter{uid: "remaining-1"}
	client2 := &mockClientWriter{uid: "remaining-2"}

	deleteXML := BuildDeleteCoT("disconnected-uid")

	// Simulate local fan-out (as done in main.go handleConnection cleanup)
	for _, c := range []*mockClientWriter{client1, client2} {
		c.Send(deleteXML)
	}

	if client1.received() != 1 {
		t.Errorf("client1 should receive 1 delete event, got %d", client1.received())
	}
	if client2.received() != 1 {
		t.Errorf("client2 should receive 1 delete event, got %d", client2.received())
	}

	// Verify the content is a t-x-d event for the correct UID
	msg := string(client1.messages[0])
	if !strings.Contains(msg, `type="t-x-d"`) {
		t.Error("message should be a t-x-d delete event")
	}
	if !strings.Contains(msg, `uid="disconnected-uid"`) {
		t.Error("message should reference the disconnected UID")
	}
}

// extractXMLAttr extracts an attribute value from a raw XML string (simple helper for tests).
func extractXMLAttr(xml, attr string) string {
	key := attr + `="`
	idx := strings.Index(xml, key)
	if idx < 0 {
		return ""
	}
	start := idx + len(key)
	end := strings.Index(xml[start:], `"`)
	if end < 0 {
		return ""
	}
	return xml[start : start+end]
}

func TestPublishCoTNilConn(t *testing.T) {
	b := testBridge()

	// Should return nil silently when NATS is not connected
	err := b.PublishCoT([]byte(`<event uid="1" type="a-f-G" time="t" stale="s" how="h"><point lat="0" lon="0" hae="0" ce="0" le="0"/></event>`), "sender")
	if err != nil {
		t.Errorf("expected nil error for nil connection, got: %v", err)
	}
}

func TestPublishCoTInvalidXML(t *testing.T) {
	// PublishCoT with nil conn should return nil even for invalid XML
	// because the nil conn check happens first
	b := testBridge()

	err := b.PublishCoT([]byte("not xml"), "sender")
	if err != nil {
		t.Errorf("expected nil error for nil connection even with bad XML, got: %v", err)
	}
}
