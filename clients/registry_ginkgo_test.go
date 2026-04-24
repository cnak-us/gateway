package clients

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// newGinkgoTestRegistry creates a Registry in memory-only mode (no NATS KV).
func newGinkgoTestRegistry() *Registry {
	return &Registry{
		podName: "test-pod",
		local:   make(map[string]*ClientInfo),
		logger:  slog.Default(),
	}
}

var _ = Describe("Registry", func() {

	Describe("Register", func() {
		It("should register a client successfully", func() {
			r := newGinkgoTestRegistry()
			info := &ClientInfo{
				UID:      "uid-1",
				Callsign: "ALPHA-1",
				IP:       "10.0.0.1",
			}

			err := r.Register(info)
			Expect(err).NotTo(HaveOccurred())

			r.mu.RLock()
			Expect(r.local).To(HaveKey("uid-1"))
			Expect(r.local["uid-1"].PodName).To(Equal("test-pod"))
			r.mu.RUnlock()
		})

		It("should overwrite existing client on re-register", func() {
			r := newGinkgoTestRegistry()
			err := r.Register(&ClientInfo{UID: "uid-1", Callsign: "OLD"})
			Expect(err).NotTo(HaveOccurred())

			err = r.Register(&ClientInfo{UID: "uid-1", Callsign: "NEW"})
			Expect(err).NotTo(HaveOccurred())

			r.mu.RLock()
			Expect(r.local["uid-1"].Callsign).To(Equal("NEW"))
			r.mu.RUnlock()
		})

		It("should set PodName from registry", func() {
			r := newGinkgoTestRegistry()
			info := &ClientInfo{UID: "uid-1", PodName: "other-pod"}
			err := r.Register(info)
			Expect(err).NotTo(HaveOccurred())

			r.mu.RLock()
			Expect(r.local["uid-1"].PodName).To(Equal("test-pod"))
			r.mu.RUnlock()
		})
	})

	Describe("Deregister", func() {
		It("should remove a registered client", func() {
			r := newGinkgoTestRegistry()
			r.Register(&ClientInfo{UID: "uid-1"})

			err := r.Deregister("uid-1")
			Expect(err).NotTo(HaveOccurred())

			r.mu.RLock()
			Expect(r.local).NotTo(HaveKey("uid-1"))
			r.mu.RUnlock()
		})

		It("should be a no-op for non-existent client", func() {
			r := newGinkgoTestRegistry()
			err := r.Deregister("nonexistent")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("GetClient", func() {
		It("should retrieve a registered client", func() {
			r := newGinkgoTestRegistry()
			r.Register(&ClientInfo{UID: "uid-1", Callsign: "ALPHA"})

			client, err := r.GetClient("uid-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(client.Callsign).To(Equal("ALPHA"))
		})

		It("should return error for non-existent client", func() {
			r := newGinkgoTestRegistry()
			_, err := r.GetClient("missing")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Describe("UpdateLastSeen", func() {
		It("should update the LastSeen timestamp", func() {
			r := newGinkgoTestRegistry()
			r.Register(&ClientInfo{UID: "uid-1", LastSeen: time.Now().Add(-1 * time.Hour)})

			before := time.Now().Add(-1 * time.Second)
			err := r.UpdateLastSeen("uid-1")
			Expect(err).NotTo(HaveOccurred())

			r.mu.RLock()
			Expect(r.local["uid-1"].LastSeen).To(BeTemporally(">", before))
			r.mu.RUnlock()
		})

		It("should return error for unknown client", func() {
			r := newGinkgoTestRegistry()
			err := r.UpdateLastSeen("unknown")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found locally"))
		})
	})

	Describe("ListLocal", func() {
		It("should list all local clients", func() {
			r := newGinkgoTestRegistry()
			r.Register(&ClientInfo{UID: "uid-1"})
			r.Register(&ClientInfo{UID: "uid-2"})
			r.Register(&ClientInfo{UID: "uid-3"})

			clients := r.ListLocal()
			Expect(clients).To(HaveLen(3))
		})

		It("should return empty slice for empty registry", func() {
			r := newGinkgoTestRegistry()
			clients := r.ListLocal()
			Expect(clients).To(BeEmpty())
		})
	})

	Describe("ListAll", func() {
		It("should return local clients when no KV store", func() {
			r := newGinkgoTestRegistry()
			r.Register(&ClientInfo{UID: "uid-1"})
			r.Register(&ClientInfo{UID: "uid-2"})

			clients, err := r.ListAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(clients).To(HaveLen(2))
		})

		It("should return empty list for empty registry", func() {
			r := newGinkgoTestRegistry()
			clients, err := r.ListAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(clients).To(BeEmpty())
		})
	})

	Describe("Count", func() {
		It("should return number of local clients", func() {
			r := newGinkgoTestRegistry()
			Expect(r.Count()).To(Equal(0))

			r.Register(&ClientInfo{UID: "uid-1"})
			Expect(r.Count()).To(Equal(1))

			r.Register(&ClientInfo{UID: "uid-2"})
			Expect(r.Count()).To(Equal(2))

			r.Deregister("uid-1")
			Expect(r.Count()).To(Equal(1))
		})
	})

	Describe("concurrent access", func() {
		It("should handle concurrent register/deregister safely", func() {
			r := newGinkgoTestRegistry()
			var wg sync.WaitGroup

			// Register 100 clients concurrently
			for i := 0; i < 100; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					r.Register(&ClientInfo{UID: fmt.Sprintf("uid-%d", idx)})
				}(i)
			}
			wg.Wait()

			Expect(r.Count()).To(Equal(100))

			// Deregister 50 concurrently
			for i := 0; i < 50; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					r.Deregister(fmt.Sprintf("uid-%d", idx))
				}(i)
			}
			wg.Wait()

			Expect(r.Count()).To(Equal(50))
		})

		It("should handle concurrent register and ListLocal safely", func() {
			r := newGinkgoTestRegistry()
			var wg sync.WaitGroup

			wg.Add(2)
			go func() {
				defer wg.Done()
				for i := 0; i < 50; i++ {
					r.Register(&ClientInfo{UID: fmt.Sprintf("uid-%d", i)})
				}
			}()

			go func() {
				defer wg.Done()
				for i := 0; i < 50; i++ {
					r.ListLocal()
				}
			}()

			wg.Wait()
			// No panic or data race
		})

		It("should handle concurrent UpdateLastSeen calls", func() {
			r := newGinkgoTestRegistry()
			r.Register(&ClientInfo{UID: "uid-1"})

			var wg sync.WaitGroup
			for i := 0; i < 50; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					r.UpdateLastSeen("uid-1")
				}()
			}
			wg.Wait()
		})
	})

	Describe("ClientInfo fields", func() {
		It("should preserve all fields through register/get", func() {
			r := newGinkgoTestRegistry()
			now := time.Now()
			info := &ClientInfo{
				UID:             "uid-1",
				Callsign:        "ALPHA-1",
				IP:              "192.168.1.100",
				CertCN:          "client-cert-cn",
				ConnectedAt:     now,
				LastSeen:        now,
				ProtocolVersion: "1",
				Groups:          []string{"ALPHA", "BRAVO"},
			}

			err := r.Register(info)
			Expect(err).NotTo(HaveOccurred())

			got, err := r.GetClient("uid-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.UID).To(Equal("uid-1"))
			Expect(got.Callsign).To(Equal("ALPHA-1"))
			Expect(got.IP).To(Equal("192.168.1.100"))
			Expect(got.CertCN).To(Equal("client-cert-cn"))
			Expect(got.PodName).To(Equal("test-pod"))
			Expect(got.ProtocolVersion).To(Equal("1"))
			Expect(got.Groups).To(ConsistOf("ALPHA", "BRAVO"))
		})
	})
})
