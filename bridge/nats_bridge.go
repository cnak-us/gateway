package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	cot "github.com/cnak-us/cnak/pkg/cot"
	"github.com/cnak-us/cnak/pkg/natsutil"
)

// ClientWriter is the interface for sending data to a connected TAK client.
type ClientWriter interface {
	Send(data []byte)
	UID() string
	Groups() []string
}

// Bridge provides bidirectional NATS bridging for CoT messages.
type Bridge struct {
	nc       *nats.Conn
	logger   *slog.Logger
	clientFn func() []ClientWriter
	mu       sync.RWMutex

	cacheMu    sync.RWMutex
	pointCache map[string]*Point
}

// cotEventToPoint converts a pkg/cot.Event (CoT Event JSON) to the bridge's flat Point format.
func cotEventToPoint(e *cot.Event) *Point {
	p := &Point{
		ID:        e.UID,
		TrackID:   e.UID,
		Latitude:  e.Point.Lat,
		Longitude: e.Point.Lon,
		Altitude:  e.Point.HAE,
		CE:        e.Point.CE,
		LE:        e.Point.LE,
		Type:      e.Type,
		Timestamp: e.Time.UTC().Format(time.RFC3339),
		How:       e.How,
		Stale:     e.Stale.UTC().Format(time.RFC3339),
		Group:     DefaultGroup,
	}
	if e.Detail != nil {
		if e.Detail.Track != nil {
			p.Course = e.Detail.Track.Course
			p.Speed = e.Detail.Track.Speed
		}
		// Prefer contact.callsign, fall back to uid.callsign
		if e.Detail.Contact != nil && e.Detail.Contact.Callsign != "" {
			p.Callsign = e.Detail.Contact.Callsign
		} else if e.Detail.UID != nil && e.Detail.UID.Callsign != "" {
			p.Callsign = e.Detail.UID.Callsign
		}
	}
	return p
}

// NewBridge creates a new NATS bridge.
func NewBridge(nc *nats.Conn, logger *slog.Logger) *Bridge {
	return &Bridge{
		nc:         nc,
		logger:     logger,
		pointCache: make(map[string]*Point),
	}
}

// SetClientProvider sets the callback to get the list of connected TAK clients.
func (b *Bridge) SetClientProvider(fn func() []ClientWriter) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clientFn = fn
}

// UpdateConn replaces the NATS connection used by the bridge.
// Call StartRelay again after UpdateConn to re-subscribe on the new connection.
func (b *Bridge) UpdateConn(nc *nats.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nc = nc
}

// cachePoint stores the latest point per UID, skipping entries that are already stale.
// It normalizes ID to TrackID so that points from different sources (sensor-simulator
// publishes "uid", backend stores "uid-nanoseconds") resolve to the same cache entry.
func (b *Bridge) cachePoint(p *Point) {
	if p.Stale != "" {
		if t, err := time.Parse(time.RFC3339, p.Stale); err == nil && time.Now().After(t) {
			return
		}
	}
	// Use TrackID as the canonical key — the backend creates composite IDs
	// (e.g., "uuid-1740000000123456789") that differ from the entity UID.
	// Normalize ID so PointToCoTXML emits the correct uid attribute.
	if p.TrackID != "" {
		p.ID = p.TrackID
	}
	b.cacheMu.Lock()
	b.pointCache[p.ID] = p
	b.cacheMu.Unlock()
}

// DumpCacheTo sends the current point cache to a single client as CoT XML,
// filtered by the client's group memberships. Returns the number of points sent.
func (b *Bridge) DumpCacheTo(c ClientWriter) int {
	b.cacheMu.RLock()
	points := make([]*Point, 0, len(b.pointCache))
	for _, p := range b.pointCache {
		points = append(points, p)
	}
	b.cacheMu.RUnlock()

	clientGroups := c.Groups()
	now := time.Now()
	sent := 0
	for _, p := range points {
		if p.Stale != "" {
			if t, err := time.Parse(time.RFC3339, p.Stale); err == nil && now.After(t) {
				continue
			}
		}
		if !ShouldRelay(p.Group, clientGroups) {
			continue
		}
		cotXML, err := PointToCoTXML(p)
		if err != nil {
			b.logger.Warn("cache dump: failed to convert point to CoT XML", "error", err, "uid", p.ID)
			continue
		}
		c.Send(cotXML)
		sent++
	}
	b.logger.Info("dumped point cache to client", "client", c.UID(), "points", sent)
	return sent
}

// SeedCache adds points to the cache without publishing them to NATS.
// Used to pre-populate the cache from the backend API on startup.
func (b *Bridge) SeedCache(points []*Point) int {
	seeded := 0
	for _, p := range points {
		if p == nil || p.ID == "" {
			continue
		}
		b.cachePoint(p)
		seeded++
	}
	return seeded
}

