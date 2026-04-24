package protocol

import (
	"bufio"
	"fmt"
)

const (
	// ProtocolXML identifies TAK Protocol v0 (XML stream).
	ProtocolXML = "xml"
	// ProtocolProtobuf identifies TAK Protocol v1 (protobuf framed).
	ProtocolProtobuf = "protobuf"
)

// DetectProtocol peeks at the first byte of the stream to determine the TAK protocol version.
// Returns ProtocolProtobuf if the first byte is 0xBF (magic byte), ProtocolXML if '<'.
func DetectProtocol(r *bufio.Reader) (string, error) {
	b, err := r.Peek(1)
	if err != nil {
		return "", fmt.Errorf("detecting protocol: %w", err)
	}

	switch b[0] {
	case MagicByte:
		return ProtocolProtobuf, nil
	case '<':
		return ProtocolXML, nil
	default:
		return "", fmt.Errorf("unknown protocol: first byte 0x%02X", b[0])
	}
}
