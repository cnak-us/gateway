package marti

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cnak-us/gateway/audit"
	"github.com/cnak-us/gateway/ca"
	"github.com/cnak-us/gateway/config"
	"software.sslmate.com/src/go-pkcs12"
)

func ginkgoTestMartiAPI() *MartiAPI {
	authority := ca.NewCA()
	ExpectWithOffset(1, authority.GenerateCA("TestOrg", "TestOU")).To(Succeed())

	creds := NewInMemoryCredentialStore()
	ExpectWithOffset(1, creds.Add("admin", "password")).To(Succeed())

	cfg := &config.Config{
		CAOrganization:   "TestOrg",
		HTTPSAPIPort:     8446,
		TLSStreamingPort: 8089,
	}

	logger := slog.Default()
	auditor := audit.NewAuditor(nil, logger)

	return NewMartiAPI(cfg, authority, creds, nil, auditor, nil, logger)
}

func ginkgoBasicAuth(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func ginkgoGenerateCSR(cn string) []byte {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	template := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: cn},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
}

func ginkgoGenerateCSRRaw(cn string) string {
	csrPEM := ginkgoGenerateCSR(cn)
	s := string(csrPEM)
	s = strings.ReplaceAll(s, "-----BEGIN CERTIFICATE REQUEST-----\n", "")
	s = strings.ReplaceAll(s, "-----END CERTIFICATE REQUEST-----\n", "")
	s = strings.TrimSpace(s)
	return s
}

