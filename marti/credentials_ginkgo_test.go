package marti

import (
	"encoding/base64"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Credentials", func() {

	Describe("InMemoryCredentialStore", func() {
		Describe("NewInMemoryCredentialStore", func() {
			It("should create an empty store", func() {
				store := NewInMemoryCredentialStore()
				Expect(store).NotTo(BeNil())
				Expect(store.creds).NotTo(BeNil())
				Expect(store.creds).To(BeEmpty())
			})
		})

		Describe("Add", func() {
			It("should add a credential successfully", func() {
				store := NewInMemoryCredentialStore()
				err := store.Add("user1", "pass1")
				Expect(err).NotTo(HaveOccurred())
				Expect(store.creds).To(HaveKey("user1"))
			})

			It("should store bcrypt hash, not plaintext", func() {
				store := NewInMemoryCredentialStore()
				Expect(store.Add("user1", "secretpass")).To(Succeed())

				hash := store.creds["user1"]
				Expect(hash).NotTo(Equal("secretpass"))
				Expect(hash).To(HavePrefix("$2a$"))
			})

			It("should overwrite existing credential", func() {
				store := NewInMemoryCredentialStore()
				Expect(store.Add("user1", "pass1")).To(Succeed())
				hash1 := store.creds["user1"]

				Expect(store.Add("user1", "pass2")).To(Succeed())
				hash2 := store.creds["user1"]

				Expect(hash2).NotTo(Equal(hash1))
			})
		})

		Describe("ValidateCredential", func() {
			It("should validate correct credentials", func() {
				store := NewInMemoryCredentialStore()
				Expect(store.Add("admin", "password")).To(Succeed())

				Expect(store.ValidateCredential("admin", "password")).To(BeTrue())
			})

			It("should reject incorrect password", func() {
				store := NewInMemoryCredentialStore()
				Expect(store.Add("admin", "password")).To(Succeed())

				Expect(store.ValidateCredential("admin", "wrong")).To(BeFalse())
			})

			It("should reject non-existent username", func() {
				store := NewInMemoryCredentialStore()
				Expect(store.ValidateCredential("nonexistent", "password")).To(BeFalse())
			})

			It("should reject empty username", func() {
				store := NewInMemoryCredentialStore()
				Expect(store.ValidateCredential("", "password")).To(BeFalse())
			})

			It("should reject empty password against stored credential", func() {
				store := NewInMemoryCredentialStore()
				Expect(store.Add("admin", "password")).To(Succeed())

				Expect(store.ValidateCredential("admin", "")).To(BeFalse())
			})

			It("should handle credential with empty password stored", func() {
				store := NewInMemoryCredentialStore()
				Expect(store.Add("admin", "")).To(Succeed())

				Expect(store.ValidateCredential("admin", "")).To(BeTrue())
				Expect(store.ValidateCredential("admin", "anything")).To(BeFalse())
			})
		})
	})

	Describe("validateBasicAuth", func() {
		var store *InMemoryCredentialStore

		BeforeEach(func() {
			store = NewInMemoryCredentialStore()
			Expect(store.Add("admin", "password")).To(Succeed())
		})

		It("should validate correct basic auth header", func() {
			authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:password"))
			username, ok := validateBasicAuth(authHeader, store)
			Expect(ok).To(BeTrue())
			Expect(username).To(Equal("admin"))
		})

		It("should reject empty auth header", func() {
			username, ok := validateBasicAuth("", store)
			Expect(ok).To(BeFalse())
			Expect(username).To(BeEmpty())
		})

		It("should reject non-Basic scheme", func() {
			username, ok := validateBasicAuth("Bearer token123", store)
			Expect(ok).To(BeFalse())
			Expect(username).To(BeEmpty())
		})

		It("should reject invalid base64 payload", func() {
			username, ok := validateBasicAuth("Basic !!!invalid!!!", store)
			Expect(ok).To(BeFalse())
			Expect(username).To(BeEmpty())
		})

		It("should reject payload without colon separator", func() {
			username, ok := validateBasicAuth("Basic "+base64.StdEncoding.EncodeToString([]byte("nocolon")), store)
			Expect(ok).To(BeFalse())
			Expect(username).To(BeEmpty())
		})

		It("should reject wrong password", func() {
			authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:wrong"))
			username, ok := validateBasicAuth(authHeader, store)
			Expect(ok).To(BeFalse())
			Expect(username).To(BeEmpty())
		})

		It("should handle case-insensitive Basic prefix", func() {
			authHeader := "basic " + base64.StdEncoding.EncodeToString([]byte("admin:password"))
			username, ok := validateBasicAuth(authHeader, store)
			Expect(ok).To(BeTrue())
			Expect(username).To(Equal("admin"))
		})

		It("should handle password containing colons", func() {
			store2 := NewInMemoryCredentialStore()
			Expect(store2.Add("user", "pass:with:colons")).To(Succeed())

			authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass:with:colons"))
			username, ok := validateBasicAuth(authHeader, store2)
			Expect(ok).To(BeTrue())
			Expect(username).To(Equal("user"))
		})

		It("should handle only scheme with no payload", func() {
			username, ok := validateBasicAuth("Basic", store)
			Expect(ok).To(BeFalse())
			Expect(username).To(BeEmpty())
		})
	})

	Describe("ensurePEMHeaders", func() {
		It("should pass through CSR with existing headers", func() {
			input := "-----BEGIN CERTIFICATE REQUEST-----\ndata\n-----END CERTIFICATE REQUEST-----"
			result := ensurePEMHeaders(input)
			Expect(result).To(ContainSubstring("-----BEGIN CERTIFICATE REQUEST-----"))
			Expect(result).To(ContainSubstring("data"))
		})

		It("should add headers to raw base64", func() {
			input := "MIICYzCCAUsCAQAwHjEcMBoGA1UEAwwTdGVzdC1jbGllbnQ="
			result := ensurePEMHeaders(input)
			Expect(result).To(HavePrefix("-----BEGIN CERTIFICATE REQUEST-----\n"))
			Expect(result).To(HaveSuffix("-----END CERTIFICATE REQUEST-----"))
			Expect(result).To(ContainSubstring("MIICYzCCAUsCAQAwHjEcMBoGA1UEAwwTdGVzdC1jbGllbnQ="))
		})

		It("should strip leading/trailing whitespace", func() {
			input := "   \n  MIICdata  \n  "
			result := ensurePEMHeaders(input)
			Expect(result).To(HavePrefix("-----BEGIN CERTIFICATE REQUEST-----\n"))
		})

		It("should handle empty string", func() {
			result := ensurePEMHeaders("")
			Expect(result).To(ContainSubstring("-----BEGIN CERTIFICATE REQUEST-----"))
			Expect(result).To(ContainSubstring("-----END CERTIFICATE REQUEST-----"))
		})
	})

	Describe("base64Wrap", func() {
		It("should return empty string for empty input", func() {
			result := base64Wrap([]byte{})
			Expect(result).To(BeEmpty())
		})

		It("should not wrap short strings", func() {
			result := base64Wrap([]byte("hello"))
			Expect(result).NotTo(ContainSubstring("\n"))
		})

		It("should wrap at 64 characters", func() {
			data := make([]byte, 256)
			result := base64Wrap(data)
			lines := strings.Split(result, "\n")
			for _, line := range lines {
				Expect(len(line)).To(BeNumerically("<=", 64))
			}
		})

		It("should produce decodable base64", func() {
			data := []byte("test data for base64 encoding that is fairly long to test wrapping")
			result := base64Wrap(data)
			cleaned := strings.ReplaceAll(result, "\n", "")
			decoded, err := base64.StdEncoding.DecodeString(cleaned)
			Expect(err).NotTo(HaveOccurred())
			Expect(decoded).To(Equal(data))
		})

		It("should handle exact 48-byte boundary (produces exactly 64 base64 chars)", func() {
			data := make([]byte, 48)
			result := base64Wrap(data)
			Expect(len(result)).To(Equal(64))
			Expect(result).NotTo(ContainSubstring("\n"))
		})
	})
})
