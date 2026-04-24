package protocol

import (
	"bufio"
	"bytes"
	"io"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type ginkgoErrorReader struct {
	err error
}

func (r *ginkgoErrorReader) Read(p []byte) (int, error) {
	return 0, r.err
}

var _ = Describe("Protocol Detection", func() {

	Describe("DetectProtocol", func() {
		Context("XML protocol", func() {
			It("should detect XML when first byte is '<'", func() {
				r := bufio.NewReader(bytes.NewReader([]byte("<event version=\"2.0\">")))
				proto, err := DetectProtocol(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(proto).To(Equal(ProtocolXML))
			})

			It("should not consume the '<' byte", func() {
				input := []byte("<event/>")
				r := bufio.NewReader(bytes.NewReader(input))
				_, err := DetectProtocol(r)
				Expect(err).NotTo(HaveOccurred())

				b, err := r.ReadByte()
				Expect(err).NotTo(HaveOccurred())
				Expect(b).To(Equal(byte('<')))
			})

			It("should detect XML starting with XML declaration", func() {
				r := bufio.NewReader(bytes.NewReader([]byte("<?xml version=\"1.0\"?>")))
				proto, err := DetectProtocol(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(proto).To(Equal(ProtocolXML))
			})
		})

		Context("Protobuf protocol", func() {
			It("should detect protobuf when first byte is magic byte 0xBF", func() {
				r := bufio.NewReader(bytes.NewReader([]byte{MagicByte, 0x05, 0x01, 0x02}))
				proto, err := DetectProtocol(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(proto).To(Equal(ProtocolProtobuf))
			})

			It("should not consume the magic byte", func() {
				input := []byte{MagicByte, 0x01, 0xFF}
				r := bufio.NewReader(bytes.NewReader(input))
				_, err := DetectProtocol(r)
				Expect(err).NotTo(HaveOccurred())

				b, err := r.ReadByte()
				Expect(err).NotTo(HaveOccurred())
				Expect(b).To(BeNumerically("==", MagicByte))
			})
		})

		Context("unknown protocol", func() {
			It("should return error for data starting with 'A'", func() {
				r := bufio.NewReader(bytes.NewReader([]byte("ABCDEF")))
				_, err := DetectProtocol(r)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unknown protocol"))
			})

			It("should return error for data starting with null byte", func() {
				r := bufio.NewReader(bytes.NewReader([]byte{0x00, 0x01, 0x02}))
				_, err := DetectProtocol(r)
				Expect(err).To(HaveOccurred())
			})

			It("should return error for data starting with 0xFF", func() {
				r := bufio.NewReader(bytes.NewReader([]byte{0xFF, 0xFF, 0xFF}))
				_, err := DetectProtocol(r)
				Expect(err).To(HaveOccurred())
			})

			It("should return error for single non-protocol byte", func() {
				r := bufio.NewReader(bytes.NewReader([]byte{0x42}))
				_, err := DetectProtocol(r)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("empty and error input", func() {
			It("should return error for empty reader", func() {
				r := bufio.NewReader(bytes.NewReader([]byte{}))
				_, err := DetectProtocol(r)
				Expect(err).To(HaveOccurred())
			})

			It("should return error for nil reader content", func() {
				r := bufio.NewReader(bytes.NewReader(nil))
				_, err := DetectProtocol(r)
				Expect(err).To(HaveOccurred())
			})

			It("should propagate reader errors", func() {
				r := bufio.NewReader(&ginkgoErrorReader{err: io.ErrUnexpectedEOF})
				_, err := DetectProtocol(r)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("adversarial inputs", func() {
			It("should handle all-zero bytes", func() {
				r := bufio.NewReader(bytes.NewReader(make([]byte, 100)))
				_, err := DetectProtocol(r)
				Expect(err).To(HaveOccurred())
			})

			It("should handle all-0xFF bytes", func() {
				data := make([]byte, 100)
				for i := range data {
					data[i] = 0xFF
				}
				r := bufio.NewReader(bytes.NewReader(data))
				_, err := DetectProtocol(r)
				Expect(err).To(HaveOccurred())
			})

			It("should handle very large input starting with valid byte", func() {
				data := make([]byte, 1024*1024)
				data[0] = '<'
				r := bufio.NewReader(bytes.NewReader(data))
				proto, err := DetectProtocol(r)
				Expect(err).NotTo(HaveOccurred())
				Expect(proto).To(Equal(ProtocolXML))
			})
		})
	})

	Describe("Protocol constants", func() {
		It("should have correct XML constant", func() {
			Expect(ProtocolXML).To(Equal("xml"))
		})

		It("should have correct Protobuf constant", func() {
			Expect(ProtocolProtobuf).To(Equal("protobuf"))
		})

		It("should have correct magic byte", func() {
			Expect(byte(MagicByte)).To(Equal(byte(0xBF)))
		})
	})
})
