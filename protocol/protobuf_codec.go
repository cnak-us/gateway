package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// MagicByte is the TAK Protocol v1 framing byte that precedes every protobuf message.
const MagicByte = 0xBF

// ProtobufCodec reads and writes TAK Protocol v1 (protobuf) framed messages.
// Wire format: [0xBF] [varint length] [payload bytes]
type ProtobufCodec struct {
	r io.Reader
}

// NewProtobufCodec creates a new protobuf stream codec wrapping the given reader.
func NewProtobufCodec(r io.Reader) *ProtobufCodec {
	return &ProtobufCodec{r: r}
}

// ReadMessage reads the next protobuf-framed message from the stream.
// It expects: magic byte (0xBF), varint-encoded length, then payload of that length.
func (c *ProtobufCodec) ReadMessage() ([]byte, error) {
	// Read and verify magic byte
	var magic [1]byte
	if _, err := io.ReadFull(c.r, magic[:]); err != nil {
		return nil, err
	}
	if magic[0] != MagicByte {
		return nil, fmt.Errorf("expected magic byte 0x%02X, got 0x%02X", MagicByte, magic[0])
	}

	// Read varint-encoded length
	length, err := readVarint(c.r)
	if err != nil {
		return nil, fmt.Errorf("reading message length: %w", err)
	}

	if length == 0 {
		return nil, fmt.Errorf("zero-length message")
	}

	// Sanity check to avoid huge allocations from corrupt data
	const maxMessageSize = 10 * 1024 * 1024 // 10 MB
	if length > maxMessageSize {
		return nil, fmt.Errorf("message length %d exceeds maximum %d", length, maxMessageSize)
	}

	// Read payload
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.r, payload); err != nil {
		return nil, fmt.Errorf("reading message payload: %w", err)
	}

	return payload, nil
}

// WriteMessage writes a protobuf-framed message: magic byte + varint length + payload.
func WriteMessage(w io.Writer, payload []byte) error {
	// Write magic byte
	if _, err := w.Write([]byte{MagicByte}); err != nil {
		return err
	}

	// Write varint-encoded length
	if err := writeVarint(w, uint64(len(payload))); err != nil {
		return err
	}

	// Write payload
	_, err := w.Write(payload)
	return err
}

// readVarint reads a base-128 varint from the reader, one byte at a time.
func readVarint(r io.Reader) (uint64, error) {
	var result uint64
	var shift uint
	var buf [1]byte

	for {
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, err
		}
		b := buf[0]
		result |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, nil
		}
		shift += 7
		if shift >= 64 {
			return 0, fmt.Errorf("varint overflow")
		}
	}
}

// writeVarint writes a base-128 varint to the writer.
func writeVarint(w io.Writer, v uint64) error {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], v)
	_, err := w.Write(buf[:n])
	return err
}
