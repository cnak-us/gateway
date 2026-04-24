package protocol

import (
	"bytes"
	"io"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type ginkgoFailWriter struct{}

func (w *ginkgoFailWriter) Write(p []byte) (int, error) {
	return 0, io.ErrClosedPipe
}

var _ = Describe("XML Codec", func() {

	Describe("XMLCodec ReadEvent", func() {
		Context("valid events", func() {
			It("should read a single complete event", func() {
				input := `<event version="2.0" uid="test-1" type="a-f-G"><point lat="38.0" lon="-77.0" hae="0"/><detail/></event>`
				codec := NewXMLCodec(strings.NewReader(input))

				data, err := codec.ReadEvent()
				Expect(err).NotTo(HaveOccurred())
				Expect(string(data)).To(Equal(input))
			})

			It("should read multiple consecutive events", func() {
				event1 := `<event uid="1"><point/><detail/></event>`
				event2 := `<event uid="2"><point/><detail/></event>`
				codec := NewXMLCodec(strings.NewReader(event1 + event2))

				data1, err := codec.ReadEvent()
				Expect(err).NotTo(HaveOccurred())
				Expect(string(data1)).To(Equal(event1))

				data2, err := codec.ReadEvent()
				Expect(err).NotTo(HaveOccurred())
				Expect(string(data2)).To(Equal(event2))
			})

			It("should skip XML declaration before event", func() {
				input := `<?xml version="1.0" encoding="UTF-8"?><event uid="1"><point/></event>`
				codec := NewXMLCodec(strings.NewReader(input))

				data, err := codec.ReadEvent()
				Expect(err).NotTo(HaveOccurred())
				Expect(string(data)).To(ContainSubstring("<event"))
				Expect(string(data)).NotTo(ContainSubstring("<?xml"))
			})

			It("should skip whitespace between events", func() {
				input := "  \n\t  <event uid=\"1\"><point/></event>  \n  <event uid=\"2\"><point/></event>"
				codec := NewXMLCodec(strings.NewReader(input))

				_, err := codec.ReadEvent()
				Expect(err).NotTo(HaveOccurred())

				_, err = codec.ReadEvent()
				Expect(err).NotTo(HaveOccurred())
			})

			It("should skip multiple XML declarations between events", func() {
				input := `<?xml version="1.0"?><event uid="1"><point/></event><?xml version="1.0"?><event uid="2"><point/></event>`
				codec := NewXMLCodec(strings.NewReader(input))

				_, err := codec.ReadEvent()
				Expect(err).NotTo(HaveOccurred())
				_, err = codec.ReadEvent()
				Expect(err).NotTo(HaveOccurred())
			})

			It("should handle nested elements correctly", func() {
				input := `<event uid="1"><point/><detail><remarks>test</remarks></detail></event>`
				codec := NewXMLCodec(strings.NewReader(input))

				data, err := codec.ReadEvent()
				Expect(err).NotTo(HaveOccurred())
				Expect(string(data)).To(HaveSuffix("</event>"))
				Expect(string(data)).To(Equal(input))
			})

			It("should handle event with special characters in attribute values", func() {
				input := `<event uid="test&amp;uid" type="a-f-G"><point/></event>`
				codec := NewXMLCodec(strings.NewReader(input))

				data, err := codec.ReadEvent()
				Expect(err).NotTo(HaveOccurred())
				Expect(string(data)).To(ContainSubstring("test&amp;uid"))
			})
		})

		Context("error conditions", func() {
			It("should return error on empty input", func() {
				codec := NewXMLCodec(strings.NewReader(""))
				_, err := codec.ReadEvent()
				Expect(err).To(HaveOccurred())
			})

			It("should return error on unexpected EOF mid-event", func() {
				codec := NewXMLCodec(strings.NewReader(`<event uid="1"><point/>`))
				_, err := codec.ReadEvent()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unexpected EOF"))
			})

			It("should return EOF after all events consumed", func() {
				input := `<event uid="1"><point/></event>`
				codec := NewXMLCodec(strings.NewReader(input))

				_, err := codec.ReadEvent()
				Expect(err).NotTo(HaveOccurred())

				_, err = codec.ReadEvent()
				Expect(err).To(HaveOccurred())
			})
		})

		Context("adversarial inputs", func() {
			It("should handle very large event", func() {
				// Build a large event with many detail elements
				var sb strings.Builder
				sb.WriteString(`<event uid="large" type="a-f-G">`)
				sb.WriteString(`<point lat="0" lon="0" hae="0" ce="0" le="0"/>`)
				sb.WriteString("<detail>")
				for i := 0; i < 1000; i++ {
					sb.WriteString(`<element key="value"/>`)
				}
				sb.WriteString("</detail>")
				sb.WriteString("</event>")

				codec := NewXMLCodec(strings.NewReader(sb.String()))
				data, err := codec.ReadEvent()
				Expect(err).NotTo(HaveOccurred())
				Expect(string(data)).To(HaveSuffix("</event>"))
			})

			It("should handle whitespace-only input", func() {
				codec := NewXMLCodec(strings.NewReader("   \n\t\r  "))
				_, err := codec.ReadEvent()
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("WriteEvent", func() {
		It("should write event data to writer", func() {
			var buf bytes.Buffer
			event := []byte(`<event uid="1"><point/></event>`)

			err := WriteEvent(&buf, event)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.Bytes()).To(Equal(event))
		})

		It("should handle empty event", func() {
			var buf bytes.Buffer
			err := WriteEvent(&buf, []byte{})
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.Len()).To(BeZero())
		})

		It("should propagate writer errors", func() {
			err := WriteEvent(&ginkgoFailWriter{}, []byte("<event/>"))
			Expect(err).To(HaveOccurred())
		})
	})
})
