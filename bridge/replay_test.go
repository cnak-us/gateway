package bridge

import (
	"log/slog"
	"testing"
	"time"
)

func TestExtractGroupFromSubject(t *testing.T) {
	tests := []struct {
		subject string
		want    string
	}{
		{"tracks.ALPHA.update", "ALPHA"},
		{"tracks.__ANON__.update", "__ANON__"},
		{"tracks.Team_Blue.update", "Team_Blue"},
		{"tracks.update", DefaultGroup},   // malformed
		{"tracks", DefaultGroup},          // too short
		{"", DefaultGroup},                // empty
		{"tracks.a.b.c", "a"},             // extra dots — takes first segment
	}
	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			got := extractGroupFromSubject(tt.subject)
			if got != tt.want {
				t.Errorf("extractGroupFromSubject(%q) = %q, want %q", tt.subject, got, tt.want)
			}
		})
	}
}

func TestParseReplayWindow(t *testing.T) {
	logger := slog.Default()
	defaultDur := 5 * time.Minute

	tests := []struct {
		input string
		want  time.Duration
	}{
		{"5m", 5 * time.Minute},
		{"30s", 30 * time.Second},
		{"1h", time.Hour},
		{"10m30s", 10*time.Minute + 30*time.Second},
		{"", defaultDur},          // empty uses default
		{"invalid", defaultDur},   // invalid uses default
		{"5x", defaultDur},        // invalid unit uses default
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseReplayWindow(tt.input, defaultDur, logger)
			if got != tt.want {
				t.Errorf("ParseReplayWindow(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestReplayConfig_Disabled(t *testing.T) {
	b := testBridge()
	client := &mockClientWriter{uid: "test-client"}

	cfg := ReplayConfig{
		Enabled: false,
		Window:  5 * time.Minute,
	}

	replayed, err := b.ReplayMissedMessages(client, time.Now().Add(-1*time.Minute), cfg)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if replayed != 0 {
		t.Errorf("expected 0 replayed when disabled, got %d", replayed)
	}
}

func TestReplayNilConnection(t *testing.T) {
	b := testBridge() // nc is nil
	client := &mockClientWriter{uid: "test-client"}

	cfg := ReplayConfig{
		Enabled: true,
		Window:  5 * time.Minute,
	}

	_, err := b.ReplayMissedMessages(client, time.Now().Add(-1*time.Minute), cfg)
	if err == nil {
		t.Error("expected error when NATS not connected")
	}
}
