package natsutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
)

// Publish publishes data to a NATS subject with error handling and structured logging.
// Returns error if connection is nil or publish fails.
func Publish(nc *nats.Conn, subject string, data []byte) error {
	if nc == nil {
		return errors.New("nats: cannot publish, connection is nil")
	}

	if err := nc.Publish(subject, data); err != nil {
		slog.Error("nats: publish failed", "subject", subject, "error", err)
		return fmt.Errorf("nats: publish to %s: %w", subject, err)
	}

	slog.Debug("nats: published", "subject", subject, "bytes", len(data))
	return nil
}

// PublishJSON marshals v to JSON and publishes to subject.
// Returns error if marshal fails, connection is nil, or publish fails.
func PublishJSON(nc *nats.Conn, subject string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("nats: json marshal failed", "subject", subject, "error", err)
		return fmt.Errorf("nats: marshal for %s: %w", subject, err)
	}

	return Publish(nc, subject, data)
}