// PruneStaleCacheEntries removes entries whose Stale timestamp has passed.
func (b *Bridge) PruneStaleCacheEntries() {
	now := time.Now()
	b.cacheMu.Lock()
	for uid, p := range b.pointCache {
		if p.Stale == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, p.Stale); err == nil && now.After(t) {
			delete(b.pointCache, uid)
		}
	}
	b.cacheMu.Unlock()
}

// PublishCoT parses CoT XML into a CoT Event, publishes the Event as JSON to
// tracks.{group}.update with the raw XML in an X-Raw-XML header.
// Returns nil silently when NATS is not connected — local fan-out still works.
func (b *Bridge) PublishCoT(cotXML []byte, senderUID string) error {
	// Snapshot the connection under lock so it can be swapped safely.
	b.mu.RLock()
	nc := b.nc
	b.mu.RUnlock()

	// Skip NATS publish when not connected to avoid log spam.
	// Local fan-out in main.go handles client-to-client relay independently.
	if nc == nil || !nc.IsConnected() {
		return nil
	}

	point, err := CotXMLToPoint(cotXML)
	if err != nil {
		return fmt.Errorf("failed to parse CoT XML: %w", err)
	}

	// Skip non-track CoT types (e.g. t-x-c-m chat messages, t-x-c-t heartbeats).
	// Only atom types (a-*) represent actual entity positions worth storing.
	if strings.HasPrefix(point.Type, "t-x-c-") {
		return nil
	}

	b.cachePoint(point)

	group := natsutil.SanitizeSubjectToken(point.Group)

	// Parse into full CoT Event for the richer JSON format
	event, err := cot.FromXMLBytes(cotXML)
	if err != nil {
		return fmt.Errorf("failed to parse CoT event: %w", err)
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal CoT event: %w", err)
	}

	// Publish CoT Event JSON to tracks.{group}.update with raw XML in header
	subject := fmt.Sprintf("tracks.%s.update", group)
	msg := &nats.Msg{
		Subject: subject,
		Data:    eventJSON,
		Header:  nats.Header{},
	}
	msg.Header.Set("X-Raw-XML", string(cotXML))

	if err := nc.PublishMsg(msg); err != nil {
		return fmt.Errorf("failed to publish to %s: %w", subject, err)
	}

	b.logger.Debug("published track to NATS", "subject", subject, "uid", event.UID)

	return nil
}

// StartRelay subscribes to NATS subjects and relays messages to connected TAK clients.
// It subscribes to tracks.broadcast (raw CoT) and tracks.*.update (track updates).
// Messages are relayed to all connected clients except the original sender.
func (b *Bridge) StartRelay(ctx context.Context) error {
	b.mu.RLock()
	nc := b.nc
	b.mu.RUnlock()

	// Subscribe to tracks.broadcast — CoT Event JSON relay to TAK clients
	broadcastSub, err := nc.Subscribe("tracks.broadcast", func(msg *nats.Msg) {
		var event cot.Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			b.logger.Warn("failed to unmarshal broadcast message", "error", err)
			return
		}

		if !event.Stale.IsZero() && time.Now().After(event.Stale) {
			b.logger.Debug("dropped stale event", "uid", event.UID)
			return
		}

		// If raw XML is in the header, relay it directly; otherwise convert from event
		rawXML := msg.Header.Get("X-Raw-XML")
		var cotXML []byte
		if rawXML != "" {
			cotXML = []byte(rawXML)
		} else {
			point := cotEventToPoint(&event)
			var err error
			cotXML, err = PointToCoTXML(point)
			if err != nil {
				b.logger.Warn("failed to convert event to CoT XML", "error", err, "uid", event.UID)
				return
			}
		}

		group := DefaultGroup
		if event.Detail != nil {
			// Check raw XML for __group if available, otherwise default
			if rawXML != "" {
				group = ExtractGroupFromCoT([]byte(rawXML))
			}
		}

		b.relayToClients(cotXML, event.UID, group)
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to tracks.broadcast: %w", err)
	}

	// Subscribe to tracks.*.update — CoT Event JSON from other services.
	tracksSub, err := nc.Subscribe("tracks.*.update", func(msg *nats.Msg) {
		// Extract group from NATS subject (tracks.{group}.update)
		subjectGroup := ""
		parts := strings.SplitN(msg.Subject, ".", 3)
		if len(parts) >= 2 {
			subjectGroup = parts[1]
		}

		var event cot.Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			b.logger.Warn("failed to unmarshal track message", "error", err)
			return
		}

		if !event.Stale.IsZero() && time.Now().After(event.Stale) {
			b.logger.Debug("dropped stale event", "uid", event.UID)
			return
		}

		if event.UID == "" {
			return
		}

		point := cotEventToPoint(&event)
		if subjectGroup != "" {
			point.Group = subjectGroup
		}

		b.cachePoint(point)

		// If raw XML is in the header, relay it directly; otherwise convert from point
		rawXML := msg.Header.Get("X-Raw-XML")
		var cotXML []byte
		if rawXML != "" {
			cotXML = []byte(rawXML)
		} else {
			var err error
			cotXML, err = PointToCoTXML(point)
			if err != nil {
				b.logger.Warn("failed to convert track to CoT XML", "error", err, "uid", point.ID)
				return
			}
		}

		b.relayToClients(cotXML, point.ID, point.Group)
	})
	if err != nil {
		broadcastSub.Unsubscribe()
		return fmt.Errorf("failed to subscribe to tracks.*.update: %w", err)
	}

	b.logger.Info("NATS relay started", "subjects", []string{"tracks.broadcast", "tracks.*.update"})

	// Periodically prune stale cache entries
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				b.PruneStaleCacheEntries()
			}
		}
	}()

	// Wait for context cancellation then clean up
	go func() {
		<-ctx.Done()
		broadcastSub.Unsubscribe()
		tracksSub.Unsubscribe()
		b.logger.Info("NATS relay stopped")
	}()

	return nil
}

