// Package logging provides structured logging setup with optional NATS publishing.
package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// Setup initializes structured logging for a service.
// It configures slog as the default logger and bridges stdlib log output.
// If a NATS connection is provided, logs are also published to NATS.
func Setup(serviceName string, nc ...*nats.Conn) *slog.Logger {
	level := parseLevel(os.Getenv("LOG_LEVEL"))
	format := strings.ToLower(os.Getenv("LOG_FORMAT"))

	opts := &slog.HandlerOptions{Level: level}

	var baseHandler slog.Handler
	if format == "text" {
		baseHandler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		baseHandler = slog.NewJSONHandler(os.Stdout, opts)
	}

	// Add service name attribute to all log lines
	baseHandler = baseHandler.WithAttrs([]slog.Attr{
		slog.String("service", serviceName),
	})

	var handler slog.Handler = baseHandler

	// Wrap with NATS publishing handler if connection provided
	if len(nc) > 0 && nc[0] != nil {
		handler = &natsHandler{
			base:        baseHandler,
			nc:          nc[0],
			serviceName: serviceName,
			limiter:     newRateLimiter(100),
		}
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Bridge stdlib log → slog so existing log.Printf calls output as structured JSON
	log.SetOutput(&slogWriter{logger: logger})
	log.SetFlags(0)

	return logger
}

// EnableNATS wraps the current default slog handler with a NATS publishing handler.
// Call this after establishing a NATS connection to start publishing logs.
// If the current handler is already a natsHandler, it reuses the base handler
// to avoid nested NATS publishing (safe for embedded services in the same process).
func EnableNATS(serviceName string, nc *nats.Conn) {
	if nc == nil {
		return
	}

	// Unwrap any existing natsHandler to avoid nesting.
	// Embedded services call EnableNATS after the backend already set one up;
	// without unwrapping, logs would publish to NATS multiple times.
	base := slog.Default().Handler()
	if nh, ok := base.(*natsHandler); ok {
		base = nh.base
	}

	handler := &natsHandler{
		base:        base,
		nc:          nc,
		serviceName: serviceName,
		limiter:     newRateLimiter(100),
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	log.SetOutput(&slogWriter{logger: logger})
	log.SetFlags(0)
}


func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		if s != "" && s != "info" {
			fmt.Fprintf(os.Stderr, "WARNING: unrecognized LOG_LEVEL %q, defaulting to \"info\"\n", s)
		}
		return slog.LevelInfo
	}
}

// slogWriter adapts slog.Logger to io.Writer for stdlib log bridge.
type slogWriter struct {
	logger *slog.Logger
}

func (w *slogWriter) Write(p []byte) (n int, err error) {
	msg := strings.TrimRight(string(p), "\n")
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "fatal"):
		w.logger.Error(msg)
	case strings.Contains(lower, "warn"):
		w.logger.Warn(msg)
	default:
		w.logger.Info(msg)
	}
	return len(p), nil
}

// natsHandler wraps a base slog.Handler and publishes logs to NATS.
type natsHandler struct {
	base        slog.Handler
	nc          *nats.Conn
	serviceName string
	limiter     *rateLimiter
}

func (h *natsHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

func (h *natsHandler) Handle(ctx context.Context, r slog.Record) error {
	// Always write to stdout via base handler
	err := h.base.Handle(ctx, r)

	// Publish to NATS if rate limit allows
	if h.nc != nil && h.nc.IsConnected() && h.limiter.allow() {
		levelStr := strings.ToLower(r.Level.String())

		// Use the "service" attribute from the record if present (handles
		// embedded services that share the global logger). Falls back to
		// the handler's configured serviceName.
		svcName := h.serviceName
		attrs := map[string]any{}
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "service" {
				svcName = a.Value.String()
			} else {
				attrs[a.Key] = a.Value.Any()
			}
			return true
		})

		subject := "logs." + svcName + "." + levelStr

		entry := map[string]any{
			"timestamp": r.Time.UTC().Format(time.RFC3339Nano),
			"level":     levelStr,
			"service":   svcName,
			"message":   r.Message,
		}
		if len(attrs) > 0 {
			entry["attrs"] = attrs
		}

		data, jsonErr := json.Marshal(entry)
		if jsonErr == nil {
			_ = h.nc.Publish(subject, data)
		}
	}

	return err
}

func (h *natsHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &natsHandler{
		base:        h.base.WithAttrs(attrs),
		nc:          h.nc,
		serviceName: h.serviceName,
		limiter:     h.limiter,
	}
}

func (h *natsHandler) WithGroup(name string) slog.Handler {
	return &natsHandler{
		base:        h.base.WithGroup(name),
		nc:          h.nc,
		serviceName: h.serviceName,
		limiter:     h.limiter,
	}
}

// rateLimiter implements a simple token bucket rate limiter.
type rateLimiter struct {
	mu       sync.Mutex
	tokens   int
	max      int
	lastTime time.Time
}

func newRateLimiter(maxPerSecond int) *rateLimiter {
	return &rateLimiter{
		tokens:   maxPerSecond,
		max:      maxPerSecond,
		lastTime: time.Now(),
	}
}

func (rl *rateLimiter) allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastTime).Seconds()
	rl.tokens += int(elapsed * float64(rl.max))
	if rl.tokens > rl.max {
		rl.tokens = rl.max
	}
	rl.lastTime = now

	if rl.tokens > 0 {
		rl.tokens--
		return true
	}
	return false
}
