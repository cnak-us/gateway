package marti

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cnak-us/gateway/audit"
	"github.com/cnak-us/gateway/ca"
	"github.com/cnak-us/gateway/config"
)

// testMartiAPI creates a MartiAPI suitable for testing with all dependencies set up.
func testMartiAPI(t *testing.T) (*MartiAPI, *ca.CertificateAuthority) {
	t.Helper()

	authority := ca.NewCA()
	if err := authority.GenerateCA("TestOrg", "TestOU"); err != nil {
		t.Fatalf("failed to generate CA: %v", err)
	}

	creds := NewInMemoryCredentialStore()
	creds.Add("admin", "password")

	cfg := &config.Config{
		CAOrganization:   "TestOrg",
		HTTPSAPIPort:     8446,
		TLSStreamingPort: 8089,
	}

	logger := slog.Default()
	auditor := audit.NewAuditor(nil, logger)

	m := NewMartiAPI(cfg, authority, creds, nil, auditor, nil, logger)
	return m, authority
}

func basicAuth(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func TestHandleTLSConfig(t *testing.T) {
	m, _ := testMartiAPI(t)

	req := httptest.NewRequest("GET", "/Marti/api/tls/config", nil)
	w := httptest.NewRecorder()

	m.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/xml" {
		t.Errorf("expected application/xml, got %s", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "TestOrg") {
		t.Error("response should contain organization name")
	}
	if !strings.Contains(bodyStr, "tak-gateway") {
		t.Error("response should contain OU 'tak-gateway'")
	}
	if !strings.Contains(bodyStr, "certificateConfig") {
		t.Error("response should contain certificateConfig element")
	}
}

func TestHandleVersionConfig(t *testing.T) {
	m, _ := testMartiAPI(t)

	req := httptest.NewRequest("GET", "/Marti/api/version/config", nil)
	w := httptest.NewRecorder()

	m.router.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if result["version"] != "3" {
		t.Errorf("expected version 3, got %v", result["version"])
	}
	if result["type"] != "ServerConfig" {
		t.Errorf("expected type ServerConfig, got %v", result["type"])
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data field to be an object")
	}
	if data["tls"] != true {
		t.Error("expected tls to be true")
	}
}

func TestHandleEnrollmentProfile(t *testing.T) {
	m, _ := testMartiAPI(t)

	req := httptest.NewRequest("GET", "/Marti/api/tls/profile/enrollment?clientUid=test-uid", nil)
	w := httptest.NewRecorder()

	m.router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

func TestHandleContactsAll(t *testing.T) {
	t.Run("nil registry with auth", func(t *testing.T) {
		m, _ := testMartiAPI(t)

		req := httptest.NewRequest("GET", "/Marti/api/contacts/all", nil)
		req.Header.Set("Authorization", basicAuth("admin", "password"))
		w := httptest.NewRecorder()

		m.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var contacts []Contact
		json.NewDecoder(w.Result().Body).Decode(&contacts)
		if len(contacts) != 0 {
			t.Errorf("expected empty contacts, got %d", len(contacts))
		}
	})

	t.Run("no auth", func(t *testing.T) {
		m, _ := testMartiAPI(t)

		req := httptest.NewRequest("GET", "/Marti/api/contacts/all", nil)
		w := httptest.NewRecorder()

		m.router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}

func TestHandleClientEndPoints(t *testing.T) {
	t.Run("nil registry with auth", func(t *testing.T) {
		m, _ := testMartiAPI(t)

		req := httptest.NewRequest("GET", "/Marti/api/clientEndPoints", nil)
		req.Header.Set("Authorization", basicAuth("admin", "password"))
		w := httptest.NewRecorder()

		m.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var endpoints []ClientEndpoint
		json.NewDecoder(w.Result().Body).Decode(&endpoints)
		if len(endpoints) != 0 {
			t.Errorf("expected empty endpoints, got %d", len(endpoints))
		}
	})

	t.Run("no auth", func(t *testing.T) {
		m, _ := testMartiAPI(t)

		req := httptest.NewRequest("GET", "/Marti/api/clientEndPoints", nil)
		w := httptest.NewRecorder()

		m.router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}

func TestHandleSignClientAuth(t *testing.T) {
	m, _ := testMartiAPI(t)

	t.Run("no auth", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/Marti/api/tls/signClient", strings.NewReader("fake csr"))
		w := httptest.NewRecorder()

		m.router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("wrong credentials", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/Marti/api/tls/signClient", strings.NewReader("fake csr"))
		req.Header.Set("Authorization", basicAuth("admin", "wrong"))
		w := httptest.NewRecorder()

		m.router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("valid auth but invalid CSR", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/Marti/api/tls/signClient", strings.NewReader("not a real CSR"))
		req.Header.Set("Authorization", basicAuth("admin", "password"))
		w := httptest.NewRecorder()

		m.router.ServeHTTP(w, req)

		// Should fail at CSR signing stage (500)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500 for invalid CSR, got %d", w.Code)
		}
	})
}

func TestHandleSignClientV2Auth(t *testing.T) {
	m, _ := testMartiAPI(t)

	t.Run("no auth", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/Marti/api/tls/signClient/v2?clientUid=test", strings.NewReader("fake csr"))
		w := httptest.NewRecorder()

		m.router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("valid auth but invalid CSR", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/Marti/api/tls/signClient/v2?clientUid=test-uid", strings.NewReader("garbage"))
		req.Header.Set("Authorization", basicAuth("admin", "password"))
		w := httptest.NewRecorder()

		m.router.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500 for invalid CSR, got %d", w.Code)
		}
	})
}

func TestBase64Wrap(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantLen bool
		maxLine int
	}{
		{
			name:    "empty input",
			input:   []byte{},
			wantLen: false,
		},
		{
			name:    "short input",
			input:   []byte("hello"),
			wantLen: true,
			maxLine: 64,
		},
		{
			name:    "exact 48 bytes produces 64 base64 chars",
			input:   make([]byte, 48),
			wantLen: true,
			maxLine: 64,
		},
		{
			name:    "long input requiring wrapping",
			input:   make([]byte, 256),
			wantLen: true,
			maxLine: 64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := base64Wrap(tt.input)

			if tt.wantLen && result == "" {
				t.Error("expected non-empty result")
			}
			if !tt.wantLen && len(tt.input) == 0 && result != "" {
				t.Errorf("expected empty result for empty input, got %q", result)
			}

			if tt.maxLine > 0 {
				lines := strings.Split(result, "\n")
				for i, line := range lines {
					if len(line) > tt.maxLine {
						t.Errorf("line %d exceeds %d chars: %d", i, tt.maxLine, len(line))
					}
				}
			}

			// Verify the base64 decodes correctly
			if len(tt.input) > 0 {
				cleaned := strings.ReplaceAll(result, "\n", "")
				decoded, err := base64.StdEncoding.DecodeString(cleaned)
				if err != nil {
					t.Fatalf("failed to decode base64: %v", err)
				}
				if len(decoded) != len(tt.input) {
					t.Errorf("decoded length %d != input length %d", len(decoded), len(tt.input))
				}
			}
		})
	}
}

func TestEnsurePEMHeaders(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantHas string
	}{
		{
			name:    "already has headers",
			input:   "-----BEGIN CERTIFICATE REQUEST-----\ndata\n-----END CERTIFICATE REQUEST-----",
			wantHas: "-----BEGIN CERTIFICATE REQUEST-----",
		},
		{
			name:    "raw base64 without headers",
			input:   "MIICYzCCAUsCAQAwHjEcMBoGA1UEAwwTdGVzdC1jbGllbnQ=",
			wantHas: "-----BEGIN CERTIFICATE REQUEST-----",
		},
		{
			name:    "whitespace around",
			input:   "  \n  MIICdata  \n  ",
			wantHas: "-----BEGIN CERTIFICATE REQUEST-----",
		},
		{
			name:    "empty string",
			input:   "",
			wantHas: "-----BEGIN CERTIFICATE REQUEST-----",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ensurePEMHeaders(tt.input)
			if !strings.Contains(result, tt.wantHas) {
				t.Errorf("result should contain %q, got:\n%s", tt.wantHas, result)
			}
			// If input didn't have headers, result should have END header too
			if !strings.Contains(tt.input, "BEGIN CERTIFICATE REQUEST") {
				if !strings.Contains(result, "-----END CERTIFICATE REQUEST-----") {
					t.Error("result should have END header")
				}
			}
		})
	}
}

func TestRoutes(t *testing.T) {
	m, _ := testMartiAPI(t)

	routes := []struct {
		method string
		path   string
		status int
	}{
		{"GET", "/Marti/api/tls/config", http.StatusOK},
		{"GET", "/Marti/api/version/config", http.StatusOK},
		{"GET", "/Marti/api/contacts/all", http.StatusUnauthorized},
		{"GET", "/Marti/api/clientEndPoints", http.StatusUnauthorized},
		{"GET", "/Marti/api/tls/profile/enrollment", http.StatusNoContent},
		{"POST", "/Marti/api/tls/signClient", http.StatusUnauthorized},
		{"POST", "/Marti/api/tls/signClient/", http.StatusUnauthorized},
		{"POST", "/Marti/api/tls/signClient/v2", http.StatusUnauthorized},
	}

	for _, r := range routes {
		t.Run(fmt.Sprintf("%s %s", r.method, r.path), func(t *testing.T) {
			req := httptest.NewRequest(r.method, r.path, nil)
			w := httptest.NewRecorder()

			m.router.ServeHTTP(w, req)

			if w.Code != r.status {
				t.Errorf("expected %d, got %d", r.status, w.Code)
			}
		})
	}
}
