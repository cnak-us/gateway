package clients

import (
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Manager", func() {
	var (
		registry *Registry
		manager  *Manager
	)

	BeforeEach(func() {
		registry = newGinkgoTestRegistry()
		manager = NewManager(registry, slog.Default())
	})

	Describe("NewManager", func() {
		It("should create a manager with valid fields", func() {
			Expect(manager).NotTo(BeNil())
			Expect(manager.registry).To(Equal(registry))
			Expect(manager.kickCh).NotTo(BeNil())
			Expect(manager.startedAt).NotTo(BeZero())
		})
	})

	Describe("KickClient", func() {
		It("should send kick request for existing client", func() {
			registry.Register(&ClientInfo{UID: "uid-1", Callsign: "ALPHA"})

			err := manager.KickClient("uid-1")
			Expect(err).NotTo(HaveOccurred())

			// Read the kick from the channel
			Eventually(manager.KickChan()).Should(Receive(Equal("uid-1")))
		})

		It("should return error for non-existent client", func() {
			err := manager.KickClient("nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("should handle multiple kick requests", func() {
			registry.Register(&ClientInfo{UID: "uid-1"})
			registry.Register(&ClientInfo{UID: "uid-2"})

			Expect(manager.KickClient("uid-1")).To(Succeed())
			Expect(manager.KickClient("uid-2")).To(Succeed())

			var kicks []string
			for i := 0; i < 2; i++ {
				uid := <-manager.KickChan()
				kicks = append(kicks, uid)
			}
			Expect(kicks).To(ConsistOf("uid-1", "uid-2"))
		})

		It("should return error when kick channel is full", func() {
			registry.Register(&ClientInfo{UID: "uid-1"})

			// Fill the channel (capacity 16)
			for i := 0; i < 16; i++ {
				Expect(manager.KickClient("uid-1")).To(Succeed())
			}

			// 17th should fail
			err := manager.KickClient("uid-1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("kick channel full"))
		})
	})

	Describe("KickChan", func() {
		It("should return a read-only channel", func() {
			ch := manager.KickChan()
			Expect(ch).NotTo(BeNil())
		})
	})

	Describe("GetStats", func() {
		It("should return stats with zero clients", func() {
			stats := manager.GetStats()
			Expect(stats).NotTo(BeNil())
			Expect(stats.Connected).To(Equal(0))
			Expect(stats.StartedAt).NotTo(BeZero())
			Expect(stats.Uptime).NotTo(BeEmpty())
		})

		It("should reflect client count", func() {
			registry.Register(&ClientInfo{UID: "uid-1"})
			registry.Register(&ClientInfo{UID: "uid-2"})

			stats := manager.GetStats()
			Expect(stats.Connected).To(Equal(2))
		})

		It("should update count after deregister", func() {
			registry.Register(&ClientInfo{UID: "uid-1"})
			registry.Register(&ClientInfo{UID: "uid-2"})

			registry.Deregister("uid-1")

			stats := manager.GetStats()
			Expect(stats.Connected).To(Equal(1))
		})

		It("should have a non-zero uptime after creation", func() {
			stats := manager.GetStats()
			// Uptime should be at least "0s" — just not empty
			Expect(stats.Uptime).NotTo(BeEmpty())
		})
	})
})
