package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Connection metrics
	ConnectedClients = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tak_gateway",
			Name:      "connected_clients",
			Help:      "Number of currently connected TAK clients",
		},
	)

	ConnectionsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "tak_gateway",
			Name:      "connections_total",
			Help:      "Total connections accepted",
		},
	)

	ConnectionErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "tak_gateway",
			Name:      "connection_errors_total",
			Help:      "Total connection errors",
		},
	)

	// Message metrics
	COTMessagesReceived = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "tak_gateway",
			Name:      "cot_messages_received_total",
			Help:      "Total CoT messages received from TAK clients",
		},
	)

	COTMessagesSent = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "tak_gateway",
			Name:      "cot_messages_sent_total",
			Help:      "Total CoT messages sent/relayed to TAK clients",
		},
	)

	MessageLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "tak_gateway",
			Name:      "message_latency_seconds",
			Help:      "CoT message processing latency",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		},
	)

	// Certificate metrics
	EnrollmentRequests = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "tak_gateway",
			Name:      "enrollment_requests_total",
			Help:      "Total certificate enrollment requests",
		},
	)

	CertificatesIssued = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "tak_gateway",
			Name:      "certificates_issued_total",
			Help:      "Total certificates issued",
		},
	)

	CertificatesRevoked = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "tak_gateway",
			Name:      "certificates_revoked_total",
			Help:      "Total certificates revoked",
		},
	)
)

// Handler returns the Prometheus metrics HTTP handler.
func Handler() http.Handler {
	return promhttp.Handler()
}
