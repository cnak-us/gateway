package protocol

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildVersionAnnouncement(t *testing.T) {
	data := BuildVersionAnnouncement()

	if !bytes.Contains(data, []byte(`type="t-x-takp-v"`)) {
		t.Error("announcement missing type t-x-takp-v")
	}
	if !bytes.Contains(data, []byte(`uid="takserver-version"`)) {
		t.Error("announcement missing uid takserver-version")
	}
	if !bytes.Contains(data, []byte(`<TakProtocolSupport version="1"/>`)) {
		t.Error("announcement missing TakProtocolSupport version 1")
	}
	if !bytes.Contains(data, []byte(`serverVersion="CNAK"`)) {
		t.Error("announcement missing serverVersion CNAK")
	}
	if !bytes.Contains(data, []byte(`<?xml version="1.0" encoding="UTF-8"?>`)) {
		t.Error("announcement missing XML declaration")
	}
	if !bytes.HasSuffix(data, []byte("</event>")) {
		t.Error("announcement should end with </event>")
	}
}

func TestBuildVersionResponse(t *testing.T) {
	t.Run("supported version", func(t *testing.T) {
		data := BuildVersionResponse("1", true)

		if !bytes.Contains(data, []byte(`type="t-x-takp-r"`)) {
			t.Error("response missing type t-x-takp-r")
		}
		if !bytes.Contains(data, []byte(`version="1" status="true"`)) {
			t.Error("response missing version 1 status true")
		}
	})

	t.Run("unsupported version", func(t *testing.T) {
		data := BuildVersionResponse("99", false)

		if !bytes.Contains(data, []byte(`version="99" status="false"`)) {
			t.Error("response missing version 99 status false")
		}
	})
}

func TestIsNegotiationEvent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "version announcement",
			input:    `<event type="t-x-takp-v" uid="takserver-version"><point/></event>`,
			expected: TypeTAKPVersion,
		},
		{
			name:     "protocol request",
			input:    `<event type="t-x-takp-q" uid="client"><point/></event>`,
			expected: TypeTAKPRequest,
		},
		{
			name:     "protocol response",
			input:    `<event type="t-x-takp-r" uid="takserver-version"><point/></event>`,
			expected: TypeTAKPResponse,
		},
		{
			name:     "regular SA event",
			input:    `<event type="a-f-G" uid="test"><point/></event>`,
			expected: "",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsNegotiationEvent([]byte(tt.input))
			if got != tt.expected {
				t.Errorf("IsNegotiationEvent = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractRequestedVersion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "version 1 request",
			input: `<event type="t-x-takp-q"><detail><TakControl>` +
				`<TakProtocolSupport version="1"/>` +
				`</TakControl></detail></event>`,
			expected: "1",
		},
		{
			name: "version 0 request",
			input: `<event type="t-x-takp-q"><detail><TakControl>` +
				`<TakProtocolSupport version="0"/>` +
				`</TakControl></detail></event>`,
			expected: "0",
		},
		{
			name:     "no version attribute",
			input:    `<event type="t-x-takp-q"><detail></detail></event>`,
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
			got := ExtractRequestedVersion([]byte(tt.input))
			if got != tt.expected {
				t.Errorf("ExtractRequestedVersion = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestVersionAnnouncementIsValidXML(t *testing.T) {
	data := string(BuildVersionAnnouncement())

	// Should contain required CoT event structure
	requiredParts := []string{
		`version="2.0"`,
		`how="h-e"`,
		`<point lat="0" lon="0"`,
		`ce="999999"`,
		`le="999999"`,
		`apiVersion="3"`,
	}
	for _, part := range requiredParts {
		if !strings.Contains(data, part) {
			t.Errorf("announcement missing required part: %s", part)
		}
	}
}
