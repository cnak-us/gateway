package bridge

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cnak-us/cnak/pkg/natsutil"
)

var _ = Describe("CoT Transform", func() {

	Describe("CotXMLToPoint", func() {
		It("should parse a full CoT event with all fields", func() {
			cotXML := `<event version="2.0" uid="test-uid-1" type="a-f-G-U-C" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="m-g">
				<point lat="38.8977" lon="-77.0365" hae="100.0" ce="10.0" le="5.0"/>
				<detail>
					<contact callsign="ALPHA-1"/>
					<track course="90.0" speed="5.5"/>
					<__group name="ALPHA"/>
				</detail>
			</event>`

			p, err := CotXMLToPoint([]byte(cotXML))
			Expect(err).NotTo(HaveOccurred())
			Expect(p.ID).To(Equal("test-uid-1"))
			Expect(p.TrackID).To(Equal("test-uid-1"))
			Expect(p.Latitude).To(BeNumerically("~", 38.8977, 0.0001))
			Expect(p.Longitude).To(BeNumerically("~", -77.0365, 0.0001))
			Expect(p.Altitude).To(BeNumerically("~", 100.0, 0.1))
			Expect(p.CE).To(BeNumerically("~", 10.0, 0.1))
			Expect(p.LE).To(BeNumerically("~", 5.0, 0.1))
			Expect(p.Type).To(Equal("a-f-G-U-C"))
			Expect(p.Callsign).To(Equal("ALPHA-1"))
			Expect(p.Course).To(BeNumerically("~", 90.0, 0.1))
			Expect(p.Speed).To(BeNumerically("~", 5.5, 0.1))
			Expect(p.Group).To(Equal("ALPHA"))
			Expect(p.How).To(Equal("m-g"))
			Expect(p.Stale).To(Equal("2024-01-01T00:05:00Z"))
		})

		It("should parse a minimal event without detail", func() {
			cotXML := `<event uid="uid-2" type="a-u-G" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="m-g"><point lat="0" lon="0" hae="0" ce="0" le="0"/></event>`
			p, err := CotXMLToPoint([]byte(cotXML))
			Expect(err).NotTo(HaveOccurred())
			Expect(p.ID).To(Equal("uid-2"))
			Expect(p.Group).To(Equal(DefaultGroup))
			Expect(p.Callsign).To(BeEmpty())
		})

		It("should default to __ANON__ group when no group element", func() {
			cotXML := `<event uid="uid-3" type="a-f-G" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="h-e"><point lat="1" lon="2" hae="3" ce="4" le="5"/><detail><contact callsign="test"/></detail></event>`
			p, err := CotXMLToPoint([]byte(cotXML))
			Expect(err).NotTo(HaveOccurred())
			Expect(p.Group).To(Equal(DefaultGroup))
		})

		It("should return error for invalid XML", func() {
			_, err := CotXMLToPoint([]byte("not xml"))
			Expect(err).To(HaveOccurred())
		})

		It("should return error for empty input", func() {
			_, err := CotXMLToPoint([]byte(""))
			Expect(err).To(HaveOccurred())
		})

		It("should return error for non-numeric coordinates", func() {
			cotXML := `<event uid="uid-4" type="a-u-G" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="m-g"><point lat="invalid" lon="bad" hae="nope" ce="x" le="y"/></event>`
			_, err := CotXMLToPoint([]byte(cotXML))
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ExtractGroupFromCoT", func() {
		DescribeTable("group extraction",
			func(input, expected string) {
				got := ExtractGroupFromCoT([]byte(input))
				Expect(got).To(Equal(expected))
			},
			Entry("with group", `<event uid="1" type="a" time="t" stale="s" how="h"><point lat="0" lon="0" hae="0" ce="0" le="0"/><detail><__group name="BRAVO"/></detail></event>`, "BRAVO"),
			Entry("without group", `<event uid="1" type="a" time="t" stale="s" how="h"><point lat="0" lon="0" hae="0" ce="0" le="0"/><detail/></event>`, DefaultGroup),
			Entry("invalid XML", "not xml", DefaultGroup),
			Entry("empty input", "", DefaultGroup),
			Entry("empty group name", `<event uid="1" type="a" time="t" stale="s" how="h"><point lat="0" lon="0" hae="0" ce="0" le="0"/><detail><__group name=""/></detail></event>`, DefaultGroup),
		)
	})

	Describe("ExtractUIDFromCoT", func() {
		It("should extract UID from valid event", func() {
			Expect(ExtractUIDFromCoT([]byte(`<event uid="my-uid" type="a" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="h"><point/></event>`))).To(Equal("my-uid"))
		})

		It("should return empty for invalid XML", func() {
			Expect(ExtractUIDFromCoT([]byte("garbage"))).To(BeEmpty())
		})

		It("should return empty for empty input", func() {
			Expect(ExtractUIDFromCoT([]byte(""))).To(BeEmpty())
		})
	})

	Describe("ExtractCallsignFromCoT", func() {
		It("should extract callsign when present", func() {
			Expect(ExtractCallsignFromCoT([]byte(`<event uid="1" type="a" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="h"><point/><detail><contact callsign="ALPHA-1"/></detail></event>`))).To(Equal("ALPHA-1"))
		})

		It("should return empty without detail", func() {
			Expect(ExtractCallsignFromCoT([]byte(`<event uid="1" type="a" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="h"><point/></event>`))).To(BeEmpty())
		})

		It("should return empty with detail but no contact", func() {
			Expect(ExtractCallsignFromCoT([]byte(`<event uid="1" type="a" time="2024-01-01T00:00:00Z" start="2024-01-01T00:00:00Z" stale="2024-01-01T00:05:00Z" how="h"><point/><detail><track course="0" speed="0"/></detail></event>`))).To(BeEmpty())
		})
	})

	Describe("SanitizeSubjectToken", func() {
		DescribeTable("group sanitization via shared natsutil",
			func(input, expected string) {
				Expect(natsutil.SanitizeSubjectToken(input)).To(Equal(expected))
			},
			Entry("no change needed", "ALPHA", "ALPHA"),
			Entry("spaces to underscores", "Team Alpha", "Team_Alpha"),
			Entry("dots to underscores", "group.sub", "group_sub"),
			Entry("mixed", "a b.c d", "a_b_c_d"),
			Entry("empty", "", ""),
		)
	})

	Describe("PointToCoTXML", func() {
		It("should generate XML with all fields populated", func() {
			p := &Point{
				ID: "test-uid", Latitude: 38.8977, Longitude: -77.0365, Altitude: 100.0,
				CE: 10.0, LE: 5.0, Type: "a-f-G-U-C", Callsign: "ALPHA-1",
				Course: 90.0, Speed: 5.5, Timestamp: "2024-01-01T00:00:00Z",
				Stale: "2024-01-01T00:05:00Z", How: "m-g",
			}

			xml, err := PointToCoTXML(p)
			Expect(err).NotTo(HaveOccurred())
			s := string(xml)

			Expect(s).To(ContainSubstring(`uid="test-uid"`))
			Expect(s).To(ContainSubstring(`type="a-f-G-U-C"`))
			Expect(s).To(ContainSubstring(`lat="38.897700"`))
			Expect(s).To(ContainSubstring(`callsign="ALPHA-1"`))
			Expect(s).To(HavePrefix("<event"))
			Expect(s).To(HaveSuffix("</event>"))
		})

		It("should use default type and how for minimal point", func() {
			p := &Point{ID: "min-uid"}
			xml, err := PointToCoTXML(p)
			Expect(err).NotTo(HaveOccurred())
			s := string(xml)
			Expect(s).To(ContainSubstring(`type="a-u-G"`))
			Expect(s).To(ContainSubstring(`how="m-g"`))
		})

		It("should escape XML special characters", func() {
			p := &Point{ID: `uid&"<>`, Type: "a-f-G", Callsign: `call<sign>`}
			xml, err := PointToCoTXML(p)
			Expect(err).NotTo(HaveOccurred())
			s := string(xml)
			Expect(s).To(ContainSubstring("&amp;"))
			Expect(s).To(ContainSubstring("&lt;"))
		})

		It("should generate stale from timestamp when stale is empty", func() {
			p := &Point{ID: "uid-stale", Timestamp: "2024-01-01T00:00:00Z"}
			xml, err := PointToCoTXML(p)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(xml)).To(ContainSubstring(`stale="2024-01-01T00:05:00Z"`))
		})
	})

	Describe("escapeXML", func() {
		DescribeTable("XML escaping",
			func(input, expected string) {
				Expect(escapeXML(input)).To(Equal(expected))
			},
			Entry("no escaping", "hello", "hello"),
			Entry("ampersand", "a&b", "a&amp;b"),
			Entry("angle brackets", "<tag>", "&lt;tag&gt;"),
			Entry("double quotes", `"q"`, "&quot;q&quot;"),
			Entry("single quotes", "it's", "it&apos;s"),
			Entry("empty", "", ""),
		)
	})

	Describe("ShouldRelay", func() {
		DescribeTable("relay decisions",
			func(messageGroup string, clientGroups []string, expected bool) {
				Expect(ShouldRelay(messageGroup, clientGroups)).To(Equal(expected))
			},
			Entry("empty message group", "", []string{"ALPHA"}, true),
			Entry("ANON message group", DefaultGroup, []string{"ALPHA"}, true),
			Entry("empty client groups", "ALPHA", []string(nil), true),
			Entry("matching group", "ALPHA", []string{"ALPHA", "BRAVO"}, true),
			Entry("no matching group", "CHARLIE", []string{"ALPHA", "BRAVO"}, false),
			Entry("single matching group", "BRAVO", []string{"BRAVO"}, true),
		)
	})
})
