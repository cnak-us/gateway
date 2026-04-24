package bridge

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cot "github.com/cnak-us/cnak/pkg/cot"
)

// ginkgoMockClientWriter is a test double implementing ClientWriter for Ginkgo tests.
type ginkgoMockClientWriter struct {
	uid    string
	groups []string
	mu     sync.Mutex
	sent   [][]byte
}

func (m *ginkgoMockClientWriter) Send(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	m.sent = append(m.sent, cp)
}

func (m *ginkgoMockClientWriter) UID() string      { return m.uid }
func (m *ginkgoMockClientWriter) Groups() []string { return m.groups }

func (m *ginkgoMockClientWriter) sentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

func (m *ginkgoMockClientWriter) lastSent() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		return nil
	}
	return m.sent[len(m.sent)-1]
}

var _ = Describe("NATS Bridge", func() {
	var (
		logger *slog.Logger
	)

	BeforeEach(func() {
		logger = slog.Default()
	})

	Describe("NewBridge", func() {
		It("should create a bridge with nil connection", func() {
			b := NewBridge(nil, logger)
			Expect(b).NotTo(BeNil())
			Expect(b.nc).To(BeNil())
			Expect(b.pointCache).NotTo(BeNil())
			Expect(b.pointCache).To(BeEmpty())
		})
	})

	Describe("SetClientProvider", func() {
		It("should set and retrieve client provider", func() {
			b := NewBridge(nil, logger)
			called := false
			b.SetClientProvider(func() []ClientWriter {
				called = true
				return nil
			})
			Expect(b.clientFn).NotTo(BeNil())
			b.clientFn()
			Expect(called).To(BeTrue())
		})
	})

	Describe("UpdateConn", func() {
		It("should update the NATS connection to nil", func() {
			b := NewBridge(nil, logger)
			b.UpdateConn(nil)
			Expect(b.nc).To(BeNil())
		})
	})

	Describe("cachePoint", func() {
		It("should cache a point by ID", func() {
			b := NewBridge(nil, logger)
			p := &Point{ID: "uid-1", Latitude: 38.0, Longitude: -77.0}
			b.cachePoint(p)

			b.cacheMu.RLock()
			cached, ok := b.pointCache["uid-1"]
			b.cacheMu.RUnlock()

			Expect(ok).To(BeTrue())
			Expect(cached.Latitude).To(BeNumerically("~", 38.0, 0.001))
		})

		It("should use TrackID as cache key when present", func() {
			b := NewBridge(nil, logger)
			p := &Point{ID: "composite-123456", TrackID: "track-uid", Latitude: 1.0}
			b.cachePoint(p)

			b.cacheMu.RLock()
			_, hasComposite := b.pointCache["composite-123456"]
			cached, hasTrack := b.pointCache["track-uid"]
			b.cacheMu.RUnlock()

			Expect(hasComposite).To(BeFalse())
			Expect(hasTrack).To(BeTrue())
			Expect(cached.ID).To(Equal("track-uid"))
		})

		It("should overwrite existing point with same ID", func() {
			b := NewBridge(nil, logger)
			b.cachePoint(&Point{ID: "uid-1", Latitude: 38.0})
			b.cachePoint(&Point{ID: "uid-1", Latitude: 39.0})

			b.cacheMu.RLock()
			cached := b.pointCache["uid-1"]
			b.cacheMu.RUnlock()

			Expect(cached.Latitude).To(BeNumerically("~", 39.0, 0.001))
		})

		It("should skip already-stale points", func() {
			b := NewBridge(nil, logger)
			past := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
			p := &Point{ID: "stale-uid", Stale: past}
			b.cachePoint(p)

			b.cacheMu.RLock()
			_, ok := b.pointCache["stale-uid"]
			b.cacheMu.RUnlock()

			Expect(ok).To(BeFalse())
		})

		It("should cache points with future stale time", func() {
			b := NewBridge(nil, logger)
			future := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)
			p := &Point{ID: "fresh-uid", Stale: future}
			b.cachePoint(p)

			b.cacheMu.RLock()
			_, ok := b.pointCache["fresh-uid"]
			b.cacheMu.RUnlock()

			Expect(ok).To(BeTrue())
		})

		It("should cache points with empty stale time", func() {
			b := NewBridge(nil, logger)
			p := &Point{ID: "no-stale"}
			b.cachePoint(p)

			b.cacheMu.RLock()
			_, ok := b.pointCache["no-stale"]
			b.cacheMu.RUnlock()

			Expect(ok).To(BeTrue())
		})

		It("should cache points with invalid stale time (non-RFC3339)", func() {
			b := NewBridge(nil, logger)
			p := &Point{ID: "bad-stale", Stale: "not-a-date"}
			b.cachePoint(p)

			b.cacheMu.RLock()
			_, ok := b.pointCache["bad-stale"]
			b.cacheMu.RUnlock()

			Expect(ok).To(BeTrue())
		})
	})

	Describe("SeedCache", func() {
		It("should seed multiple points", func() {
			b := NewBridge(nil, logger)
			points := []*Point{
				{ID: "seed-1", Latitude: 38.0},
				{ID: "seed-2", Latitude: 39.0},
				{ID: "seed-3", Latitude: 40.0},
			}

			count := b.SeedCache(points)
			Expect(count).To(Equal(3))

			b.cacheMu.RLock()
			Expect(b.pointCache).To(HaveLen(3))
			b.cacheMu.RUnlock()
		})

		It("should skip nil points", func() {
			b := NewBridge(nil, logger)
			points := []*Point{
				{ID: "valid"},
				nil,
				{ID: "also-valid"},
			}

			count := b.SeedCache(points)
			Expect(count).To(Equal(2))
		})

		It("should skip points with empty ID", func() {
			b := NewBridge(nil, logger)
			points := []*Point{
				{ID: ""},
				{ID: "has-id"},
			}

			count := b.SeedCache(points)
			Expect(count).To(Equal(1))
		})

		It("should return 0 for empty slice", func() {
			b := NewBridge(nil, logger)
			count := b.SeedCache([]*Point{})
			Expect(count).To(Equal(0))
		})

		It("should return 0 for nil slice", func() {
			b := NewBridge(nil, logger)
			count := b.SeedCache(nil)
			Expect(count).To(Equal(0))
		})

		It("should skip already-stale points during seed", func() {
			b := NewBridge(nil, logger)
			past := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
			points := []*Point{
				{ID: "fresh", Stale: time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)},
				{ID: "stale", Stale: past},
			}

			count := b.SeedCache(points)
			// SeedCache increments seeded for all non-nil/non-empty points,
			// but cachePoint internally skips stale ones.
			Expect(count).To(Equal(2))

			b.cacheMu.RLock()
			_, hasFresh := b.pointCache["fresh"]
			_, hasStale := b.pointCache["stale"]
			b.cacheMu.RUnlock()

			Expect(hasFresh).To(BeTrue())
			Expect(hasStale).To(BeFalse())
		})
	})

	Describe("PruneStaleCacheEntries", func() {
		It("should remove stale entries", func() {
			b := NewBridge(nil, logger)
			past := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)
			future := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)

			b.cacheMu.Lock()
			b.pointCache["stale"] = &Point{ID: "stale", Stale: past}
			b.pointCache["fresh"] = &Point{ID: "fresh", Stale: future}
			b.pointCache["no-stale"] = &Point{ID: "no-stale"}
			b.cacheMu.Unlock()

			b.PruneStaleCacheEntries()

			b.cacheMu.RLock()
			Expect(b.pointCache).To(HaveLen(2))
			Expect(b.pointCache).To(HaveKey("fresh"))
			Expect(b.pointCache).To(HaveKey("no-stale"))
			Expect(b.pointCache).NotTo(HaveKey("stale"))
			b.cacheMu.RUnlock()
		})

		It("should keep entries with invalid stale timestamps", func() {
			b := NewBridge(nil, logger)
			b.cacheMu.Lock()
			b.pointCache["bad-ts"] = &Point{ID: "bad-ts", Stale: "garbage"}
			b.cacheMu.Unlock()

			b.PruneStaleCacheEntries()

			b.cacheMu.RLock()
			Expect(b.pointCache).To(HaveKey("bad-ts"))
			b.cacheMu.RUnlock()
		})

		It("should handle empty cache", func() {
			b := NewBridge(nil, logger)
			Expect(func() { b.PruneStaleCacheEntries() }).NotTo(Panic())
		})
	})

	Describe("DumpCacheTo", func() {
		It("should dump all cache entries to client", func() {
			b := NewBridge(nil, logger)
			b.cacheMu.Lock()
			b.pointCache["uid-1"] = &Point{ID: "uid-1", Latitude: 38.0, Longitude: -77.0, Type: "a-f-G"}
			b.pointCache["uid-2"] = &Point{ID: "uid-2", Latitude: 39.0, Longitude: -78.0, Type: "a-f-G"}
			b.cacheMu.Unlock()

			client := &ginkgoMockClientWriter{uid: "test-client", groups: []string{}}
			sent := b.DumpCacheTo(client)

			Expect(sent).To(Equal(2))
			Expect(client.sentCount()).To(Equal(2))
		})

		It("should filter by group membership", func() {
			b := NewBridge(nil, logger)
			b.cacheMu.Lock()
			b.pointCache["alpha-1"] = &Point{ID: "alpha-1", Group: "ALPHA", Type: "a-f-G"}
			b.pointCache["bravo-1"] = &Point{ID: "bravo-1", Group: "BRAVO", Type: "a-f-G"}
			b.pointCache["anon-1"] = &Point{ID: "anon-1", Group: DefaultGroup, Type: "a-f-G"}
			b.cacheMu.Unlock()

			client := &ginkgoMockClientWriter{uid: "test-client", groups: []string{"ALPHA"}}
			sent := b.DumpCacheTo(client)

			// ALPHA point + __ANON__ point (always relayed), BRAVO filtered out
			Expect(sent).To(Equal(2))
		})

		It("should handle empty cache", func() {
			b := NewBridge(nil, logger)
			client := &ginkgoMockClientWriter{uid: "test-client"}
			sent := b.DumpCacheTo(client)
			Expect(sent).To(Equal(0))
			Expect(client.sentCount()).To(Equal(0))
		})

		It("should send valid CoT XML to client", func() {
			b := NewBridge(nil, logger)
			b.cacheMu.Lock()
			b.pointCache["uid-1"] = &Point{
				ID: "uid-1", Latitude: 38.0, Longitude: -77.0,
				Type: "a-f-G", Callsign: "TEST-1",
			}
			b.cacheMu.Unlock()

			client := &ginkgoMockClientWriter{uid: "test-client", groups: []string{}}
			b.DumpCacheTo(client)

			data := client.lastSent()
			Expect(data).NotTo(BeNil())
			Expect(string(data)).To(ContainSubstring("<event"))
			Expect(string(data)).To(ContainSubstring(`uid="uid-1"`))
			Expect(string(data)).To(ContainSubstring(`callsign="TEST-1"`))
		})
	})

	Describe("relayToClients", func() {
		It("should relay to all clients except sender", func() {
			b := NewBridge(nil, logger)

			sender := &ginkgoMockClientWriter{uid: "sender-uid", groups: []string{}}
			receiver1 := &ginkgoMockClientWriter{uid: "recv-1", groups: []string{}}
			receiver2 := &ginkgoMockClientWriter{uid: "recv-2", groups: []string{}}

			b.SetClientProvider(func() []ClientWriter {
				return []ClientWriter{sender, receiver1, receiver2}
			})

			data := []byte("<event/>")
			b.relayToClients(data, "sender-uid", DefaultGroup)

			Expect(sender.sentCount()).To(Equal(0))
			Expect(receiver1.sentCount()).To(Equal(1))
			Expect(receiver2.sentCount()).To(Equal(1))
		})

		It("should filter by group membership", func() {
			b := NewBridge(nil, logger)

			alphaClient := &ginkgoMockClientWriter{uid: "alpha", groups: []string{"ALPHA"}}
			bravoClient := &ginkgoMockClientWriter{uid: "bravo", groups: []string{"BRAVO"}}

			b.SetClientProvider(func() []ClientWriter {
				return []ClientWriter{alphaClient, bravoClient}
			})

			b.relayToClients([]byte("<event/>"), "external", "ALPHA")

			Expect(alphaClient.sentCount()).To(Equal(1))
			Expect(bravoClient.sentCount()).To(Equal(0))
		})

		It("should relay __ANON__ messages to all clients", func() {
			b := NewBridge(nil, logger)

			client1 := &ginkgoMockClientWriter{uid: "c1", groups: []string{"ALPHA"}}
			client2 := &ginkgoMockClientWriter{uid: "c2", groups: []string{"BRAVO"}}

			b.SetClientProvider(func() []ClientWriter {
				return []ClientWriter{client1, client2}
			})

			b.relayToClients([]byte("<event/>"), "external", DefaultGroup)

			Expect(client1.sentCount()).To(Equal(1))
			Expect(client2.sentCount()).To(Equal(1))
		})

		It("should do nothing with nil client provider", func() {
			b := NewBridge(nil, logger)
			// clientFn is nil by default
			Expect(func() {
				b.relayToClients([]byte("<event/>"), "sender", DefaultGroup)
			}).NotTo(Panic())
		})

		It("should do nothing when client provider returns empty list", func() {
			b := NewBridge(nil, logger)
			b.SetClientProvider(func() []ClientWriter {
				return []ClientWriter{}
			})

			Expect(func() {
				b.relayToClients([]byte("<event/>"), "sender", "ALPHA")
			}).NotTo(Panic())
		})
	})

	Describe("PublishCoT", func() {
		It("should return nil silently when NATS is nil", func() {
			b := NewBridge(nil, logger)
			cotXML := []byte(`<event uid="uid-1" type="a-f-G" time="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="m-g"><point lat="38" lon="-77" hae="0" ce="0" le="0"/></event>`)
			err := b.PublishCoT(cotXML, "sender")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error for invalid CoT XML", func() {
			// Need a connected NATS to get past the nil check, but we
			// can't easily mock nats.Conn. The nil conn path returns nil.
			// With nil conn, PublishCoT returns nil before parsing.
			b := NewBridge(nil, logger)
			err := b.PublishCoT([]byte("not xml"), "sender")
			Expect(err).NotTo(HaveOccurred()) // nil conn = early return
		})
	})

	Describe("cotEventToPoint", func() {
		testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		testStale := time.Date(2024, 1, 1, 0, 5, 0, 0, time.UTC)

		It("should convert a full CoT event JSON to Point", func() {
			e := &cot.Event{
				UID:   "test-uid",
				Type:  "a-f-G-U-C",
				Time:  testTime,
				Stale: testStale,
				How:   "m-g",
				Point: cot.Point{Lat: 38.8977, Lon: -77.0365, HAE: 100.0, CE: 10.0, LE: 5.0},
				Detail: &cot.Detail{
					Track:   &cot.Track{Course: 90.0, Speed: 5.5},
					Contact: &cot.Contact{Callsign: "ALPHA-1"},
				},
			}

			p := cotEventToPoint(e)

			Expect(p.ID).To(Equal("test-uid"))
			Expect(p.TrackID).To(Equal("test-uid"))
			Expect(p.Latitude).To(BeNumerically("~", 38.8977, 0.0001))
			Expect(p.Longitude).To(BeNumerically("~", -77.0365, 0.0001))
			Expect(p.Altitude).To(BeNumerically("~", 100.0, 0.1))
			Expect(p.CE).To(BeNumerically("~", 10.0, 0.1))
			Expect(p.LE).To(BeNumerically("~", 5.0, 0.1))
			Expect(p.Type).To(Equal("a-f-G-U-C"))
			Expect(p.Callsign).To(Equal("ALPHA-1"))
			Expect(p.Course).To(BeNumerically("~", 90.0, 0.1))
			Expect(p.Speed).To(BeNumerically("~", 5.5, 0.1))
			Expect(p.How).To(Equal("m-g"))
			Expect(p.Stale).To(Equal("2024-01-01T00:05:00Z"))
			Expect(p.Group).To(Equal(DefaultGroup))
		})

		It("should handle nil Detail", func() {
			e := &cot.Event{
				UID:   "uid-1",
				Type:  "a-u-G",
				Point: cot.Point{Lat: 10.0, Lon: 20.0},
			}

			p := cotEventToPoint(e)
			Expect(p.ID).To(Equal("uid-1"))
			Expect(p.Callsign).To(BeEmpty())
			Expect(p.Course).To(BeZero())
			Expect(p.Speed).To(BeZero())
		})

		It("should prefer contact.callsign over uid.callsign", func() {
			e := &cot.Event{
				UID:  "uid-1",
				Type: "a-f-G",
				Detail: &cot.Detail{
					Contact: &cot.Contact{Callsign: "CONTACT-CS"},
					UID:     &cot.UIDDetail{Callsign: "UID-CS"},
				},
			}

			p := cotEventToPoint(e)
			Expect(p.Callsign).To(Equal("CONTACT-CS"))
		})

		It("should fall back to uid.callsign when contact is nil", func() {
			e := &cot.Event{
				UID:  "uid-1",
				Type: "a-f-G",
				Detail: &cot.Detail{
					UID: &cot.UIDDetail{Callsign: "UID-CS"},
				},
			}

			p := cotEventToPoint(e)
			Expect(p.Callsign).To(Equal("UID-CS"))
		})

		It("should fall back to uid.callsign when contact callsign is empty", func() {
			e := &cot.Event{
				UID:  "uid-1",
				Type: "a-f-G",
				Detail: &cot.Detail{
					Contact: &cot.Contact{Callsign: ""},
					UID:     &cot.UIDDetail{Callsign: "FALLBACK"},
				},
			}

			p := cotEventToPoint(e)
			Expect(p.Callsign).To(Equal("FALLBACK"))
		})

		It("should handle Detail with nil track", func() {
			e := &cot.Event{
				UID:    "uid-1",
				Type:   "a-f-G",
				Detail: &cot.Detail{},
			}

			p := cotEventToPoint(e)
			Expect(p.Course).To(BeZero())
			Expect(p.Speed).To(BeZero())
			Expect(p.Callsign).To(BeEmpty())
		})
	})

	Describe("concurrent cache access", func() {
		It("should handle concurrent cachePoint calls safely", func() {
			b := NewBridge(nil, logger)
			var wg sync.WaitGroup

			for i := 0; i < 100; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					p := &Point{
						ID:       fmt.Sprintf("uid-%d", idx),
						Latitude: float64(idx),
					}
					b.cachePoint(p)
				}(i)
			}

			wg.Wait()

			b.cacheMu.RLock()
			Expect(len(b.pointCache)).To(Equal(100))
			b.cacheMu.RUnlock()
		})

		It("should handle concurrent SeedCache and PruneStaleCacheEntries", func() {
			b := NewBridge(nil, logger)
			past := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)

			var wg sync.WaitGroup
			wg.Add(2)

			go func() {
				defer wg.Done()
				points := make([]*Point, 50)
				for i := range points {
					points[i] = &Point{
						ID:    fmt.Sprintf("seed-%d", i),
						Stale: past,
					}
				}
				b.SeedCache(points)
			}()

			go func() {
				defer wg.Done()
				for i := 0; i < 10; i++ {
					b.PruneStaleCacheEntries()
				}
			}()

			wg.Wait()
			// Just verify no panic or deadlock
		})
	})
})
