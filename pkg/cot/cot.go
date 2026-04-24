// Package cot provides Cursor on Target (CoT) message types and encoding.
//
// Based on MITRE CoT Message Router Developer Knowledge Base and MIL-STD-2525D standard.
// Supports both JSON (for internal use) and XML (for CoT protocol) formats.
package cot

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Event represents a Cursor on Target (CoT) event message
//
// Required fields per MITRE CoT spec:
//   - version: Schema version (typically "2.0")
//   - uid: Unique sender-assigned identifier (stable across updates)
//   - type: Hyphen-delimited type code (e.g., "a-f-G-U-C" for friendly ground unit combat)
//   - time: Generation time (ISO-8601 UTC with Z suffix)
//   - start: Event start/valid-from (ISO-8601 UTC with Z suffix)
//   - stale: Expiration/valid-until (ISO-8601 UTC with Z suffix)
//   - point: Geospatial location (required)
//
// Time semantics: time <= start <= stale (stale should be in the future)
type Event struct {
	Version string    `json:"version"`          // Schema version (e.g., "2.0")
	UID     string    `json:"uid"`              // Unique identifier
	Type    string    `json:"type"`             // Hyphen-delimited type code (e.g., "a-f-G-U-C")
	Time    time.Time `json:"time"`             // Generation time (UTC)
	Start   time.Time `json:"start"`            // Event start/valid-from (UTC)
	Stale   time.Time `json:"stale"`            // Expiration/valid-until (UTC)
	How     string    `json:"how,omitempty"`    // Production method (e.g., "m-g", "h-e")
	Access  string    `json:"access,omitempty"` // Access policy
	QoS     string    `json:"qos,omitempty"`    // Quality of service
	Opex    string    `json:"opex,omitempty"`   // Operational context
	Point   Point     `json:"point"`            // Geospatial location (required)
	Detail  *Detail   `json:"detail,omitempty"` // Optional detail sub-schemas
}

// Point represents the geospatial location tied to the event
//
// Per MITRE CoT spec:
//   - lat: Latitude (decimal degrees, WGS-84)
//   - lon: Longitude (decimal degrees, WGS-84)
//   - hae: Height Above Ellipsoid (meters, positive up, WGS-84 ellipsoid)
//   - ce: Circular error / horizontal uncertainty (meters)
//   - le: Linear error / vertical uncertainty (meters)
type Point struct {
	Lat float64 `json:"lat"` // Latitude (decimal degrees, WGS-84)
	Lon float64 `json:"lon"` // Longitude (decimal degrees, WGS-84)
	HAE float64 `json:"hae"` // Height Above Ellipsoid (meters)
	CE  float64 `json:"ce"`  // Circular error / horizontal uncertainty (meters)
	LE  float64 `json:"le"`  // Linear error / vertical uncertainty (meters)
}

// IsPositionUnknown returns true if the point represents an unknown position.
// Per CoT spec, lat=0 lon=0 means "position unknown" (e.g., before GPS fix).
func (p Point) IsPositionUnknown() bool {
	return p.Lat == 0 && p.Lon == 0
}

// Detail contains optional sub-schemas that extend the base event
type Detail struct {
	Track    *Track     `json:"track,omitempty"`       // Kinematics/motion data
	UID      *UIDDetail `json:"uid,omitempty"`         // Alternate identifiers
	Remarks  *Remarks   `json:"remarks,omitempty"`     // Free-text annotations
	FlowTags *FlowTags  `json:"_flow-tags_,omitempty"` // Processing provenance
	Contact  *Contact   `json:"contact,omitempty"`     // Contact information
	Status   *Status    `json:"status,omitempty"`      // Status information
	Sensor   *Sensor    `json:"sensor,omitempty"`      // Sensor information
	Health   *Health    `json:"__health,omitempty"`    // Biometric health data (CNAK extension)
}

