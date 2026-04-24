package bridge

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	cot "github.com/cnak-us/cnak/pkg/cot"
)

// ReplayConfig holds store-and-forward replay settings.
type ReplayConfig struct {
	Enabled bool
	Window  time.Duration
}

// ReplayMissedMessages queries JetStream for messages published between
// disconnectTime and now on tracks.*.update, and sends them to the client
// in chronological order. It respects the configured window and the client's
// group memberships. This should be called asynchronously after DumpCacheTo.
//
// Returns the number of messages replayed, or an error if JetStream is unavailable.
func (b *Bridge) ReplayMissedMessages(c ClientWriter, disconnectTime time.Time, cfg ReplayConfig) (int, error) {
	if !cfg.Enabled {
		return 0, nil
	}

	b.mu.RLock()
	nc := b.nc
	b.mu.RUnlock()

	if nc == nil || !nc.IsConnected() {
		return 0, fmt.Errorf("NATS not connected")
	}

	js, err := nc.JetStream()
	if err != nil {
		return 0, fmt.Errorf("JetStream not available: %w", err)
	}

	// Clamp the replay start to the configured window
	earliest := time.Now().Add(-cfg.Window)
	if disconnectTime.Before(earliest) {
		disconnectTime = earliest
	}

	// Subscribe to tracks.*.update starting from the disconnect time.
	// Use an ordered consumer for efficient, chronological delivery.
	sub, err := js.Subscribe(
		"tracks.*.update",
		func(_ *nats.Msg) {}, // we pull manually below
		nats.OrderedConsumer(),
		nats.StartTime(disconnectTime),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create replay subscription: %w", err)
	}
	defer sub.Unsubscribe()

	clientGroups := c.Groups()
	replayed := 0
	// Use a deadline so we don't hang if the stream is huge or stuck.
	now := time.Now()
	deadline := now.Add(10 * time.Second)

	for time.Now().Before(deadline) {
		msg, err := sub.NextMsg(500 * time.Millisecond)
		if err != nil {
			// Timeout means we've consumed all available messages
			break
		}

		// Extract group from NATS subject (tracks.{group}.update)
		messageGroup := extractGroupFromSubject(msg.Subject)

		if !ShouldRelay(messageGroup, clientGroups) {
			continue
		}

		var event cot.Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			b.logger.Warn("replay: failed to unmarshal message", "error", err)
			continue
		}

		if !event.Stale.IsZero() && now.After(event.Stale) {
			continue
		}

		// Prefer raw XML from header; fall back to JSON->XML conversion
		rawXML := msg.Header.Get("X-Raw-XML")
		var cotXML []byte
		if rawXML != "" {
			cotXML = []byte(rawXML)
		} else {
			point := cotEventToPoint(&event)
			if messageGroup != "" {
				point.Group = messageGroup
			}
			cotXML, err = PointToCoTXML(point)
			if err != nil {
				b.logger.Warn("replay: failed to convert to CoT XML", "error", err)
				continue
			}
		}

		c.Send(cotXML)
		replayed++
	}

	b.logger.Info("store-and-forward replay complete",
		"client", c.UID(),
		"replayed", replayed,
		"since", disconnectTime.Format(time.RFC3339),
	)

	return replayed, nil
}

// extractGroupFromSubject pulls the group token from "tracks.{group}.update".
func extractGroupFromSubject(subject string) string {
	// Simple split — subject is always "tracks.GROUP.update"
	var dot1, dot2 int
	for i, ch := range subject {
		if ch == '.' {
			if dot1 == 0 {
				dot1 = i
			} else {
				dot2 = i
				break
			}
		}
	}
	if dot1 > 0 && dot2 > dot1 {
		return subject[dot1+1 : dot2]
	}
	return DefaultGroup
}

// ParseReplayWindow parses a duration string for the replay window config.
// Returns the parsed duration or the default if parsing fails.
func ParseReplayWindow(s string, defaultDuration time.Duration, logger *slog.Logger) time.Duration {
	if s == "" {
		return defaultDuration
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		logger.Warn("invalid STORE_FORWARD_WINDOW, using default",
			"value", s,
			"default", defaultDuration,
			"error", err,
		)
		return defaultDuration
	}
	return d
}
