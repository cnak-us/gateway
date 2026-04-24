package protocol

import (
	"bytes"
	"encoding/binary"
	"io"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type ginkgoFailWriterAt struct {
	written int
	failAt  int
}

func (w *ginkgoFailWriterAt) Write(p []byte) (int, error) {
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

func ginkgoEncodeProtobufMessage(payload []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(MagicByte)
	lenBuf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(lenBuf, uint64(len(payload)))
	buf.Write(lenBuf[:n])
	buf.Write(payload)
	return buf.Bytes()
}

var _ = Describe("Protobuf Codec", func() {

	Describe("ReadMessage", func() {
		Context("valid messages", func() {
			It("should read a single valid message", func() {
				payload := []byte("hello protobuf")
				msg := ginkgoEncodeProtobufMessage(payload)
				codec := NewProtobufCodec(bytes.NewReader(msg))

				data, err := codec.ReadMessage()
				Expect(err).NotTo(HaveOccurred())
				Expect(data).To(Equal(payload))
			})

			It("should read multiple consecutive messages", func() {
				msg1 := ginkgoEncodeProtobufMessage([]byte("first"))
				msg2 := ginkgoEncodeProtobufMessage([]byte("second"))
				combined := append(msg1, msg2...)
				codec := NewProtobufCodec(bytes.NewReader(combined))

				data1, err := codec.ReadMessage()
				Expect(err).NotTo(HaveOccurred())
				Expect(string(data1)).To(Equal("first"))

				data2, err := codec.ReadMessage()
				Expect(err).NotTo(HaveOccurred())
				Expect(string(data2)).To(Equal("second"))
			})

			It("should handle a 1-byte payload", func() {
				msg := ginkgoEncodeProtobufMessage([]byte{0x42})
				codec := NewProtobufCodec(bytes.NewReader(msg))

				data, err := codec.ReadMessage()
				Expect(err).NotTo(HaveOccurred())
				Expect(data).To(Equal([]byte{0x42}))
			})

			It("should handle a large valid payload (1KB)", func() {
				payload := make([]byte, 1024)
				for i := range payload {
					payload[i] = byte(i % 256)
				}
				msg := ginkgoEncodeProtobufMessage(payload)
				codec := NewProtobufCodec(bytes.NewReader(msg))

				data, err := codec.ReadMessage()
				Expect(err).NotTo(HaveOccurred())
				Expect(len(data)).To(Equal(1024))
			})
		})

		Context("error conditions", func() {
			It("should reject wrong magic byte", func() {
				data := []byte{0xAA, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05}
				codec := NewProtobufCodec(bytes.NewReader(data))

				_, err := codec.ReadMessage()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("expected magic byte"))
			})

			It("should reject zero-length message", func() {
				data := []byte{MagicByte, 0x00}
				codec := NewProtobufCodec(bytes.NewReader(data))

				_, err := codec.ReadMessage()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("zero-length"))
			})

			It("should reject truncated payload", func() {
				data := []byte{MagicByte, 0x0A, 0x01, 0x02, 0x03}
				codec := NewProtobufCodec(bytes.NewReader(data))

				_, err := codec.ReadMessage()
				Expect(err).To(HaveOccurred())
			})

			It("should return error on empty reader", func() {
				codec := NewProtobufCodec(bytes.NewReader(nil))
				_, err := codec.ReadMessage()
				Expect(err).To(HaveOccurred())
			})

			It("should reject oversized messages (> 10MB)", func() {
				var buf bytes.Buffer
				buf.WriteByte(MagicByte)
				lenBuf := make([]byte, binary.MaxVarintLen64)
				n := binary.PutUvarint(lenBuf, 11*1024*1024)
				buf.Write(lenBuf[:n])

				codec := NewProtobufCodec(bytes.NewReader(buf.Bytes()))
				_, err := codec.ReadMessage()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("exceeds maximum"))
			})

			It("should detect varint overflow", func() {
				var buf bytes.Buffer
				buf.WriteByte(MagicByte)
				for i := 0; i < 12; i++ {
					buf.WriteByte(0xFF)
				}
				codec := NewProtobufCodec(bytes.NewReader(buf.Bytes()))
				_, err := codec.ReadMessage()
				Expect(err).To(HaveOccurred())
			})
		})

		Context("adversarial inputs", func() {
			It("should handle all-zero bytes after magic byte", func() {
				data := make([]byte, 100)
				data[0] = MagicByte
				codec := NewProtobufCodec(bytes.NewReader(data))
				_, err := codec.ReadMessage()
				Expect(err).To(HaveOccurred())
			})

			It("should handle all-0xFF bytes", func() {
				data := make([]byte, 100)
				for i := range data {
					data[i] = 0xFF
				}
				codec := NewProtobufCodec(bytes.NewReader(data))
				_, err := codec.ReadMessage()
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("WriteMessage", func() {
		It("should write and read back correctly (round-trip)", func() {
			var buf bytes.Buffer
			payload := []byte("test payload")

			err := WriteMessage(&buf, payload)
			Expect(err).NotTo(HaveOccurred())

			codec := NewProtobufCodec(bytes.NewReader(buf.Bytes()))
			data, err := codec.ReadMessage()
			Expect(err).NotTo(HaveOccurred())
			Expect(data).To(Equal(payload))
		})

		It("should write empty payload", func() {
			var buf bytes.Buffer
			err := WriteMessage(&buf, []byte{})
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.Len()).To(BeNumerically(">=", 2))
		})

		DescribeTable("round-trip various sizes",
			func(size int) {
				var buf bytes.Buffer
				payload := make([]byte, size)
				for i := range payload {
					payload[i] = byte(i % 256)
				}

				Expect(WriteMessage(&buf, payload)).To(Succeed())

				codec := NewProtobufCodec(bytes.NewReader(buf.Bytes()))
				data, err := codec.ReadMessage()
				Expect(err).NotTo(HaveOccurred())
				Expect(data).To(Equal(payload))
			},
			Entry("1 byte", 1),
			Entry("10 bytes", 10),
			Entry("127 bytes", 127),
			Entry("128 bytes (varint boundary)", 128),
			Entry("255 bytes", 255),
			Entry("256 bytes", 256),
			Entry("1000 bytes", 1000),
			Entry("10000 bytes", 10000),
		)

		It("should propagate writer errors", func() {
			w := &ginkgoFailWriterAt{failAt: 0}
			err := WriteMessage(w, []byte("test"))
			Expect(err).To(HaveOccurred())
		})
	})
})