// Track conveys platform motion/kinematics
type Track struct {
	Course     float64 `json:"course,omitempty"`     // Course/bearing (degrees, 0-360)
	Speed      float64 `json:"speed,omitempty"`      // Speed (m/s or knots, profile-dependent)
	TrackValid bool    `json:"trackValid,omitempty"` // Whether track data is valid
	SNic       float64 `json:"sNic,omitempty"`       // Speed confidence (optional)
	CNic       float64 `json:"cNic,omitempty"`       // Course confidence (optional)
}

// UIDDetail stores external system identifiers and aliases
type UIDDetail struct {
	Droid    string `json:"droid,omitempty"`    // Droid identifier
	ICD      string `json:"icd,omitempty"`      // ICD identifier
	IMEI     string `json:"imei,omitempty"`     // IMEI identifier
	Callsign string `json:"callsign,omitempty"` // Callsign
	Tail     string `json:"tail,omitempty"`     // Tail number
	Serial   string `json:"serial,omitempty"`   // Serial number
}

// Remarks contains free-text annotations
type Remarks struct {
	Notes []Note `json:"note,omitempty"` // One or more note blocks
}

// Note represents a single remark/note
type Note struct {
	Text   string    `json:"text,omitempty"`   // Note text
	Source string    `json:"source,omitempty"` // Source of the note
	TS     time.Time `json:"ts,omitempty"`     // Timestamp
}

// FlowTags contains breadcrumbs of systems/plugins that touched the event
type FlowTags struct {
	Sources []Source `json:"source,omitempty"` // List of sources
}

// Source represents a system/plugin that processed the event
type Source struct {
	Name string    `json:"name"`           // Source name
	TS   time.Time `json:"ts"`             // Timestamp
	Host string    `json:"host,omitempty"` // Host identifier
	UID  string    `json:"uid,omitempty"`  // Source UID
	Op   string    `json:"op,omitempty"`   // Operation
}

