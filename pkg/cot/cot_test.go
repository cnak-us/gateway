package cot

import (
	"strings"
	"testing"
	"time"
)

func TestNewEvent(t *testing.T) {
	e := NewEvent("test-uid", "a-f-G-U-C", 38.8977, -77.0365)
	if e.UID != "test-uid" {
		t.Errorf("UID = %q, want %q", e.UID, "test-uid")
	}
	if e.Type != "a-f-G-U-C" {
		t.Errorf("Type = %q, want %q", e.Type, "a-f-G-U-C")
	}
	if e.Point.Lat != 38.8977 {
		t.Errorf("Lat = %f, want %f", e.Point.Lat, 38.8977)
	}
	if e.Version != "2.0" {
		t.Errorf("Version = %q, want %q", e.Version, "2.0")
	}
	if e.How != "m-g" {
		t.Errorf("How = %q, want %q", e.How, "m-g")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	e := NewEventWithTrack("uid-1", "a-f-G-U-C", 10.0, 20.0, 90.0, 5.0)
	e.Detail.Contact = &Contact{Callsign: "ALPHA-1"}

	data, err := e.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}

	got, err := FromJSON(data)
	if err != nil {
		t.Fatalf("FromJSON: %v", err)
	}

	if got.UID != e.UID {
		t.Errorf("UID = %q, want %q", got.UID, e.UID)
	}
	if got.Detail == nil || got.Detail.Track == nil {
		t.Fatal("Detail.Track is nil")
	}
	if got.Detail.Track.Speed != 5.0 {
		t.Errorf("Speed = %f, want %f", got.Detail.Track.Speed, 5.0)
	}
	if got.Detail.Contact == nil || got.Detail.Contact.Callsign != "ALPHA-1" {
		t.Error("Contact.Callsign not preserved")
	}
}

func TestXMLRoundTrip(t *testing.T) {
	e := NewEvent("xml-test", "a-h-G-U-C", 34.0, -118.0)
	e.Detail = &Detail{
		Track:   &Track{Course: 180, Speed: 10, TrackValid: true},
		Contact: &Contact{Callsign: "HOSTILE-1"},
	}

	xmlStr, err := e.ToXML()
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}

	got, err := FromXML(xmlStr)
	if err != nil {
		t.Fatalf("FromXML: %v", err)
	}

	if got.UID != "xml-test" {
		t.Errorf("UID = %q, want %q", got.UID, "xml-test")
	}
	if got.Type != "a-h-G-U-C" {
		t.Errorf("Type = %q, want %q", got.Type, "a-h-G-U-C")
	}
	if got.Detail == nil || got.Detail.Track == nil {
		t.Fatal("Detail.Track is nil")
	}
	if got.Detail.Track.Course != 180 {
		t.Errorf("Course = %f, want %f", got.Detail.Track.Course, 180.0)
	}
	if got.Detail.Contact == nil || got.Detail.Contact.Callsign != "HOSTILE-1" {
		t.Error("Contact.Callsign not preserved")
	}
}

func TestFromXMLBytes(t *testing.T) {
	xml := `<event version="2.0" uid="test" type="a-f-G" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="m-g"><point lat="10.0" lon="20.0" hae="0" ce="50" le="30"/></event>`

	e, err := FromXMLBytes([]byte(xml))
	if err != nil {
		t.Fatalf("FromXMLBytes: %v", err)
	}
	if e.UID != "test" {
		t.Errorf("UID = %q, want %q", e.UID, "test")
	}
	if e.Point.Lat != 10.0 {
		t.Errorf("Lat = %f, want %f", e.Point.Lat, 10.0)
	}
}