var _ = Describe("Enrollment Handlers", func() {

	Describe("handleTLSConfig", func() {
		It("should return certificate config XML with organization name", func() {
			m := ginkgoTestMartiAPI()
			req := httptest.NewRequest("GET", "/Marti/api/tls/config", nil)
			w := httptest.NewRecorder()

			m.router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Content-Type")).To(Equal("application/xml"))

			body := w.Body.String()
			Expect(body).To(ContainSubstring("TestOrg"))
			Expect(body).To(ContainSubstring("tak-gateway"))
			Expect(body).To(ContainSubstring("certificateConfig"))
		})
	})

	Describe("handleSignClient (v1)", func() {
		Context("authentication", func() {
			It("should reject requests with no auth header", func() {
				m := ginkgoTestMartiAPI()
				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient", strings.NewReader("fake"))
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusUnauthorized))
			})

			It("should reject requests with wrong credentials", func() {
				m := ginkgoTestMartiAPI()
				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient", strings.NewReader("fake"))
				req.Header.Set("Authorization", ginkgoBasicAuth("admin", "wrong"))
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusUnauthorized))
			})

			It("should reject Bearer auth", func() {
				m := ginkgoTestMartiAPI()
				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient", strings.NewReader("fake"))
				req.Header.Set("Authorization", "Bearer some-token")
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusUnauthorized))
			})

			It("should reject invalid base64 in auth header", func() {
				m := ginkgoTestMartiAPI()
				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient", strings.NewReader("fake"))
				req.Header.Set("Authorization", "Basic !!!not-base64!!!")
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusUnauthorized))
			})

			It("should reject base64-encoded data without colon separator", func() {
				m := ginkgoTestMartiAPI()
				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient", strings.NewReader("fake"))
				req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("nocolon")))
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusUnauthorized))
			})
		})

		Context("with valid authentication", func() {
			It("should return PKCS#12 for a valid CSR with PEM headers", func() {
				m := ginkgoTestMartiAPI()
				csrPEM := ginkgoGenerateCSR("test-atak-client")

				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient?clientUID=test-uid", strings.NewReader(string(csrPEM)))
				req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusOK))
				Expect(w.Header().Get("Content-Type")).To(Equal("application/x-pkcs12"))

				p12Data := w.Body.Bytes()
				Expect(p12Data).NotTo(BeEmpty())

				// Decode as trust store (v1 uses trust store format)
				certs, err := pkcs12.DecodeTrustStore(p12Data, "atakatak")
				Expect(err).NotTo(HaveOccurred())
				Expect(len(certs)).To(BeNumerically(">=", 2))

				// Find the signedCert and CA entries
				var foundSigned bool
				var foundCA bool
				for _, c := range certs {
					if c.Subject.CommonName == "test-atak-client" {
						foundSigned = true
					}
					if c.IsCA {
						foundCA = true
					}
				}
				Expect(foundSigned).To(BeTrue(), "should contain signedCert entry")
				Expect(foundCA).To(BeTrue(), "should contain ca entry")
			})

			It("should handle CSR without PEM headers (raw base64)", func() {
				m := ginkgoTestMartiAPI()
				rawCSR := ginkgoGenerateCSRRaw("raw-client")

				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient?clientUID=raw-uid", strings.NewReader(rawCSR))
				req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusOK))
				Expect(w.Header().Get("Content-Type")).To(Equal("application/x-pkcs12"))
			})

			It("should return 500 for garbage CSR data", func() {
				m := ginkgoTestMartiAPI()

				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient", strings.NewReader("completely invalid garbage"))
				req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusInternalServerError))
			})

			It("should return 500 for empty body", func() {
				m := ginkgoTestMartiAPI()

				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient", strings.NewReader(""))
				req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusInternalServerError))
			})

			It("should also work on trailing-slash route", func() {
				m := ginkgoTestMartiAPI()
				csrPEM := ginkgoGenerateCSR("trailing-slash-client")

				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient/", strings.NewReader(string(csrPEM)))
				req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusOK))
			})
		})
	})

	Describe("handleSignClientV2", func() {
		Context("authentication", func() {
			It("should reject requests with no auth", func() {
				m := ginkgoTestMartiAPI()
				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient/v2?clientUid=test", strings.NewReader("fake"))
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusUnauthorized))
			})
		})

		Context("JSON response (default)", func() {
			It("should return JSON with base64-DER encoded certs", func() {
				m := ginkgoTestMartiAPI()
				csrPEM := ginkgoGenerateCSR("v2-json-client")

				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient/v2?clientUid=v2-uid", strings.NewReader(string(csrPEM)))
				req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusOK))
				Expect(w.Header().Get("Content-Type")).To(Equal("application/json"))

				// NOTE: The v2 handler wraps PEM-encoded certificates into JSON
				// string values that may contain literal newlines, which is
				// technically invalid JSON. This is a known server behavior
				// that ATAK clients handle by stripping newlines before parsing.
				// We replicate that here.
				cleaned := strings.ReplaceAll(w.Body.String(), "\n", "")
				var result map[string]string
				Expect(json.Unmarshal([]byte(cleaned), &result)).To(Succeed())
				Expect(result).To(HaveKey("signedCert"))
				Expect(result).To(HaveKey("ca0"))
				Expect(result).To(HaveKey("ca1"))

				// ca0 and ca1 should be identical
				Expect(result["ca0"]).To(Equal(result["ca1"]))

				// Verify signedCert is valid base64 DER
				signedB64 := strings.ReplaceAll(result["signedCert"], "\n", "")
				signedDER, err := base64.StdEncoding.DecodeString(signedB64)
				Expect(err).NotTo(HaveOccurred())

				signedCert, err := x509.ParseCertificate(signedDER)
				Expect(err).NotTo(HaveOccurred())
				Expect(signedCert.Subject.CommonName).To(Equal("v2-json-client"))

				// Verify CA cert
				caB64 := strings.ReplaceAll(result["ca0"], "\n", "")
				caDER, err := base64.StdEncoding.DecodeString(caB64)
				Expect(err).NotTo(HaveOccurred())
				caCert, err := x509.ParseCertificate(caDER)
				Expect(err).NotTo(HaveOccurred())
				Expect(caCert.IsCA).To(BeTrue())
			})

			It("should default to JSON when no Accept header is set", func() {
				m := ginkgoTestMartiAPI()
				csrPEM := ginkgoGenerateCSR("v2-no-accept")

				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient/v2?clientUid=test", strings.NewReader(string(csrPEM)))
				req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusOK))
				Expect(w.Header().Get("Content-Type")).To(Equal("application/json"))
			})

			It("should default to JSON for */* accept header", func() {
				m := ginkgoTestMartiAPI()
				csrPEM := ginkgoGenerateCSR("v2-wildcard-accept")

				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient/v2?clientUid=test", strings.NewReader(string(csrPEM)))
				req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
				req.Header.Set("Accept", "*/*")
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusOK))
				Expect(w.Header().Get("Content-Type")).To(Equal("application/json"))
			})
		})

		Context("XML response", func() {
			It("should return XML when Accept: application/xml", func() {
				m := ginkgoTestMartiAPI()
				csrPEM := ginkgoGenerateCSR("v2-xml-client")

				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient/v2?clientUid=xml-uid", strings.NewReader(string(csrPEM)))
				req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
				req.Header.Set("Accept", "application/xml")
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusOK))
				Expect(w.Header().Get("Content-Type")).To(Equal("application/xml"))

				body := w.Body.String()
				Expect(body).To(ContainSubstring("<enrollment>"))
				Expect(body).To(ContainSubstring("<signedCert>"))
				Expect(body).To(ContainSubstring("<ca>"))
				Expect(body).To(ContainSubstring("</enrollment>"))
			})
		})

		Context("error handling", func() {
			It("should return 500 for malformed CSR", func() {
				m := ginkgoTestMartiAPI()

				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient/v2?clientUid=err", strings.NewReader("garbage"))
				req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusInternalServerError))
			})

			It("should return 500 for random bytes", func() {
				m := ginkgoTestMartiAPI()
				randomBytes := make([]byte, 512)
				_, _ = rand.Read(randomBytes)

				req := httptest.NewRequest("POST", "/Marti/api/tls/signClient/v2?clientUid=rand", strings.NewReader(base64.StdEncoding.EncodeToString(randomBytes)))
				req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusInternalServerError))
			})
		})
	})

	Describe("handleEnrollmentProfile", func() {
		It("should return 204 No Content", func() {
			m := ginkgoTestMartiAPI()
			req := httptest.NewRequest("GET", "/Marti/api/tls/profile/enrollment?clientUid=test-uid", nil)
			w := httptest.NewRecorder()

			m.router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusNoContent))
		})
	})

	Describe("handleVersionConfig", func() {
		It("should return server config as JSON", func() {
			m := ginkgoTestMartiAPI()
			req := httptest.NewRequest("GET", "/Marti/api/version/config", nil)
			w := httptest.NewRecorder()

			m.router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusOK))

			var result map[string]interface{}
			Expect(json.NewDecoder(w.Body).Decode(&result)).To(Succeed())
			Expect(result["version"]).To(Equal("3"))
			Expect(result["type"]).To(Equal("ServerConfig"))

			data := result["data"].(map[string]interface{})
			Expect(data["tls"]).To(BeTrue())
		})
	})

	Describe("handleContactsAll", func() {
		It("should return empty array with nil registry", func() {
			m := ginkgoTestMartiAPI()
			req := httptest.NewRequest("GET", "/Marti/api/contacts/all", nil)
			req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
			w := httptest.NewRecorder()

			m.router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusOK))

			var contacts []Contact
			Expect(json.NewDecoder(w.Body).Decode(&contacts)).To(Succeed())
			Expect(contacts).To(BeEmpty())
		})

		It("should reject requests without auth", func() {
			m := ginkgoTestMartiAPI()
			req := httptest.NewRequest("GET", "/Marti/api/contacts/all", nil)
			w := httptest.NewRecorder()

			m.router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusUnauthorized))
		})
	})

	Describe("handleClientEndPoints", func() {
		It("should return empty array with nil registry", func() {
			m := ginkgoTestMartiAPI()
			req := httptest.NewRequest("GET", "/Marti/api/clientEndPoints", nil)
			req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
			w := httptest.NewRecorder()

			m.router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusOK))

			var endpoints []ClientEndpoint
			Expect(json.NewDecoder(w.Body).Decode(&endpoints)).To(Succeed())
			Expect(endpoints).To(BeEmpty())
		})

		It("should reject requests without auth", func() {
			m := ginkgoTestMartiAPI()
			req := httptest.NewRequest("GET", "/Marti/api/clientEndPoints", nil)
			w := httptest.NewRecorder()

			m.router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusUnauthorized))
		})
	})

	Describe("Route registration", func() {
		DescribeTable("all routes respond with expected status",
			func(method, path string, expectedStatus int) {
				m := ginkgoTestMartiAPI()
				req := httptest.NewRequest(method, path, nil)
				w := httptest.NewRecorder()

				m.router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(expectedStatus))
			},
			Entry("TLS config", "GET", "/Marti/api/tls/config", http.StatusOK),
			Entry("version config", "GET", "/Marti/api/version/config", http.StatusOK),
			Entry("contacts no auth", "GET", "/Marti/api/contacts/all", http.StatusUnauthorized),
			Entry("client endpoints no auth", "GET", "/Marti/api/clientEndPoints", http.StatusUnauthorized),
			Entry("enrollment profile", "GET", "/Marti/api/tls/profile/enrollment", http.StatusNoContent),
			Entry("signClient no auth", "POST", "/Marti/api/tls/signClient", http.StatusUnauthorized),
			Entry("signClient/ no auth", "POST", "/Marti/api/tls/signClient/", http.StatusUnauthorized),
			Entry("signClient v2 no auth", "POST", "/Marti/api/tls/signClient/v2", http.StatusUnauthorized),
		)
	})

	Describe("Adversarial inputs", func() {
		It("should handle a very large CSR body gracefully", func() {
			m := ginkgoTestMartiAPI()
			largeBody := strings.Repeat("A", 1024*1024)

			req := httptest.NewRequest("POST", "/Marti/api/tls/signClient", strings.NewReader(largeBody))
			req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
			w := httptest.NewRecorder()

			m.router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})

		It("should handle null bytes in CSR body", func() {
			m := ginkgoTestMartiAPI()
			req := httptest.NewRequest("POST", "/Marti/api/tls/signClient", strings.NewReader("MIIC\x00\x00\x00YzCCAUs"))
			req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
			w := httptest.NewRecorder()

			m.router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})

		It("should handle concurrent enrollment requests", func() {
			m := ginkgoTestMartiAPI()

			done := make(chan int, 10)
			for i := 0; i < 10; i++ {
				go func() {
					defer GinkgoRecover()
					csrPEM := ginkgoGenerateCSR("concurrent-client")
					req := httptest.NewRequest("POST", "/Marti/api/tls/signClient?clientUID=concurrent", strings.NewReader(string(csrPEM)))
					req.Header.Set("Authorization", ginkgoBasicAuth("admin", "password"))
					w := httptest.NewRecorder()

					m.router.ServeHTTP(w, req)
					done <- w.Code
				}()
			}

			for i := 0; i < 10; i++ {
				code := <-done
				Expect(code).To(Equal(http.StatusOK))
			}
		})
	})
})