// ShouldRelay determines whether a message from messageGroup should be sent to a
// client with the given groups. Rules:
//   - __ANON__ or empty messageGroup → always relay
//   - Empty clientGroups → always relay (backward compatible for unresolved clients)
//   - Otherwise: relay only if the client has a matching group
func ShouldRelay(messageGroup string, clientGroups []string) bool {
	if messageGroup == "" || messageGroup == DefaultGroup {
		return true
	}
	if len(clientGroups) == 0 {
		return true
	}
	for _, g := range clientGroups {
		if g == messageGroup {
			return true
		}
	}
	return false
}

// BuildDeleteCoT creates a t-x-d (delete) CoT event for the given UID.
// TAK Server broadcasts this when a client disconnects so that other clients
// remove the stale contact from their maps and contact lists.
func BuildDeleteCoT(uid string) []byte {
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	staleStr := now.Add(20 * time.Second).Format(time.RFC3339)

	return []byte(fmt.Sprintf(
		`<event version="2.0" uid="%s" type="t-x-d" how="h-g-i-g-o" time="%s" start="%s" stale="%s">`+
			`<point lat="0" lon="0" hae="0" ce="999999" le="999999"/>`+
			`<detail><link uid="%s" type="a-f-G" relation="p-p"/></detail>`+
			`</event>`,
		cot.EscapeXML(uid), nowStr, nowStr, staleStr, cot.EscapeXML(uid),
	))
}

// PublishDisconnect broadcasts a t-x-d delete CoT event for the given UID
// to the NATS tracks.broadcast subject so that clients on other pods also
// receive it. Returns nil silently when NATS is not connected.
func (b *Bridge) PublishDisconnect(uid string) error {
	b.mu.RLock()
	nc := b.nc
	b.mu.RUnlock()

	if nc == nil || !nc.IsConnected() {
		return nil
	}

	deleteXML := BuildDeleteCoT(uid)

	msg := &nats.Msg{
		Subject: "tracks.broadcast",
		Header:  nats.Header{},
	}
	msg.Header.Set("X-Raw-XML", string(deleteXML))

	// Build a minimal CoT Event JSON for the broadcast subscriber
	event := cot.Event{
		UID:   uid,
		Type:  "t-x-d",
		How:   "h-g-i-g-o",
		Time:  time.Now().UTC(),
		Start: time.Now().UTC(),
		Stale: time.Now().UTC().Add(20 * time.Second),
		Point: cot.Point{Lat: 0, Lon: 0, HAE: 0, CE: 999999, LE: 999999},
	}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal delete event: %w", err)
	}
	msg.Data = eventJSON

	if err := nc.PublishMsg(msg); err != nil {
		return fmt.Errorf("failed to publish disconnect to tracks.broadcast: %w", err)
	}

	b.logger.Info("published disconnect event", "uid", uid)
	return nil
}

// RemoveFromCache removes a client's cached point entry.
func (b *Bridge) RemoveFromCache(uid string) {
	b.cacheMu.Lock()
	delete(b.pointCache, uid)
	b.cacheMu.Unlock()
}

// relayToClients sends data to all connected clients except the sender,
// filtered by group membership.
func (b *Bridge) relayToClients(data []byte, senderUID, messageGroup string) {
	b.mu.RLock()
	fn := b.clientFn
	b.mu.RUnlock()

	if fn == nil {
		return
	}

	clients := fn()
	for _, c := range clients {
		if c.UID() == senderUID {
			continue
		}
		if !ShouldRelay(messageGroup, c.Groups()) {
			continue
		}
		c.Send(data)
	}
}
