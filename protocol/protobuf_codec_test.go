package protocol

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestProtobufCodecReadMessage(t *testing.T) {
	t.Run("valid message", func(t *testing.T) {
		payload := []byte("hello protobuf")
		msg := encodeProtobufMessage(payload)
		codec := NewProtobufCodec(bytes.NewReader(msg))

		data, err := codec.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage failed: %v", err)
		}
		if !bytes.Equal(data, payload) {
			t.Errorf("payload mismatch: got %q, want %q", data, payload)
		}
	})

	t.Run("multiple messages", func(t *testing.T) {
		msg1 := encodeProtobufMessage([]byte("first"))
		msg2 := encodeProtobufMessage([]byte("second"))
		combined := append(msg1, msg2...)
		codec := NewProtobufCodec(bytes.NewReader(combined))

		data1, err := codec.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage 1 failed: %v", err)
		}
		if string(data1) != "first" {
			t.Errorf("expected 'first', got %q", data1)
		}

		data2, err := codec.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage 2 failed: %v", err)
		}
		if string(data2) != "second" {
			t.Errorf("expected 'second', got %q", data2)
		}
	})

	t.Run("wrong magic byte", func(t *testing.T) {
		data := []byte{0xAA, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05}
		codec := NewProtobufCodec(bytes.NewReader(data))

		_, err := codec.ReadMessage()
		if err == nil {
			t.Error("expected error for wrong magic byte")
		}
	})

	t.Run("zero length message", func(t *testing.T) {
		data := []byte{MagicByte, 0x00}
		codec := NewProtobufCodec(bytes.NewReader(data))

		_, err := codec.ReadMessage()
		if err == nil {
			t.Error("expected error for zero-length message")
		}
	})

	t.Run("truncated payload", func(t *testing.T) {
		// Magic byte + varint length of 10, but only 3 payload bytes
		data := []byte{MagicByte, 0x0A, 0x01, 0x02, 0x03}
		codec := NewProtobufCodec(bytes.NewReader(data))

		_, err := codec.ReadMessage()
		if err == nil {
			t.Error("expected error for truncated payload")
		}
	})

	t.Run("EOF on empty reader", func(t *testing.T) {
		codec := NewProtobufCodec(bytes.NewReader(nil))
		_, err := codec.ReadMessage()
		if err == nil {
			t.Error("expected error on empty reader")
		}
	})

	t.Run("oversized message", func(t *testing.T) {
		// Encode a length that exceeds the 10MB limit
		var buf bytes.Buffer
		buf.WriteByte(MagicByte)
		// Varint for 11 * 1024 * 1024 (exceeds 10MB limit)
		lenBuf := make([]byte, binary.MaxVarintLen64)
		n := binary.PutUvarint(lenBuf, 11*1024*1024)
		buf.Write(lenBuf[:n])
		// Don't actually write 11MB of data, the length check should reject first

		codec := NewProtobufCodec(bytes.NewReader(buf.Bytes()))
		_, err := codec.ReadMessage()
		if err == nil {
			t.Error("expected error for oversized message")
		}
	})

	t.Run("large valid message", func(t *testing.T) {
		payload := make([]byte, 1024)
		for i := range payload {
			payload[i] = byte(i % 256)
		}
		msg := encodeProtobufMessage(payload)
		codec := NewProtobufCodec(bytes.NewReader(msg))

		data, err := codec.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage failed: %v", err)
		}
		if len(data) != 1024 {
			t.Errorf("expected 1024 bytes, got %d", len(data))
		}
	})

	t.Run("varint overflow", func(t *testing.T) {
		// Create a varint that doesn't terminate (all bytes have high bit set)
		var buf bytes.Buffer
		buf.WriteByte(MagicByte)
		for i := 0; i < 12; i++ {
			buf.WriteByte(0xFF) // continuation bytes
		}
		codec := NewProtobufCodec(bytes.NewReader(buf.Bytes()))
		_, err := codec.ReadMessage()
		if err == nil {
			t.Error("expected error for varint overflow")
		}
	})
}

func TestWriteMessage(t *testing.T) {
	t.Run("basic write", func(t *testing.T) {
		var buf bytes.Buffer
		payload := []byte("test payload")

		err := WriteMessage(&buf, payload)
		if err != nil {
			t.Fatalf("WriteMessage failed: %v", err)
		}

		// Read it back
		codec := NewProtobufCodec(bytes.NewReader(buf.Bytes()))
		data, err := codec.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage failed: %v", err)
		}
		if !bytes.Equal(data, payload) {
			t.Errorf("roundtrip failed: got %q, want %q", data, payload)
		}
	})

	t.Run("empty payload", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteMessage(&buf, []byte{})
		if err != nil {
			t.Fatalf("WriteMessage failed: %v", err)
		}
		// Should write magic byte + varint(0) = 2 bytes
		if buf.Len() < 2 {
			t.Errorf("expected at least 2 bytes, got %d", buf.Len())
		}
	})

	t.Run("roundtrip various sizes", func(t *testing.T) {
		sizes := []int{1, 10, 127, 128, 255, 256, 1000, 10000}
		for _, size := range sizes {
			var buf bytes.Buffer
			payload := make([]byte, size)
			for i := range payload {
				payload[i] = byte(i % 256)
			}

			if err := WriteMessage(&buf, payload); err != nil {
				t.Fatalf("WriteMessage(size=%d) failed: %v", size, err)
			}

			codec := NewProtobufCodec(bytes.NewReader(buf.Bytes()))
			data, err := codec.ReadMessage()
			if err != nil {
				t.Fatalf("ReadMessage(size=%d) failed: %v", size, err)
			}
			if !bytes.Equal(data, payload) {
				t.Errorf("roundtrip(size=%d) failed", size)
			}
		}
	})

	t.Run("error writer on magic byte", func(t *testing.T) {
		w := &failWriterAt{failAt: 0}
		err := WriteMessage(w, []byte("test"))
		if err == nil {
			t.Error("expected error from fail writer")
		}
	})
}

// encodeProtobufMessage creates a properly framed protobuf message.
func encodeProtobufMessage(payload []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(MagicByte)
	lenBuf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(lenBuf, uint64(len(payload)))
	buf.Write(lenBuf[:n])
	buf.Write(payload)
	return buf.Bytes()
}

// failWriterAt fails after writing failAt bytes.
type failWriterAt struct {
	written int
	failAt  int
}

func (w *failWriterAt) Write(p []byte) (int, error) {
	if w.written >= w.failAt {
		return 0, io.ErrClosedPipe
	}
	canWrite := w.failAt - w.written
	if canWrite > len(p) {
		canWrite = len(p)
	}
	w.written += canWrite
	if w.written >= w.failAt {
		return canWrite, io.ErrClosedPipe
	}
	return canWrite, nil
}
