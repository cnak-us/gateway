package bridge

import (
	"strings"
	"testing"
	"time"

	cot "github.com/cnak-us/cnak/pkg/cot"
)

func TestCotXMLToPoint(t *testing.T) {
	t.Run("full CoT event", func(t *testing.T) {
		cotXML := `<event version="2.0" uid="test-uid-1" type="a-f-G-U-C" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="m-g">
			<point lat="38.8977" lon="-77.0365" hae="100.0" ce="10.0" le="5.0"/>
			<detail>
				<contact callsign="ALPHA-1"/>
				<track course="90.0" speed="5.5"/>
				<__group name="ALPHA"/>
			</detail>
		</event>`

		p, err := CotXMLToPoint([]byte(cotXML))
		if err != nil {
			t.Fatalf("CotXMLToPoint failed: %v", err)
		}

		if p.ID != "test-uid-1" {
			t.Errorf("expected ID 'test-uid-1', got %q", p.ID)
		}
		if p.TrackID != "test-uid-1" {
			t.Errorf("expected TrackID 'test-uid-1', got %q", p.TrackID)
		}
		if p.Latitude != 38.8977 {
			t.Errorf("expected lat 38.8977, got %f", p.Latitude)
		}
		if p.Longitude != -77.0365 {
			t.Errorf("expected lon -77.0365, got %f", p.Longitude)
		}
		if p.Altitude != 100.0 {
			t.Errorf("expected alt 100.0, got %f", p.Altitude)
		}
		if p.CE != 10.0 {
			t.Errorf("expected CE 10.0, got %f", p.CE)
		}
		if p.LE != 5.0 {
			t.Errorf("expected LE 5.0, got %f", p.LE)
		}
		if p.Type != "a-f-G-U-C" {
			t.Errorf("expected type 'a-f-G-U-C', got %q", p.Type)
		}
		if p.Callsign != "ALPHA-1" {
			t.Errorf("expected callsign 'ALPHA-1', got %q", p.Callsign)
		}
		if p.Course != 90.0 {
			t.Errorf("expected course 90.0, got %f", p.Course)
		}
		if p.Speed != 5.5 {
			t.Errorf("expected speed 5.5, got %f", p.Speed)
		}
		if p.Group != "ALPHA" {
			t.Errorf("expected group 'ALPHA', got %q", p.Group)
		}
		if p.How != "m-g" {
			t.Errorf("expected how 'm-g', got %q", p.How)
		}
		if p.Stale != "2024-01-01T00:05:00Z" {
			t.Errorf("expected stale '2024-01-01T00:05:00Z', got %q", p.Stale)
		}
	})

	t.Run("minimal event without detail", func(t *testing.T) {
		cotXML := `<event uid="uid-2" type="a-u-G" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="m-g"><point lat="0" lon="0" hae="0" ce="0" le="0"/></event>`

		p, err := CotXMLToPoint([]byte(cotXML))
		if err != nil {
			t.Fatalf("CotXMLToPoint failed: %v", err)
		}

		if p.ID != "uid-2" {
			t.Errorf("expected ID 'uid-2', got %q", p.ID)
		}
		if p.Group != DefaultGroup {
			t.Errorf("expected default group %q, got %q", DefaultGroup, p.Group)
		}
		if p.Callsign != "" {
			t.Errorf("expected empty callsign, got %q", p.Callsign)
		}
	})

	t.Run("event with no group", func(t *testing.T) {
		cotXML := `<event uid="uid-3" type="a-f-G" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="h-e"><point lat="1" lon="2" hae="3" ce="4" le="5"/><detail><contact callsign="test"/></detail></event>`

		p, err := CotXMLToPoint([]byte(cotXML))
		if err != nil {
			t.Fatalf("CotXMLToPoint failed: %v", err)
		}

		if p.Group != DefaultGroup {
			t.Errorf("expected default group, got %q", p.Group)
		}
	})

	t.Run("invalid XML", func(t *testing.T) {
		_, err := CotXMLToPoint([]byte("not xml"))
		if err == nil {
			t.Error("expected error for invalid XML")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		_, err := CotXMLToPoint([]byte(""))
		if err == nil {
			t.Error("expected error for empty input")
		}
	})

	t.Run("non-numeric coordinates", func(t *testing.T) {
		cotXML := `<event uid="uid-4" type="a-u-G" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="m-g"><point lat="invalid" lon="bad" hae="nope" ce="x" le="y"/></event>`

		_, err := CotXMLToPoint([]byte(cotXML))
		if err == nil {
			t.Error("expected error for non-numeric coordinates")
		}
	})
}

func TestExtractGroupFromCoT(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "with group",
			input:    `<event uid="1" type="a" time="t" stale="s" how="h"><point lat="0" lon="0" hae="0" ce="0" le="0"/><detail><__group name="BRAVO"/></detail></event>`,
			expected: "BRAVO",
		},
		{
			name:     "without group",
			input:    `<event uid="1" type="a" time="t" stale="s" how="h"><point lat="0" lon="0" hae="0" ce="0" le="0"/><detail/></event>`,
			expected: DefaultGroup,
		},
		{
			name:     "invalid XML",
			input:    "not xml",
			expected: DefaultGroup,
		},
		{
			name:     "empty input",
			input:    "",
			expected: DefaultGroup,
		},
		{
			name:     "empty group name",
			input:    `<event uid="1" type="a" time="t" stale="s" how="h"><point lat="0" lon="0" hae="0" ce="0" le="0"/><detail><__group name=""/></detail></event>`,
			expected: DefaultGroup,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractGroupFromCoT([]byte(tt.input))
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestExtractUIDFromCoT(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid event",
			input:    `<event uid="my-uid" type="a" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="h"><point/></event>`,
			expected: "my-uid",
		},
		{
			name:     "invalid XML",
			input:    "garbage",
			expected: "",
		},
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractUIDFromCoT([]byte(tt.input))
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestExtractCallsignFromCoT(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "with callsign",
			input:    `<event uid="1" type="a" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="h"><point/><detail><contact callsign="ALPHA-1"/></detail></event>`,
			expected: "ALPHA-1",
		},
		{
			name:     "without detail",
			input:    `<event uid="1" type="a" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="h"><point/></event>`,
			expected: "",
		},
		{
			name:     "detail without contact",
			input:    `<event uid="1" type="a" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="h"><point/><detail><track course="0" speed="0"/></detail></event>`,
			expected: "",
		},
		{
			name:     "invalid XML",
			input:    "bad",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCallsignFromCoT([]byte(tt.input))
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestSanitizeGroup(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ALPHA", "ALPHA"},
		{"Team Alpha", "Team_Alpha"},
		{"group.sub", "group-sub"},
		{"a b.c d", "a_b-c_d"},
		{"", ""},
		{"no_change", "no_change"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SanitizeGroup(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeGroup(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestPointToCoTXML(t *testing.T) {
	t.Run("full point", func(t *testing.T) {
		p := &Point{
			ID:        "test-uid",
			Latitude:  38.8977,
			Longitude: -77.0365,
			Altitude:  100.0,
			CE:        10.0,
			LE:        5.0,
			Type:      "a-f-G-U-C",
			Callsign:  "ALPHA-1",
			Course:    90.0,
			Speed:     5.5,
			Timestamp: "2024-01-01T00:00:00Z",
			Stale:     "2024-01-01T00:05:00Z",
			How:       "m-g",
		}

		xml, err := PointToCoTXML(p)
		if err != nil {
			t.Fatalf("PointToCoTXML failed: %v", err)
		}

		s := string(xml)
		if !strings.Contains(s, `uid="test-uid"`) {
			t.Error("missing uid attribute")
		}
		if !strings.Contains(s, `type="a-f-G-U-C"`) {
			t.Error("missing type attribute")
		}
		if !strings.Contains(s, `lat="38.897700"`) {
			t.Error("missing lat attribute")
		}
		if !strings.Contains(s, `lon="-77.036500"`) {
			t.Error("missing lon attribute")
		}
		if !strings.Contains(s, `callsign="ALPHA-1"`) {
			t.Error("missing callsign")
		}
		if !strings.Contains(s, `course="90.0"`) {
			t.Error("missing course")
		}
		if !strings.Contains(s, `speed="5.5"`) {
			t.Error("missing speed")
		}
		if !strings.Contains(s, `how="m-g"`) {
			t.Error("missing how attribute")
		}
		if !strings.HasPrefix(s, "<event") {
			t.Error("should start with <event")
		}
		if !strings.HasSuffix(s, "</event>") {
			t.Error("should end with </event>")
		}
	})

	t.Run("minimal point defaults", func(t *testing.T) {
		p := &Point{
			ID:        "min-uid",
			Latitude:  0,
			Longitude: 0,
		}

		xml, err := PointToCoTXML(p)
		if err != nil {
			t.Fatalf("PointToCoTXML failed: %v", err)
		}

		s := string(xml)
		// Should have default type
		if !strings.Contains(s, `type="a-u-G"`) {
			t.Error("missing default type a-u-G")
		}
		// Should have default how
		if !strings.Contains(s, `how="m-g"`) {
			t.Error("missing default how m-g")
		}
		// Should not have callsign element (empty callsign)
		if strings.Contains(s, "contact") {
			t.Error("should not have contact element for empty callsign")
		}
		// Should not have track element (zero course and speed)
		if strings.Contains(s, "track") {
			t.Error("should not have track element for zero course/speed")
		}
	})

	t.Run("XML special chars escaped", func(t *testing.T) {
		p := &Point{
			ID:       `uid&"<>`,
			Type:     "a-f-G",
			Callsign: `call<sign>`,
		}

		xml, err := PointToCoTXML(p)
		if err != nil {
			t.Fatalf("PointToCoTXML failed: %v", err)
		}

		s := string(xml)
		if strings.Contains(s, "&\"") {
			t.Error("unescaped ampersand and quote")
		}
		if !strings.Contains(s, "&amp;") {
			t.Error("ampersand should be escaped")
		}
		if !strings.Contains(s, "&lt;") {
			t.Error("< should be escaped")
		}
	})

	t.Run("stale generated from timestamp", func(t *testing.T) {
		p := &Point{
			ID:        "uid-stale",
			Timestamp: "2024-01-01T00:00:00Z",
		}

		xml, err := PointToCoTXML(p)
		if err != nil {
			t.Fatalf("PointToCoTXML failed: %v", err)
		}

		s := string(xml)
		// Stale should be 5 minutes after timestamp
		if !strings.Contains(s, `stale="2024-01-01T00:05:00Z"`) {
			t.Errorf("expected stale 5 min after timestamp, got: %s", s)
		}
	})
}

func TestEscapeXML(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"a&b", "a&amp;b"},
		{"<tag>", "&lt;tag&gt;"},
		{`"quote"`, "&quot;quote&quot;"},
		{"it's", "it&apos;s"},
		{"a&b<c>d\"e'f", "a&amp;b&lt;c&gt;d&quot;e&apos;f"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeXML(tt.input)
			if got != tt.expected {
				t.Errorf("escapeXML(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCotEventToPoint(t *testing.T) {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testStale := time.Date(2024, 1, 1, 0, 5, 0, 0, time.UTC)

	t.Run("full event", func(t *testing.T) {
		e := &cot.Event{
			UID:   "evt-uid",
			Type:  "a-f-G-U-C",
			Time:  testTime,
			Start: testTime,
			Stale: testStale,
			How:   "m-g",
			Point: cot.Point{Lat: 38.8977, Lon: -77.0365, HAE: 100.0, CE: 10.0, LE: 5.0},
			Detail: &cot.Detail{
				Track:   &cot.Track{Course: 90.0, Speed: 5.5},
				Contact: &cot.Contact{Callsign: "ALPHA-1"},
			},
		}

		p := cotEventToPoint(e)

		if p.ID != "evt-uid" {
			t.Errorf("expected ID 'evt-uid', got %q", p.ID)
		}
		if p.Callsign != "ALPHA-1" {
			t.Errorf("expected callsign 'ALPHA-1', got %q", p.Callsign)
		}
		if p.Course != 90.0 {
			t.Errorf("expected course 90.0, got %f", p.Course)
		}
		if p.Group != DefaultGroup {
			t.Errorf("expected default group, got %q", p.Group)
		}
	})

	t.Run("uid callsign fallback", func(t *testing.T) {
		e := &cot.Event{
			UID:  "uid-1",
			Type: "a-u-G",
			Detail: &cot.Detail{
				UID: &cot.UIDDetail{Callsign: "UID-CALLSIGN"},
			},
		}

		p := cotEventToPoint(e)
		if p.Callsign != "UID-CALLSIGN" {
			t.Errorf("expected fallback to uid callsign, got %q", p.Callsign)
		}
	})

	t.Run("no detail", func(t *testing.T) {
		e := &cot.Event{UID: "uid-2", Type: "a-u-G"}
		p := cotEventToPoint(e)
		if p.Callsign != "" {
			t.Errorf("expected empty callsign, got %q", p.Callsign)
		}
	})
}

func TestShouldRelay(t *testing.T) {
	tests := []struct {
		name         string
		messageGroup string
		clientGroups []string
		expected     bool
	}{
		{
			name:         "empty message group",
			messageGroup: "",
			clientGroups: []string{"ALPHA"},
			expected:     true,
		},
		{
			name:         "ANON message group",
			messageGroup: DefaultGroup,
			clientGroups: []string{"ALPHA"},
			expected:     true,
		},
		{
			name:         "empty client groups",
			messageGroup: "ALPHA",
			clientGroups: nil,
			expected:     true,
		},
		{
			name:         "matching group",
			messageGroup: "ALPHA",
			clientGroups: []string{"ALPHA", "BRAVO"},
			expected:     true,
		},
		{
			name:         "no matching group",
			messageGroup: "CHARLIE",
			clientGroups: []string{"ALPHA", "BRAVO"},
			expected:     false,
		},
		{
			name:         "single matching group",
			messageGroup: "BRAVO",
			clientGroups: []string{"BRAVO"},
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldRelay(tt.messageGroup, tt.clientGroups)
			if got != tt.expected {
				t.Errorf("ShouldRelay(%q, %v) = %v, want %v", tt.messageGroup, tt.clientGroups, got, tt.expected)
			}
		})
	}
}