func TestEscapeXML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"a&b", "a&amp;b"},
		{"<tag>", "&lt;tag&gt;"},
		{`"quoted"`, "&quot;quoted&quot;"},
		{"it's", "it&apos;s"},
		{`a&b<c>d"e'f`, "a&amp;b&lt;c&gt;d&quot;e&apos;f"},
	}
	for _, tt := range tests {
		if got := EscapeXML(tt.input); got != tt.want {
			t.Errorf("EscapeXML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseCoTTime(t *testing.T) {
	tests := []struct {
		input string
		ok    bool
	}{
		{"2024-01-01T00:00:00Z", true},
		{"2024-01-01T00:00:00.000Z", true},
		{"2024-01-01T00:00:00+00:00", true},
		{"2024-01-01T00:00:00", true},
		{"", false},
		{"not-a-date", false},
	}
	for _, tt := range tests {
		_, err := ParseCoTTime(tt.input)
		if (err == nil) != tt.ok {
			t.Errorf("ParseCoTTime(%q) error = %v, wantOk = %v", tt.input, err, tt.ok)
		}
	}
}

func TestValidate(t *testing.T) {
	e := NewEvent("uid", "a-f-G", 0, 0)
	if err := e.Validate(); err != nil {
		t.Errorf("valid event failed validation: %v", err)
	}

	bad := &Event{}
	if err := bad.Validate(); err == nil {
		t.Error("empty event should fail validation")
	}
}

func TestIsValidCoTXML(t *testing.T) {
	good := `<event version="2.0" uid="x" type="a" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z"><point lat="0" lon="0" hae="0" ce="0" le="0"/></event>`
	if !IsValidCoTXML(good) {
		t.Error("valid XML should return true")
	}
	if IsValidCoTXML("not xml") {
		t.Error("invalid XML should return false")
	}
}

func TestSetters(t *testing.T) {
	e := NewEvent("uid", "a-f-G", 0, 0)
	e.SetPoint(10, 20, 100, 5, 3)
	if e.Point.Lat != 10 || e.Point.Lon != 20 {
		t.Error("SetPoint failed")
	}

	e.SetTrack(90, 5, true)
	if e.Detail == nil || e.Detail.Track == nil || e.Detail.Track.Course != 90 {
		t.Error("SetTrack failed")
	}

	e.SetCallsign("BRAVO")
	if e.Detail.UID == nil || e.Detail.UID.Callsign != "BRAVO" {
		t.Error("SetCallsign failed")
	}

	e.AddFlowTag("cnak")
	if e.Detail.FlowTags == nil || len(e.Detail.FlowTags.Sources) != 1 {
		t.Error("AddFlowTag failed")
	}

	e.AddRemark("test note")
	if e.Detail.Remarks == nil || len(e.Detail.Remarks.Notes) != 1 {
		t.Error("AddRemark failed")
	}

	e.SetStale(10 * time.Minute)
	if e.Stale.Sub(e.Time) != 10*time.Minute {
		t.Error("SetStale failed")
	}
}

func TestToXMLContainsExpectedElements(t *testing.T) {
	e := NewEvent("uid-1", "a-f-G-U-C", 38.0, -77.0)
	e.Detail = &Detail{
		Track:   &Track{Course: 90, Speed: 5, TrackValid: true},
		Contact: &Contact{Callsign: "ALPHA"},
		Status:  &Status{Battery: 80},
	}

	xmlStr, err := e.ToXML()
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}

	checks := []string{
		`uid="uid-1"`,
		`type="a-f-G-U-C"`,
		`<track course=`,
		`<contact callsign="ALPHA"`,
		`<status battery="80"`,
		`<point lat="38.000000"`,
	}
	for _, c := range checks {
		if !strings.Contains(xmlStr, c) {
			t.Errorf("XML missing %q in:\n%s", c, xmlStr)
		}
	}
}

func TestParseError(t *testing.T) {
	pe := &ParseError{Field: "time", Value: "bad", Message: "parse failed"}
	if !strings.Contains(pe.Error(), "time") {
		t.Error("ParseError should contain field name")
	}
	if !strings.Contains(pe.Error(), "bad") {
		t.Error("ParseError should contain value")
	}

	pe2 := &ParseError{Field: "xml", Message: "invalid"}
	if strings.Contains(pe2.Error(), `""`) {
		t.Error("ParseError without value should not show empty quotes")
	}
}
