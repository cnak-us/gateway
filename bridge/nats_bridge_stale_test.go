package bridge

import (
	"testing"
	"time"
)

func TestDumpCacheToSkipsStalePoints(t *testing.T) {
	b := testBridge()

	staleTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	freshTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	b.pointCache["stale-uid"] = &Point{
		ID:        "stale-uid",
		Latitude:  38.0,
		Longitude: -77.0,
		Type:      "a-f-G",
		Group:     DefaultGroup,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Stale:     staleTime,
	}
	b.pointCache["fresh-uid"] = &Point{
		ID:        "fresh-uid",
		Latitude:  39.0,
		Longitude: -78.0,
		Type:      "a-f-G",
		Group:     DefaultGroup,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Stale:     freshTime,
	}
	b.pointCache["no-stale-uid"] = &Point{
		ID:        "no-stale-uid",
		Latitude:  40.0,
		Longitude: -79.0,
		Type:      "a-f-G",
		Group:     DefaultGroup,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		// Stale intentionally empty — should always pass through
	}

	client := &mockClientWriter{uid: "test-client", groups: nil}
	sent := b.DumpCacheTo(client)

	if sent != 2 {
		t.Errorf("expected 2 points sent (fresh + no-stale), got %d", sent)
	}
}

func TestDumpCacheToSendsFreshPoints(t *testing.T) {
	b := testBridge()

	freshTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	b.pointCache["fresh-1"] = &Point{
		ID:        "fresh-1",
		Latitude:  38.0,
		Longitude: -77.0,
		Type:      "a-f-G",
		Group:     DefaultGroup,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Stale:     freshTime,
	}
	b.pointCache["fresh-2"] = &Point{
		ID:        "fresh-2",
		Latitude:  39.0,
		Longitude: -78.0,
		Type:      "a-f-G",
		Group:     DefaultGroup,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Stale:     freshTime,
	}

	client := &mockClientWriter{uid: "test-client", groups: nil}
	sent := b.DumpCacheTo(client)

	if sent != 2 {
		t.Errorf("expected 2 fresh points sent, got %d", sent)
	}
}

func TestDumpCacheToEmptyStalePassesThrough(t *testing.T) {
	b := testBridge()

	b.pointCache["no-stale"] = &Point{
		ID:        "no-stale",
		Latitude:  38.0,
		Longitude: -77.0,
		Type:      "a-f-G",
		Group:     DefaultGroup,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		// Stale intentionally empty
	}

	client := &mockClientWriter{uid: "test-client", groups: nil}
	sent := b.DumpCacheTo(client)

	if sent != 1 {
		t.Errorf("expected 1 point sent (no stale = pass through), got %d", sent)
	}
}

func TestDumpCacheToStaleExactlyNowSkips(t *testing.T) {
	b := testBridge()

	// Set stale to a moment just before now so time.Now().After(t) is true
	staleTime := time.Now().Add(-1 * time.Millisecond).Format(time.RFC3339)

	b.pointCache["edge-uid"] = &Point{
		ID:        "edge-uid",
		Latitude:  38.0,
		Longitude: -77.0,
		Type:      "a-f-G",
		Group:     DefaultGroup,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Stale:     staleTime,
	}

	client := &mockClientWriter{uid: "test-client", groups: nil}
	sent := b.DumpCacheTo(client)

	if sent != 0 {
		t.Errorf("expected 0 points sent (stale at/before now should skip), got %d", sent)
	}
}
