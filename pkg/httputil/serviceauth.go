// Package httputil provides common HTTP middleware and handlers.
package httputil

import (
	"context"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// Context keys for service auth identity propagation.
type contextKey string

const (
	ctxServiceUser   contextKey = "service_auth_user"
	ctxServiceGroups contextKey = "service_auth_groups"
	ctxServiceAuth   contextKey = "service_auth_type"
)

// ServiceAuthMiddleware validates that inbound requests carry a valid
// X-Service-Token header matching the SERVICE_TOKEN environment variable.
// On success it extracts X-Auth-User, X-Auth-Groups, and X-Auth-Type into
// the request context so handlers can retrieve caller identity via
// ServiceUser().
//
// If SERVICE_TOKEN is empty the middleware passes all requests through
// (development / binary mode where no inter-service auth is configured).
func ServiceAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := os.Getenv("SERVICE_TOKEN")
		if expected == "" {
			// No token configured — allow all (dev mode).
			next.ServeHTTP(w, r)
			return
		}

		provided := r.Header.Get("X-Service-Token")
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Propagate identity headers into context.
		ctx := r.Context()
		if user := r.Header.Get("X-Auth-User"); user != "" {
			ctx = context.WithValue(ctx, ctxServiceUser, user)
		}
		if groups := r.Header.Get("X-Auth-Groups"); groups != "" {
			ctx = context.WithValue(ctx, ctxServiceGroups, strings.Split(groups, ","))
		}
		if authType := r.Header.Get("X-Auth-Type"); authType != "" {
			ctx = context.WithValue(ctx, ctxServiceAuth, authType)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ServiceUser extracts the caller's email and group memberships from the
// request context (populated by ServiceAuthMiddleware).
func ServiceUser(r *http.Request) (email string, groups []string) {
	if v, ok := r.Context().Value(ctxServiceUser).(string); ok {
		email = v
	}
	if v, ok := r.Context().Value(ctxServiceGroups).([]string); ok {
		groups = v
	}
	return
}
