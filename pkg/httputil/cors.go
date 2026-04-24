// Package httputil provides common HTTP middleware and handlers.
package httputil

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
)

// defaultDevOrigins are allowed when ALLOWED_ORIGINS is not set and
// DEPLOYMENT_MODE is not "binary". This prevents reflecting arbitrary
// attacker-controlled origins while still working for local development.
var defaultDevOrigins = []string{
	"http://localhost:5173",
	"http://localhost:8080",
	"http://127.0.0.1:5173",
	"http://127.0.0.1:8080",
}

var corsWarningOnce sync.Once

// CORS middleware adds CORS headers to responses.
// When ALLOWED_ORIGINS is set, only those origins are allowed.
// When empty, allows only localhost dev origins (not arbitrary origins).
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowedOrigins := os.Getenv("ALLOWED_ORIGINS")

		if allowedOrigins == "" {
			deployMode := os.Getenv("DEPLOYMENT_MODE")
			if deployMode == "binary" {
				// Binary mode: same-origin only, no CORS headers needed
			} else {
				// Development mode: allow only known localhost origins
				corsWarningOnce.Do(func() {
					slog.Warn("ALLOWED_ORIGINS not set, using localhost defaults only")
				})
				for _, allowed := range defaultDevOrigins {
					if origin == allowed {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						w.Header().Set("Vary", "Origin")
						break
					}
				}
			}
		} else {
			// Production mode - check whitelist
			for _, allowed := range strings.Split(allowedOrigins, ",") {
				if origin == strings.TrimSpace(allowed) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					break
				}
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Service-Token")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// Security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
