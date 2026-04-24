package clients

import (
	"fmt"
	"log/slog"
	"time"
)

// Stats holds aggregate client connection statistics.
type Stats struct {
	Connected     int       `json:"connected"`
	TotalMessages int64     `json:"totalMessages"`
	Uptime        string    `json:"uptime"`
	StartedAt     time.Time `json:"startedAt"`
}

// Manager provides client lifecycle management on top of the registry.
type Manager struct {
	registry  *Registry
	kickCh    chan string
	startedAt time.Time
	logger    *slog.Logger
}

// NewManager creates a new client manager.
func NewManager(registry *Registry, logger *slog.Logger) *Manager {
	return &Manager{
		registry:  registry,
		kickCh:    make(chan string, 16),
		startedAt: time.Now(),
		logger:    logger,
	}
}

// KickClient sends a kick request for the given client UID.
func (m *Manager) KickClient(uid string) error {
	// Verify the client exists before sending kick.
	_, err := m.registry.GetClient(uid)
	if err != nil {
		return fmt.Errorf("client %s not found: %w", uid, err)
	}

	select {
	case m.kickCh <- uid:
		m.logger.Info("kick request sent", "uid", uid)
		return nil
	default:
		return fmt.Errorf("kick channel full, try again later")
	}
}

// KickChan returns a read-only channel for receiving kick requests.
func (m *Manager) KickChan() <-chan string {
	return m.kickCh
}

// GetStats returns aggregate statistics.
func (m *Manager) GetStats() *Stats {
	return &Stats{
		Connected: m.registry.Count(),
		Uptime:    time.Since(m.startedAt).Truncate(time.Second).String(),
		StartedAt: m.startedAt,
	}
}
