package protocol

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestXMLCodecReadEvent(t *testing.T) {
	t.Run("single event", func(t *testing.T) {
		input := `<event version="2.0" uid="test-1" type="a-f-G"><point lat="38.0" lon="-77.0" hae="0"/><detail/></event>`
		codec := NewXMLCodec(strings.NewReader(input))

		data, err := codec.ReadEvent()
		if err != nil {
			t.Fatalf("ReadEvent failed: %v", err)
		}
		if string(data) != input {
			t.Errorf("data mismatch:\ngot:  %s\nwant: %s", data, input)
		}
	})

	t.Run("multiple events", func(t *testing.T) {
		event1 := `<event uid="1"><point/><detail/></event>`
		event2 := `<event uid="2"><point/><detail/></event>`
		input := event1 + event2
		codec := NewXMLCodec(strings.NewReader(input))

		data1, err := codec.ReadEvent()
		if err != nil {
			t.Fatalf("ReadEvent 1 failed: %v", err)
		}
		if string(data1) != event1 {
			t.Errorf("event 1 mismatch:\ngot:  %s\nwant: %s", data1, event1)
		}

		data2, err := codec.ReadEvent()
		if err != nil {
			t.Fatalf("ReadEvent 2 failed: %v", err)
		}
		if string(data2) != event2 {
			t.Errorf("event 2 mismatch:\ngot:  %s\nwant: %s", data2, event2)
		}
	})

	t.Run("events with XML declaration", func(t *testing.T) {
		input := `<?xml version="1.0" encoding="UTF-8"?><event uid="1"><point/></event>`
		codec := NewXMLCodec(strings.NewReader(input))

		data, err := codec.ReadEvent()
		if err != nil {
			t.Fatalf("ReadEvent failed: %v", err)
		}
		if !strings.Contains(string(data), "<event") {
			t.Error("event data should contain <event")
		}
	})

	t.Run("events with whitespace between", func(t *testing.T) {
		input := "  \n\t  <event uid=\"1\"><point/></event>  \n  <event uid=\"2\"><point/></event>"
		codec := NewXMLCodec(strings.NewReader(input))

		_, err := codec.ReadEvent()
		if err != nil {
			t.Fatalf("ReadEvent 1 failed: %v", err)
		}

		_, err = codec.ReadEvent()
		if err != nil {
			t.Fatalf("ReadEvent 2 failed: %v", err)
		}
	})

	t.Run("EOF on empty input", func(t *testing.T) {
		codec := NewXMLCodec(strings.NewReader(""))
		_, err := codec.ReadEvent()
		if err == nil {
			t.Error("expected error on empty input")
		}
	})

	t.Run("unexpected EOF mid-event", func(t *testing.T) {
		input := `<event uid="1"><point/>`
		codec := NewXMLCodec(strings.NewReader(input))

		_, err := codec.ReadEvent()
		if err == nil {
			t.Error("expected error for incomplete event")
		}
	})

	t.Run("nested event-like tags", func(t *testing.T) {
		// Test event with detail containing text that looks like </event> won't happen
		// in real CoT but let's ensure the close tag detection works
		input := `<event uid="1"><point/><detail><remarks>test</remarks></detail></event>`
		codec := NewXMLCodec(strings.NewReader(input))

		data, err := codec.ReadEvent()
		if err != nil {
			t.Fatalf("ReadEvent failed: %v", err)
		}
		if !strings.HasSuffix(string(data), "</event>") {
			t.Error("event should end with </event>")
		}
	})

	t.Run("multiple XML declarations between events", func(t *testing.T) {
		input := `<?xml version="1.0"?><event uid="1"><point/></event><?xml version="1.0"?><event uid="2"><point/></event>`
		codec := NewXMLCodec(strings.NewReader(input))

		_, err := codec.ReadEvent()
		if err != nil {
			t.Fatalf("ReadEvent 1 failed: %v", err)
		}

		_, err = codec.ReadEvent()
		if err != nil {
			t.Fatalf("ReadEvent 2 failed: %v", err)
		}
	})
}

func TestWriteEvent(t *testing.T) {
	t.Run("basic write", func(t *testing.T) {
		var buf bytes.Buffer
		event := []byte(`<event uid="1"><point/></event>`)

		err := WriteEvent(&buf, event)
		if err != nil {
			t.Fatalf("WriteEvent failed: %v", err)
		}
		if !bytes.Equal(buf.Bytes(), event) {
			t.Errorf("written data mismatch")
		}
	})

	t.Run("empty event", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteEvent(&buf, []byte{})
		if err != nil {
			t.Fatalf("WriteEvent failed: %v", err)
		}
		if buf.Len() != 0 {
			t.Errorf("expected 0 bytes, got %d", buf.Len())
		}
	})

	t.Run("error writer", func(t *testing.T) {
		w := &failWriter{}
		err := WriteEvent(w, []byte(`<event/>`))
		if err == nil {
			t.Error("expected error from fail writer")
		}
	})
}

type failWriter struct{}

func (w *failWriter) Write(p []byte) (int, error) {
	return 0, io.ErrClosedPipe
}
