package protocol

import (
	"bytes"
	"fmt"
	"time"
)

// TAK Protocol negotiation event types.
const (
	TypeTAKPVersion  = "t-x-takp-v" // Server → Client: version announcement
	TypeTAKPRequest  = "t-x-takp-q" // Client → Server: version request
	TypeTAKPResponse = "t-x-takp-r" // Server → Client: version confirmation
)

// BuildVersionAnnouncement returns an XML CoT event announcing supported TAK Protocol versions.
// This is the first message sent to a new client on TLS streaming connections.
func BuildVersionAnnouncement() []byte {
	now := time.Now().UTC()
	stale := now.Add(60 * time.Second)
	nowStr := now.Format(time.RFC3339)
	staleStr := stale.Format(time.RFC3339)

	return []byte(fmt.Sprintf(
		`<?xml version="1.0" encoding="UTF-8"?>`+
			`<event version="2.0" uid="takserver-version" type="%s" how="h-e" time="%s" start="%s" stale="%s">`+
			`<point lat="0" lon="0" hae="0" ce="999999" le="999999"/>`+
			`<detail>`+
			`<TakControl>`+
			`<TakProtocolSupport version="1"/>`+
			`<TakServerVersionInfo serverVersion="CNAK" apiVersion="3"/>`+
			`</TakControl>`+
			`</detail>`+
			`</event>`,
		TypeTAKPVersion, nowStr, nowStr, staleStr,
	))
}

// BuildVersionResponse returns an XML CoT event confirming (or denying) a requested protocol version.
func BuildVersionResponse(requestedVersion string, supported bool) []byte {
	now := time.Now().UTC()
	stale := now.Add(60 * time.Second)
	nowStr := now.Format(time.RFC3339)
	staleStr := stale.Format(time.RFC3339)

	statusStr := "true"
	if !supported {
		statusStr = "false"
	}

	return []byte(fmt.Sprintf(
		`<?xml version="1.0" encoding="UTF-8"?>`+
			`<event version="2.0" uid="takserver-version" type="%s" how="h-e" time="%s" start="%s" stale="%s">`+
			`<point lat="0" lon="0" hae="0" ce="999999" le="999999"/>`+
			`<detail>`+
			`<TakControl>`+
			`<TakProtocolSupport version="%s" status="%s"/>`+
			`</TakControl>`+
			`</detail>`+
			`</event>`,
		TypeTAKPResponse, nowStr, nowStr, staleStr, requestedVersion, statusStr,
	))
}

// IsNegotiationEvent checks if an XML CoT event is a TAK Protocol negotiation message
// and returns the event type if so. Returns empty string if not a negotiation event.
func IsNegotiationEvent(data []byte) string {
	for _, typ := range []string{TypeTAKPVersion, TypeTAKPRequest, TypeTAKPResponse} {
		if bytes.Contains(data, []byte(`type="`+typ+`"`)) {
			return typ
		}
	}
	return ""
}

// ExtractRequestedVersion extracts the requested protocol version from a t-x-takp-q event.
// It looks for <TakProtocolSupport version="N"/> in the event XML.
func ExtractRequestedVersion(data []byte) string {
	// Look for TakProtocolSupport version="..." in the request
	marker := []byte(`TakProtocolSupport version="`)
	idx := bytes.Index(data, marker)
	if idx < 0 {
		return ""
	}
	start := idx + len(marker)
	end := bytes.IndexByte(data[start:], '"')
	if end < 0 {
		return ""
	}
	return string(data[start : start+end])
}
