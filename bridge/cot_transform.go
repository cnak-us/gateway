// Package bridge handles bidirectional NATS messaging and CoT format conversion
package bridge

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	cot "github.com/cnak-us/cnak/pkg/cot"
)

const (
	// DefaultGroup is the default TAK group for messages without a group
	DefaultGroup = "__ANON__"
)

// Point represents the CNAK internal point format.
// Must match what the backend MemoryStore expects.
type Point struct {
	ID        string                 `json:"id"`
	TrackID   string                 `json:"trackId"`
	Latitude  float64                `json:"latitude"`
	Longitude float64                `json:"longitude"`
	Altitude  float64                `json:"altitude,omitempty"`
	CE        float64                `json:"ce,omitempty"`
	LE        float64                `json:"le,omitempty"`
	Speed     float64                `json:"speed,omitempty"`
	Course    float64                `json:"course,omitempty"`
	Type      string                 `json:"type"`
	Callsign  string                 `json:"callsign,omitempty"`
	Group     string                 `json:"group,omitempty"`
	Timestamp string                 `json:"timestamp"`
	How       string                 `json:"how,omitempty"`
	Stale     string                 `json:"stale,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// xmlGroup represents the TAK-specific __group element (not parsed by pkg/cot)
type xmlGroup struct {
	Name string `xml:"name,attr"`
}

// xmlGroupExtractor is a minimal struct to extract just the __group from CoT XML
type xmlGroupExtractor struct {
	XMLName xml.Name `xml:"event"`
	Detail  *struct {
		Group *xmlGroup `xml:"__group"`
	} `xml:"detail"`
}

// CotXMLToPoint parses CoT XML and extracts fields into a Point struct.
func CotXMLToPoint(cotXML []byte) (*Point, error) {
	event, err := cot.FromXMLBytes(cotXML)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CoT XML: %w", err)
	}

	p := &Point{
		ID:        event.UID,
		TrackID:   event.UID,
		Latitude:  event.Point.Lat,
		Longitude: event.Point.Lon,
		Altitude:  event.Point.HAE,
		CE:        event.Point.CE,
		LE:        event.Point.LE,
		Type:      event.Type,
		Timestamp: event.Time.UTC().Format(time.RFC3339),
		How:       event.How,
		Stale:     event.Stale.UTC().Format(time.RFC3339),
		Group:     DefaultGroup,
	}

	if event.Detail != nil {
		if event.Detail.Contact != nil && event.Detail.Contact.Callsign != "" {
			p.Callsign = event.Detail.Contact.Callsign
		}
		if event.Detail.Track != nil {
			p.Course = event.Detail.Track.Course
			p.Speed = event.Detail.Track.Speed
		}
		if event.Detail.Health != nil {
			h := event.Detail.Health
			meta := make(map[string]interface{})
			if h.HeartRate > 0 {
				meta["heartRate"] = h.HeartRate
			}
			if h.SpO2 > 0 {
				meta["spo2"] = h.SpO2
			}
			if h.Stress > 0 {
				meta["stress"] = h.Stress
			}
			if h.BodyBattery > 0 {
				meta["bodyBattery"] = h.BodyBattery
			}
			if h.Temperature > 0 {
				meta["temperature"] = h.Temperature
			}
			if h.RespirationRate > 0 {
				meta["respirationRate"] = h.RespirationRate
			}
			if len(meta) > 0 {
				p.Metadata = meta
			}
		}
	}

	// __group is not parsed by pkg/cot, extract separately
	group := ExtractGroupFromCoT(cotXML)
	if group != DefaultGroup {
		p.Group = group
	}

	return p, nil
}

// ExtractGroupFromCoT extracts just the group name from CoT XML.
func ExtractGroupFromCoT(cotXML []byte) string {
	var xe xmlGroupExtractor
	if err := xml.Unmarshal(cotXML, &xe); err != nil {
		return DefaultGroup
	}
	if xe.Detail != nil && xe.Detail.Group != nil && xe.Detail.Group.Name != "" {
		return xe.Detail.Group.Name
	}
	return DefaultGroup
}

// ExtractUIDFromCoT extracts just the UID from CoT XML.
func ExtractUIDFromCoT(cotXML []byte) string {
	event, err := cot.FromXMLBytes(cotXML)
	if err != nil {
		return ""
	}
	return event.UID
}

// ExtractCallsignFromCoT extracts just the callsign from CoT XML.
func ExtractCallsignFromCoT(cotXML []byte) string {
	event, err := cot.FromXMLBytes(cotXML)
	if err != nil {
		return ""
	}
	if event.Detail != nil && event.Detail.Contact != nil {
		return event.Detail.Contact.Callsign
	}
	return ""
}
// PointToCoTXML converts a Point back to CoT XML for relay to TAK clients.
func PointToCoTXML(point *Point) ([]byte, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	timestamp := point.Timestamp
	if timestamp == "" {
		timestamp = now
	}

	stale := point.Stale
	if stale == "" {
		// Default 5 minutes from timestamp
		if t, err := time.Parse(time.RFC3339, timestamp); err == nil {
			stale = t.Add(5 * time.Minute).Format(time.RFC3339)
		} else {
			stale = time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)
		}
	}

	how := point.How
	if how == "" {
		how = "m-g"
	}

	cotType := point.Type
	if cotType == "" {
		cotType = "a-u-G"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		`<event version="2.0" uid="%s" type="%s" time="%s" start="%s" stale="%s" how="%s">`,
		cot.EscapeXML(point.ID), cot.EscapeXML(cotType), timestamp, timestamp, stale, cot.EscapeXML(how),
	))

	sb.WriteString(fmt.Sprintf(
		`<point lat="%.6f" lon="%.6f" hae="%.6f" ce="%.6f" le="%.6f"/>`,
		point.Latitude, point.Longitude, point.Altitude, point.CE, point.LE,
	))

	sb.WriteString("<detail>")
	if point.Callsign != "" {
		sb.WriteString(fmt.Sprintf(`<contact callsign="%s"/>`, cot.EscapeXML(point.Callsign)))
	}
	if point.Course != 0 || point.Speed != 0 {
		sb.WriteString(fmt.Sprintf(`<track course="%.1f" speed="%.1f"/>`, point.Course, point.Speed))
	}
	// Include __health if metadata contains health fields
	if point.Metadata != nil {
		hr, hasHR := point.Metadata["heartRate"]
		spo2, hasSpo2 := point.Metadata["spo2"]
		stress, hasStress := point.Metadata["stress"]
		bb, hasBB := point.Metadata["bodyBattery"]
		temp, hasTemp := point.Metadata["temperature"]
		resp, hasResp := point.Metadata["respirationRate"]
		if hasHR || hasSpo2 || hasStress || hasBB || hasTemp || hasResp {
			sb.WriteString("<__health")
			if hasHR {
				sb.WriteString(fmt.Sprintf(` heartRate="%v"`, hr))
			}
			if hasSpo2 {
				sb.WriteString(fmt.Sprintf(` spo2="%v"`, spo2))
			}
			if hasStress {
				sb.WriteString(fmt.Sprintf(` stress="%v"`, stress))
			}
			if hasBB {
				sb.WriteString(fmt.Sprintf(` bodyBattery="%v"`, bb))
			}
			if hasTemp {
				sb.WriteString(fmt.Sprintf(` temperature="%v"`, temp))
			}
			if hasResp {
				sb.WriteString(fmt.Sprintf(` respirationRate="%v"`, resp))
			}
			sb.WriteString("/>")
		}
	}
	// Note: __group is intentionally NOT included here. In ATAK, __group with
	// role="Team Member" overrides MIL-STD-2525 symbol rendering with team-color
	// styling. Group membership is a routing concept (used by ShouldRelay), not a
	// CoT display element. Only real SA messages from ATAK clients should carry __group.
	sb.WriteString("</detail>")

	sb.WriteString("</event>")

	return []byte(sb.String()), nil
}
