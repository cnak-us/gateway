// Package natsutil provides NATS connection helpers with retry and auth.
package natsutil

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

// ConnectConfig holds NATS connection configuration.
type ConnectConfig struct {
	URL        string
	MaxRetries int
	RetryDelay time.Duration
	CredsFile  string
	AuthToken  string
	NKeySeed   string
	Name       string
}

// DefaultConfig returns a ConnectConfig with sensible defaults.
// The default URL "nats://nats-server:4222" matches the Docker Compose service
// name. All services should read NATS_URL via ConnectConfigFromEnv() to override.
func DefaultConfig() ConnectConfig {
	return ConnectConfig{
		URL:        "nats://nats-server:4222",
		MaxRetries: 5,
		RetryDelay: 2 * time.Second,
	}
}

// ConnectConfigFromEnv builds a ConnectConfig from standard environment variables.
func ConnectConfigFromEnv() ConnectConfig {
	cfg := DefaultConfig()
	if url := os.Getenv("NATS_URL"); url != "" {
		cfg.URL = url
	}
	cfg.CredsFile = os.Getenv("NATS_CREDENTIALS_FILE")
	cfg.AuthToken = os.Getenv("NATS_AUTH_TOKEN")
	cfg.NKeySeed = os.Getenv("NATS_NKEY_SEED")
	return cfg
}

// Connect establishes a NATS connection with retry and auth support.
// Auth priority: creds file > nkey seed > token > anonymous.
// Returns nil connection (not an error) if all retries are exhausted,
// allowing the caller to run in HTTP-only mode.
func Connect(cfg ConnectConfig) (*nats.Conn, error) {
	opts := []nats.Option{}

	if cfg.Name != "" {
		opts = append(opts, nats.Name(cfg.Name))
	}

	// Auth priority: creds > nkey > token > anonymous
	if cfg.CredsFile != "" {
		opts = append(opts, nats.UserCredentials(cfg.CredsFile))
		log.Printf("Using NATS credentials file authentication: %s", cfg.CredsFile)
	} else if cfg.NKeySeed != "" {
		nkeyOpt, err := nats.NkeyOptionFromSeed(cfg.NKeySeed)
		if err != nil {
			return nil, fmt.Errorf("invalid NKey seed: %w", err)
		}
		opts = append(opts, nkeyOpt)
		log.Println("Using NATS NKey authentication")
	} else if cfg.AuthToken != "" {
		opts = append(opts, nats.Token(cfg.AuthToken))
		log.Println("Using NATS token authentication")
	}

	var nc *nats.Conn
	var err error

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		nc, err = nats.Connect(cfg.URL, opts...)
		if err == nil {
			log.Printf("Connected to NATS server at %s", cfg.URL)
			return nc, nil
		}
		if attempt < cfg.MaxRetries {
			log.Printf("NATS connection attempt %d/%d failed: %v. Retrying in %v...",
				attempt+1, cfg.MaxRetries, err, cfg.RetryDelay)
			time.Sleep(cfg.RetryDelay)
		}
	}

	return nil, fmt.Errorf("failed to connect to NATS after %d attempts: %w", cfg.MaxRetries+1, err)
}

// MustConnect is like Connect but calls log.Fatal on failure.
func MustConnect(cfg ConnectConfig) *nats.Conn {
	nc, err := Connect(cfg)
	if err != nil {
		log.Fatalf("NATS connection failed: %v", err)
	}
	return nc
}
