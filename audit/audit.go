package audit

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// AuditEvent matches the backend store.AuditLog JSON schema so events
// published by tak-gateway are displayed in the frontend audit view.
type AuditEvent struct {
	ID           string    `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	UserID       string    `json:"userId,omitempty"`
	Username     string    `json:"username"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resourceType"`
	ResourceID   string    `json:"resourceId,omitempty"`
	ResourceName string    `json:"resourceName,omitempty"`
	Details      string    `json:"details,omitempty"`
	IPAddress    string    `json:"ipAddress,omitempty"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
	Source       string    `json:"source,omitempty"`
}

// Auditor publishes audit events to NATS and logs them via slog.
type Auditor struct {
	nc     *nats.Conn
	logger *slog.Logger
}

// NewAuditor creates an Auditor. If nc is nil, events are only logged locally.
func NewAuditor(nc *nats.Conn, logger *slog.Logger) *Auditor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Auditor{nc: nc, logger: logger}
}

// Log publishes an audit event to NATS and logs it via slog.
func (a *Auditor) Log(event AuditEvent) {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Source == "" {
		event.Source = "tak-gateway"
	}
	if event.Status == "" {
		event.Status = "success"
	}

	a.logger.Info("audit event",
		"action", event.Action,
		"resourceType", event.ResourceType,
		"resourceName", event.ResourceName,
		"username", event.Username,
		"ip", event.IPAddress,
		"status", event.Status,
	)

	if a.nc != nil && a.nc.IsConnected() {
		subject := "audit." + event.ResourceType + "." + event.Action
		if data, err := json.Marshal(event); err == nil {
			if err := a.nc.Publish(subject, data); err != nil {
				a.logger.Error(fmt.Sprintf("failed to publish audit event: %v", err))
			}
		}
	}
}

// LogCertCreated records the creation of a client certificate.
func (a *Auditor) LogCertCreated(actor, username, ip string) {
	a.Log(AuditEvent{
		Username:     actor,
		Action:       "create",
		ResourceType: "certificate",
		ResourceName: username,
		IPAddress:    ip,
	})
}

// LogCertEnrolled records a client certificate enrollment.
func (a *Auditor) LogCertEnrolled(actor, certCN, clientUID, ip string) {
	a.Log(AuditEvent{
		Username:     actor,
		Action:       "enroll",
		ResourceType: "certificate",
		ResourceID:   clientUID,
		ResourceName: certCN,
		IPAddress:    ip,
	})
}

// LogCertRevoked records the revocation of a client certificate.
func (a *Auditor) LogCertRevoked(actor, certCN, ip string) {
	a.Log(AuditEvent{
		Username:     actor,
		Action:       "revoke",
		ResourceType: "certificate",
		ResourceName: certCN,
		IPAddress:    ip,
	})
}

// LogClientKicked records forcibly disconnecting a client.
func (a *Auditor) LogClientKicked(actor, clientUID, callsign, ip string) {
	a.Log(AuditEvent{
		Username:     actor,
		Action:       "kick",
		ResourceType: "client",
		ResourceID:   clientUID,
		ResourceName: callsign,
		IPAddress:    ip,
	})
}

// LogDataPackageGenerated records the generation of a data package.
func (a *Auditor) LogDataPackageGenerated(actor, hostname, ip string) {
	a.Log(AuditEvent{
		Username:     actor,
		Action:       "generate",
		ResourceType: "data-package",
		ResourceName: hostname,
		IPAddress:    ip,
	})
}

// LogTrustStoreDownloaded records a trust store download.
func (a *Auditor) LogTrustStoreDownloaded(actor, ip string) {
	a.Log(AuditEvent{
		Username:     actor,
		Action:       "download",
		ResourceType: "truststore",
		IPAddress:    ip,
	})
}

// LogQRCodeGenerated records the generation of an enrollment QR code.
func (a *Auditor) LogQRCodeGenerated(actor, username, hostname, ip string) {
	a.Log(AuditEvent{
		Username:     actor,
		Action:       "generate",
		ResourceType: "qrcode",
		ResourceName: username,
		Details:      "hostname=" + hostname,
		IPAddress:    ip,
	})
}

// LogClientConnected records a TAK client connection.
func (a *Auditor) LogClientConnected(certCN, clientUID, ip string) {
	a.Log(AuditEvent{
		Username:     certCN,
		Action:       "connect",
		ResourceType: "client",
		ResourceID:   clientUID,
		ResourceName: certCN,
		IPAddress:    ip,
	})
}

// LogClientDisconnected records a TAK client disconnection.
func (a *Auditor) LogClientDisconnected(certCN, clientUID, reason string) {
	a.Log(AuditEvent{
		Username:     certCN,
		Action:       "disconnect",
		ResourceType: "client",
		ResourceID:   clientUID,
		ResourceName: certCN,
		Details:      reason,
	})
}
