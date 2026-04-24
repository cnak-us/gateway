package protocol

import (
	"bufio"
	"bytes"
	"io"
	"testing"
)

func TestDetectProtocol(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		wantProto string
		wantErr   bool
	}{
		{
			name:      "XML protocol (starts with <)",
			input:     []byte("<event ...>"),
			wantProto: ProtocolXML,
		},
		{
			name:      "protobuf protocol (starts with magic byte)",
			input:     []byte{MagicByte, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05},
			wantProto: ProtocolProtobuf,
		},
		{
			name:    "unknown protocol (starts with A)",
			input:   []byte("ABCDEF"),
			wantErr: true,
		},
		{
			name:    "unknown protocol (starts with null)",
			input:   []byte{0x00, 0x01, 0x02},
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   []byte{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bufio.NewReader(bytes.NewReader(tt.input))
			proto, err := DetectProtocol(r)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got protocol %q", proto)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if proto != tt.wantProto {
				t.Errorf("expected %q, got %q", tt.wantProto, proto)
			}

			// Verify peek didn't consume the byte
			b, _ := r.ReadByte()
			if b != tt.input[0] {
				t.Errorf("peek consumed the byte: expected 0x%02X, got 0x%02X", tt.input[0], b)
			}
		})
	}

	t.Run("EOF on empty reader", func(t *testing.T) {
		r := bufio.NewReader(bytes.NewReader(nil))
		_, err := DetectProtocol(r)
		if err == nil {
			t.Error("expected error on empty reader")
		}
	})

	t.Run("error reader", func(t *testing.T) {
		r := bufio.NewReader(&errorReader{err: io.ErrUnexpectedEOF})
		_, err := DetectProtocol(r)
		if err == nil {
			t.Error("expected error from error reader")
		}
	})
}

type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (int, error) {
	return 0, r.err
}
