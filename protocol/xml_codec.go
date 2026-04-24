package protocol

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
)

var eventCloseTag = []byte("</event>")

// XMLCodec reads and writes TAK Protocol v0 (XML) CoT event streams.
type XMLCodec struct {
	r *bufio.Reader
}

// NewXMLCodec creates a new XML stream codec wrapping the given reader.
func NewXMLCodec(r io.Reader) *XMLCodec {
	return &XMLCodec{r: bufio.NewReader(r)}
}

// ReadEvent reads the next complete CoT XML event from the stream.
// It accumulates bytes until the </event> closing tag is found.
// XML declarations (<?xml ...?>) between events are consumed and skipped.
func (c *XMLCodec) ReadEvent() ([]byte, error) {
	var buf bytes.Buffer

	for {
		// Skip whitespace and XML declarations between events
		if buf.Len() == 0 {
			if err := c.skipPreamble(); err != nil {
				return nil, err
			}
		}

		b, err := c.r.ReadByte()
		if err != nil {
			if err == io.EOF && buf.Len() > 0 {
				return nil, fmt.Errorf("unexpected EOF in XML event")
			}
			return nil, err
		}
		buf.WriteByte(b)

		// Check if buffer ends with </event>
		if buf.Len() >= len(eventCloseTag) {
			tail := buf.Bytes()[buf.Len()-len(eventCloseTag):]
			if bytes.Equal(tail, eventCloseTag) {
				return buf.Bytes(), nil
			}
		}
	}
}

// skipPreamble consumes whitespace and XML declarations (<?xml ...?>).
func (c *XMLCodec) skipPreamble() error {
	for {
		b, err := c.r.Peek(1)
		if err != nil {
			return err
		}

		switch {
		case b[0] == ' ' || b[0] == '\t' || b[0] == '\n' || b[0] == '\r':
			// Consume whitespace
			c.r.ReadByte()

		case b[0] == '<':
			// Check for XML declaration <?xml ...?>
			peeked, err := c.r.Peek(5)
			if err != nil {
				// Not enough bytes to determine; could be start of <event
				return nil
			}
			if bytes.Equal(peeked, []byte("<?xml")) {
				// Consume until ?>
				if err := c.consumeUntil([]byte("?>")); err != nil {
					return fmt.Errorf("unterminated XML declaration: %w", err)
				}
				continue
			}
			// It's some other XML element (likely <event), return to let caller read it
			return nil

		default:
			return nil
		}
	}
}

// consumeUntil reads and discards bytes until the given delimiter is found.
func (c *XMLCodec) consumeUntil(delim []byte) error {
	for {
		b, err := c.r.ReadByte()
		if err != nil {
			return err
		}
		if b == delim[0] && len(delim) > 1 {
			peeked, err := c.r.Peek(len(delim) - 1)
			if err != nil {
				continue
			}
			if bytes.Equal(peeked, delim[1:]) {
				// Consume the rest of the delimiter
				for i := 0; i < len(delim)-1; i++ {
					c.r.ReadByte()
				}
				return nil
			}
		} else if b == delim[0] && len(delim) == 1 {
			return nil
		}
	}
}

// WriteEvent writes a complete XML event to the writer.
func WriteEvent(w io.Writer, event []byte) error {
	_, err := w.Write(event)
	return err
}