// Contact contains contact information
type Contact struct {
	Callsign string `json:"callsign,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

// Status contains status information
type Status struct {
	Battery   int    `json:"battery,omitempty"`   // Battery level (0-100)
	Readiness string `json:"readiness,omitempty"` // Readiness state
}

// Sensor contains sensor information
type Sensor struct {
	FOV   float64 `json:"fov,omitempty"`   // Field of view (degrees)
	Range float64 `json:"range,omitempty"` // Range (meters)
}

// Health contains biometric/health data from wearable devices (e.g., Garmin).
// Serialized as <__health> in CoT XML, following the ATAK double-underscore
// extension convention for platform-specific detail elements.
type Health struct {
	HeartRate       int     `json:"heartRate,omitempty"`       // Heart rate (BPM)
	SpO2            int     `json:"spo2,omitempty"`            // Blood oxygen saturation (%)
	Stress          int     `json:"stress,omitempty"`          // Stress level (0-100)
	BodyBattery     int     `json:"bodyBattery,omitempty"`     // Body battery / energy level (0-100)
	Temperature     float64 `json:"temperature,omitempty"`     // Skin/body temperature (°C)
	RespirationRate int     `json:"respirationRate,omitempty"` // Respiration rate (breaths/min)
}

// NewEvent creates a new CoT event with required fields.
// Default stale is 5 minutes, how is "m-g" (machine-generated, GPS).
func NewEvent(uid, cotType string, lat, lon float64) *Event {
	now := time.Now().UTC()
	stale := now.Add(5 * time.Minute)

	return &Event{
		Version: "2.0",
		UID:     uid,
		Type:    cotType,
		Time:    now,
		Start:   now,
		Stale:   stale,
		How:     "m-g",
		Point: Point{
			Lat: lat,
			Lon: lon,
			HAE: 0,
			CE:  50,
			LE:  30,
		},
	}
}

// NewEventWithTrack creates a new CoT event with track/kinematics data
func NewEventWithTrack(uid, cotType string, lat, lon, course, speed float64) *Event {
	event := NewEvent(uid, cotType, lat, lon)
	event.Detail = &Detail{
		Track: &Track{
			Course:     course,
			Speed:      speed,
			TrackValid: true,
		},
	}
	return event
}

// SetStale sets the stale time relative to the event time
func (e *Event) SetStale(duration time.Duration) {
	e.Stale = e.Time.Add(duration)
}

// Validate checks that the event conforms to CoT specification requirements
func (e *Event) Validate() error {
	if e.Version == "" {
		return errors.New("version is required")
	}
	if e.UID == "" {
		return errors.New("uid is required")
	}
	if e.Type == "" {
		return errors.New("type is required")
	}
	if e.Time.IsZero() {
		return errors.New("time is required")
	}
	if e.Start.IsZero() {
		return errors.New("start is required")
	}
	if e.Stale.IsZero() {
		return errors.New("stale is required")
	}

	if e.Time.After(e.Start) {
		return errors.New("time must be <= start")
	}
	if e.Start.After(e.Stale) {
		return errors.New("start must be <= stale")
	}
	if !e.Stale.After(time.Now().UTC()) {
		return errors.New("stale should be in the future")
	}

	return nil
}

// SetPoint updates the point location
func (e *Event) SetPoint(lat, lon, hae, ce, le float64) {
	e.Point = Point{
		Lat: lat,
		Lon: lon,
		HAE: hae,
		CE:  ce,
		LE:  le,
	}
}

// SetTrack updates or adds track/kinematics data
func (e *Event) SetTrack(course, speed float64, valid bool) {
	if e.Detail == nil {
		e.Detail = &Detail{}
	}
	if e.Detail.Track == nil {
		e.Detail.Track = &Track{}
	}
	e.Detail.Track.Course = course
	e.Detail.Track.Speed = speed
	e.Detail.Track.TrackValid = valid
}

// SetCallsign sets the callsign in the detail UID
func (e *Event) SetCallsign(callsign string) {
	if e.Detail == nil {
		e.Detail = &Detail{}
	}
	if e.Detail.UID == nil {
		e.Detail.UID = &UIDDetail{}
	}
	e.Detail.UID.Callsign = callsign
}

// AddFlowTag adds a flow tag source to track processing provenance
func (e *Event) AddFlowTag(name string) {
	if e.Detail == nil {
		e.Detail = &Detail{}
	}
	if e.Detail.FlowTags == nil {
		e.Detail.FlowTags = &FlowTags{}
	}
	e.Detail.FlowTags.Sources = append(e.Detail.FlowTags.Sources, Source{
		Name: name,
		TS:   time.Now().UTC(),
	})
}

// SetHealth sets biometric health data on the event
func (e *Event) SetHealth(heartRate, spo2, stress, bodyBattery int) {
	if e.Detail == nil {
		e.Detail = &Detail{}
	}
	e.Detail.Health = &Health{
		HeartRate:   heartRate,
		SpO2:        spo2,
		Stress:      stress,
		BodyBattery: bodyBattery,
	}
}

// AddRemark adds a remark/note to the event
func (e *Event) AddRemark(text string) {
	if e.Detail == nil {
		e.Detail = &Detail{}
	}
	if e.Detail.Remarks == nil {
		e.Detail.Remarks = &Remarks{}
	}
	e.Detail.Remarks.Notes = append(e.Detail.Remarks.Notes, Note{
		Text: text,
		TS:   time.Now().UTC(),
	})
}

// ToJSON converts the event to JSON bytes
func (e *Event) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// FromJSON creates an event from JSON bytes
func FromJSON(data []byte) (*Event, error) {
	var event Event
	err := json.Unmarshal(data, &event)
	return &event, err
}

// ToXML converts the event to XML string
func (e *Event) ToXML() (string, error) {
	var sb strings.Builder

	timeStr := e.Time.UTC().Format(time.RFC3339)
	startStr := e.Start.UTC().Format(time.RFC3339)
	staleStr := e.Stale.UTC().Format(time.RFC3339)

	sb.WriteString(fmt.Sprintf(`<event version="%s" uid="%s" type="%s" time="%s" start="%s" stale="%s"`,
		EscapeXML(e.Version), EscapeXML(e.UID), EscapeXML(e.Type), timeStr, startStr, staleStr))

	if e.How != "" {
		sb.WriteString(fmt.Sprintf(` how="%s"`, EscapeXML(e.How)))
	}
	if e.Access != "" {
		sb.WriteString(fmt.Sprintf(` access="%s"`, EscapeXML(e.Access)))
	}
	if e.QoS != "" {
		sb.WriteString(fmt.Sprintf(` qos="%s"`, EscapeXML(e.QoS)))
	}
	if e.Opex != "" {
		sb.WriteString(fmt.Sprintf(` opex="%s"`, EscapeXML(e.Opex)))
	}
	sb.WriteString(">")

	if e.Detail != nil {
		sb.WriteString("<detail>")

		if e.Detail.Track != nil {
			track := e.Detail.Track
			sb.WriteString(fmt.Sprintf(`<track course="%.6f" speed="%.6f" trackValid="%t"`,
				track.Course, track.Speed, track.TrackValid))
			if track.SNic > 0 {
				sb.WriteString(fmt.Sprintf(` sNic="%.6f"`, track.SNic))
			}
			if track.CNic > 0 {
				sb.WriteString(fmt.Sprintf(` cNic="%.6f"`, track.CNic))
			}
			sb.WriteString("/>")
		}

		if e.Detail.UID != nil {
			uid := e.Detail.UID
			sb.WriteString("<uid")
			if uid.Callsign != "" {
				sb.WriteString(fmt.Sprintf(` callsign="%s"`, EscapeXML(uid.Callsign)))
			}
			if uid.Droid != "" {
				sb.WriteString(fmt.Sprintf(` droid="%s"`, EscapeXML(uid.Droid)))
			}
			if uid.ICD != "" {
				sb.WriteString(fmt.Sprintf(` icd="%s"`, EscapeXML(uid.ICD)))
			}
			if uid.IMEI != "" {
				sb.WriteString(fmt.Sprintf(` imei="%s"`, EscapeXML(uid.IMEI)))
			}
			if uid.Tail != "" {
				sb.WriteString(fmt.Sprintf(` tail="%s"`, EscapeXML(uid.Tail)))
			}
			if uid.Serial != "" {
				sb.WriteString(fmt.Sprintf(` serial="%s"`, EscapeXML(uid.Serial)))
			}
			sb.WriteString("/>")
		}

		if e.Detail.Remarks != nil && len(e.Detail.Remarks.Notes) > 0 {
			sb.WriteString("<remarks>")
			for _, note := range e.Detail.Remarks.Notes {
				sb.WriteString("<note")
				if note.Source != "" {
					sb.WriteString(fmt.Sprintf(` source="%s"`, EscapeXML(note.Source)))
				}
				if !note.TS.IsZero() {
					sb.WriteString(fmt.Sprintf(` ts="%s"`, note.TS.UTC().Format(time.RFC3339)))
				}
				sb.WriteString(">")
				if note.Text != "" {
					sb.WriteString(EscapeXML(note.Text))
				}
				sb.WriteString("</note>")
			}
			sb.WriteString("</remarks>")
		}

		if e.Detail.FlowTags != nil && len(e.Detail.FlowTags.Sources) > 0 {
			sb.WriteString("<_flow-tags_>")
			for _, source := range e.Detail.FlowTags.Sources {
				sb.WriteString(fmt.Sprintf(`<source name="%s" ts="%s"`,
					EscapeXML(source.Name), source.TS.UTC().Format(time.RFC3339)))
				if source.Host != "" {
					sb.WriteString(fmt.Sprintf(` host="%s"`, EscapeXML(source.Host)))
				}
				if source.UID != "" {
					sb.WriteString(fmt.Sprintf(` uid="%s"`, EscapeXML(source.UID)))
				}
				if source.Op != "" {
					sb.WriteString(fmt.Sprintf(` op="%s"`, EscapeXML(source.Op)))
				}
				sb.WriteString("/>")
			}
			sb.WriteString("</_flow-tags_>")
		}

		if e.Detail.Contact != nil {
			contact := e.Detail.Contact
			sb.WriteString("<contact")
			if contact.Callsign != "" {
				sb.WriteString(fmt.Sprintf(` callsign="%s"`, EscapeXML(contact.Callsign)))
			}
			if contact.Endpoint != "" {
				sb.WriteString(fmt.Sprintf(` endpoint="%s"`, EscapeXML(contact.Endpoint)))
			}
			sb.WriteString("/>")
		}

		if e.Detail.Status != nil {
			status := e.Detail.Status
			sb.WriteString("<status")
			if status.Battery > 0 {
				sb.WriteString(fmt.Sprintf(` battery="%d"`, status.Battery))
			}
			if status.Readiness != "" {
				sb.WriteString(fmt.Sprintf(` readiness="%s"`, EscapeXML(status.Readiness)))
			}
			sb.WriteString("/>")
		}

		if e.Detail.Sensor != nil {
			sensor := e.Detail.Sensor
			sb.WriteString("<sensor")
			if sensor.FOV > 0 {
				sb.WriteString(fmt.Sprintf(` fov="%.6f"`, sensor.FOV))
			}
			if sensor.Range > 0 {
				sb.WriteString(fmt.Sprintf(` range="%.6f"`, sensor.Range))
			}
			sb.WriteString("/>")
		}

		if e.Detail.Health != nil {
			h := e.Detail.Health
			sb.WriteString("<__health")
			if h.HeartRate > 0 {
				sb.WriteString(fmt.Sprintf(` heartRate="%d"`, h.HeartRate))
			}
			if h.SpO2 > 0 {
				sb.WriteString(fmt.Sprintf(` spo2="%d"`, h.SpO2))
			}
			if h.Stress > 0 {
				sb.WriteString(fmt.Sprintf(` stress="%d"`, h.Stress))
			}
			if h.BodyBattery > 0 {
				sb.WriteString(fmt.Sprintf(` bodyBattery="%d"`, h.BodyBattery))
			}
			if h.Temperature > 0 {
				sb.WriteString(fmt.Sprintf(` temperature="%.1f"`, h.Temperature))
			}
			if h.RespirationRate > 0 {
				sb.WriteString(fmt.Sprintf(` respirationRate="%d"`, h.RespirationRate))
			}
			sb.WriteString("/>")
		}

		sb.WriteString("</detail>")
	}

	sb.WriteString(fmt.Sprintf(`<point lat="%.6f" lon="%.6f" hae="%.6f" ce="%.6f" le="%.6f"/>`,
		e.Point.Lat, e.Point.Lon, e.Point.HAE, e.Point.CE, e.Point.LE))

	sb.WriteString("</event>")

	return sb.String(), nil
}

// EscapeXML escapes XML special characters in attribute values and text content.
func EscapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// Common CoT Type codes (MIL-STD-2525 derived)
const (
	TypeFriendlyGroundUnitCombat    = "a-f-G-U-C"
	TypeFriendlyGroundUnitNonCombat = "a-f-G-U-N"
	TypeFriendlyAirFixedWing        = "a-f-A-M-F"
	TypeFriendlyAirRotaryWing       = "a-f-A-M-H"
	TypeHostileGroundUnit           = "a-h-G-U-C"
	TypeHostileAirUnit              = "a-h-A-M-F"
	TypeUnknownGroundUnit           = "a-u-G-U-C"
	TypeUnknownAirUnit              = "a-u-A-M-F"
	TypeGenericTrack                = "a-u-G-U-U-S-R-S"
)

// Common "how" values (production method)
const (
	HowMachineGPS      = "m-g"
	HowHumanEstimated  = "h-e"
	HowHumanGPS        = "h-g"
	HowMachineInertial = "m-i"
)

// ParseError provides context about parsing failures
type ParseError struct {
	Field   string
	Value   string
	Message string
}

func (e *ParseError) Error() string {
	if e.Value != "" {
		return fmt.Sprintf("cot: failed to parse %s (%q): %s", e.Field, e.Value, e.Message)
	}
	return fmt.Sprintf("cot: failed to parse %s: %s", e.Field, e.Message)
}

// XML parsing structures for CoT messages

type xmlEvent struct {
	XMLName xml.Name   `xml:"event"`
	Version string     `xml:"version,attr"`
	UID     string     `xml:"uid,attr"`
	Type    string     `xml:"type,attr"`
	Time    string     `xml:"time,attr"`
	Start   string     `xml:"start,attr"`
	Stale   string     `xml:"stale,attr"`
	How     string     `xml:"how,attr"`
	Access  string     `xml:"access,attr"`
	QoS     string     `xml:"qos,attr"`
	Opex    string     `xml:"opex,attr"`
	Point   xmlPoint   `xml:"point"`
	Detail  *xmlDetail `xml:"detail"`
}

type xmlPoint struct {
	Lat string `xml:"lat,attr"`
	Lon string `xml:"lon,attr"`
	HAE string `xml:"hae,attr"`
	CE  string `xml:"ce,attr"`
	LE  string `xml:"le,attr"`
}

type xmlDetail struct {
	Track    *xmlTrack    `xml:"track"`
	UID      *xmlUID      `xml:"uid"`
	Remarks  *xmlRemarks  `xml:"remarks"`
	FlowTags *xmlFlowTags `xml:"_flow-tags_"`
	Contact  *xmlContact  `xml:"contact"`
	Status   *xmlStatus   `xml:"status"`
	Sensor   *xmlSensor   `xml:"sensor"`
	Health   *xmlHealth   `xml:"__health"`
}

type xmlTrack struct {
	Course     string `xml:"course,attr"`
	Speed      string `xml:"speed,attr"`
	TrackValid string `xml:"trackValid,attr"`
	SNic       string `xml:"sNic,attr"`
	CNic       string `xml:"cNic,attr"`
}

type xmlUID struct {
	Callsign string `xml:"callsign,attr"`
	Droid    string `xml:"droid,attr"`
	ICD      string `xml:"icd,attr"`
	IMEI     string `xml:"imei,attr"`
	Tail     string `xml:"tail,attr"`
	Serial   string `xml:"serial,attr"`
}

type xmlRemarks struct {
	Notes []xmlNote `xml:"note"`
}

type xmlNote struct {
	Text   string `xml:",chardata"`
	Source string `xml:"source,attr"`
	TS     string `xml:"ts,attr"`
}

type xmlFlowTags struct {
	Sources []xmlSource `xml:"source"`
}

type xmlSource struct {
	Name string `xml:"name,attr"`
	TS   string `xml:"ts,attr"`
	Host string `xml:"host,attr"`
	UID  string `xml:"uid,attr"`
	Op   string `xml:"op,attr"`
}

type xmlContact struct {
	Callsign string `xml:"callsign,attr"`
	Endpoint string `xml:"endpoint,attr"`
}

type xmlStatus struct {
	Battery   string `xml:"battery,attr"`
	Readiness string `xml:"readiness,attr"`
}

type xmlSensor struct {
	FOV   string `xml:"fov,attr"`
	Range string `xml:"range,attr"`
}

type xmlHealth struct {
	HeartRate       string `xml:"heartRate,attr"`
	SpO2            string `xml:"spo2,attr"`
	Stress          string `xml:"stress,attr"`
	BodyBattery     string `xml:"bodyBattery,attr"`
	Temperature     string `xml:"temperature,attr"`
	RespirationRate string `xml:"respirationRate,attr"`
}

// Common timestamp formats used in CoT messages
var cotTimeFormats = []string{
	time.RFC3339,
	time.RFC3339Nano,
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05.000Z",
	"2006-01-02T15:04:05.000000Z",
	"2006-01-02T15:04:05-07:00",
	"2006-01-02T15:04:05.000-07:00",
	"2006-01-02T15:04:05",
}

// ParseCoTTime parses a timestamp string using common CoT formats.
func ParseCoTTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty timestamp")
	}

	for _, format := range cotTimeFormats {
		if t, err := time.Parse(format, s); err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("unrecognized timestamp format: %s", s)
}

func parseFloat(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}

func parseInt(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes"
}

// FromXML parses a CoT XML string into an Event
func FromXML(xmlData string) (*Event, error) {
	return FromXMLBytes([]byte(xmlData))
}

// FromXMLBytes parses a CoT XML byte slice into an Event
func FromXMLBytes(xmlData []byte) (*Event, error) {
	var xe xmlEvent
	if err := xml.Unmarshal(xmlData, &xe); err != nil {
		return nil, &ParseError{Field: "xml", Message: err.Error()}
	}

	eventTime, err := ParseCoTTime(xe.Time)
	if err != nil {
		return nil, &ParseError{Field: "time", Value: xe.Time, Message: err.Error()}
	}

	startTime, err := ParseCoTTime(xe.Start)
	if err != nil {
		return nil, &ParseError{Field: "start", Value: xe.Start, Message: err.Error()}
	}

	staleTime, err := ParseCoTTime(xe.Stale)
	if err != nil {
		return nil, &ParseError{Field: "stale", Value: xe.Stale, Message: err.Error()}
	}

	lat, err := parseFloat(xe.Point.Lat)
	if err != nil {
		return nil, &ParseError{Field: "point.lat", Value: xe.Point.Lat, Message: err.Error()}
	}

	lon, err := parseFloat(xe.Point.Lon)
	if err != nil {
		return nil, &ParseError{Field: "point.lon", Value: xe.Point.Lon, Message: err.Error()}
	}

	hae, err := parseFloat(xe.Point.HAE)
	if err != nil {
		return nil, &ParseError{Field: "point.hae", Value: xe.Point.HAE, Message: err.Error()}
	}

	ce, err := parseFloat(xe.Point.CE)
	if err != nil {
		return nil, &ParseError{Field: "point.ce", Value: xe.Point.CE, Message: err.Error()}
	}

	le, err := parseFloat(xe.Point.LE)
	if err != nil {
		return nil, &ParseError{Field: "point.le", Value: xe.Point.LE, Message: err.Error()}
	}

	event := &Event{
		Version: xe.Version,
		UID:     xe.UID,
		Type:    xe.Type,
		Time:    eventTime,
		Start:   startTime,
		Stale:   staleTime,
		How:     xe.How,
		Access:  xe.Access,
		QoS:     xe.QoS,
		Opex:    xe.Opex,
		Point: Point{
			Lat: lat,
			Lon: lon,
			HAE: hae,
			CE:  ce,
			LE:  le,
		},
	}

	if xe.Detail != nil {
		event.Detail = &Detail{}

		if xe.Detail.Track != nil {
			course, _ := parseFloat(xe.Detail.Track.Course)
			speed, _ := parseFloat(xe.Detail.Track.Speed)
			sNic, _ := parseFloat(xe.Detail.Track.SNic)
			cNic, _ := parseFloat(xe.Detail.Track.CNic)

			event.Detail.Track = &Track{
				Course:     course,
				Speed:      speed,
				TrackValid: parseBool(xe.Detail.Track.TrackValid),
				SNic:       sNic,
				CNic:       cNic,
			}
		}

		if xe.Detail.UID != nil {
			event.Detail.UID = &UIDDetail{
				Callsign: xe.Detail.UID.Callsign,
				Droid:    xe.Detail.UID.Droid,
				ICD:      xe.Detail.UID.ICD,
				IMEI:     xe.Detail.UID.IMEI,
				Tail:     xe.Detail.UID.Tail,
				Serial:   xe.Detail.UID.Serial,
			}
		}

		if xe.Detail.Remarks != nil && len(xe.Detail.Remarks.Notes) > 0 {
			event.Detail.Remarks = &Remarks{
				Notes: make([]Note, 0, len(xe.Detail.Remarks.Notes)),
			}
			for _, xn := range xe.Detail.Remarks.Notes {
				note := Note{
					Text:   strings.TrimSpace(xn.Text),
					Source: xn.Source,
				}
				if xn.TS != "" {
					if ts, err := ParseCoTTime(xn.TS); err == nil {
						note.TS = ts
					}
				}
				event.Detail.Remarks.Notes = append(event.Detail.Remarks.Notes, note)
			}
		}

		if xe.Detail.FlowTags != nil && len(xe.Detail.FlowTags.Sources) > 0 {
			event.Detail.FlowTags = &FlowTags{
				Sources: make([]Source, 0, len(xe.Detail.FlowTags.Sources)),
			}
			for _, xs := range xe.Detail.FlowTags.Sources {
				source := Source{
					Name: xs.Name,
					Host: xs.Host,
					UID:  xs.UID,
					Op:   xs.Op,
				}
				if xs.TS != "" {
					if ts, err := ParseCoTTime(xs.TS); err == nil {
						source.TS = ts
					}
				}
				event.Detail.FlowTags.Sources = append(event.Detail.FlowTags.Sources, source)
			}
		}

		if xe.Detail.Contact != nil {
			event.Detail.Contact = &Contact{
				Callsign: xe.Detail.Contact.Callsign,
				Endpoint: xe.Detail.Contact.Endpoint,
			}
		}

		if xe.Detail.Status != nil {
			battery, _ := parseInt(xe.Detail.Status.Battery)
			event.Detail.Status = &Status{
				Battery:   battery,
				Readiness: xe.Detail.Status.Readiness,
			}
		}

		if xe.Detail.Sensor != nil {
			fov, _ := parseFloat(xe.Detail.Sensor.FOV)
			sensorRange, _ := parseFloat(xe.Detail.Sensor.Range)
			event.Detail.Sensor = &Sensor{
				FOV:   fov,
				Range: sensorRange,
			}
		}

		if xe.Detail.Health != nil {
			hr, _ := parseInt(xe.Detail.Health.HeartRate)
			spo2, _ := parseInt(xe.Detail.Health.SpO2)
			stress, _ := parseInt(xe.Detail.Health.Stress)
			bb, _ := parseInt(xe.Detail.Health.BodyBattery)
			temp, _ := parseFloat(xe.Detail.Health.Temperature)
			resp, _ := parseInt(xe.Detail.Health.RespirationRate)
			if hr > 0 || spo2 > 0 || stress > 0 || bb > 0 || temp > 0 || resp > 0 {
				event.Detail.Health = &Health{
					HeartRate:       hr,
					SpO2:            spo2,
					Stress:          stress,
					BodyBattery:     bb,
					Temperature:     temp,
					RespirationRate: resp,
				}
			}
		}

		if event.Detail.Track == nil && event.Detail.UID == nil &&
			event.Detail.Remarks == nil && event.Detail.FlowTags == nil &&
			event.Detail.Contact == nil && event.Detail.Status == nil &&
			event.Detail.Sensor == nil && event.Detail.Health == nil {
			event.Detail = nil
		}
	}

	return event, nil
}

// MustFromXML parses a CoT XML string into an Event, panicking on error.
func MustFromXML(xmlData string) *Event {
	event, err := FromXML(xmlData)
	if err != nil {
		panic(fmt.Sprintf("MustFromXML: %v", err))
	}
	return event
}

// IsValidCoTXML checks if the given string is valid CoT XML
func IsValidCoTXML(xmlData string) bool {
	_, err := FromXML(xmlData)
	return err == nil
}
