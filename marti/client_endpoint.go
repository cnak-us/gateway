package marti

import (
	"encoding/json"
	"net/http"
	"time"
)

// ClientEndpoint represents a TAK client connection endpoint.
type ClientEndpoint struct {
	CallSign      string `json:"callsign"`
	UID           string `json:"uid"`
	LastEventTime string `json:"lastEventTime"`
	LastStatus    string `json:"lastStatus"`
}

// handleClientEndPoints returns all connected client endpoints as a JSON array.
// Requires Basic Auth to prevent unauthenticated client enumeration.
func (m *MartiAPI) handleClientEndPoints(w http.ResponseWriter, r *http.Request) {
	if _, ok := validateBasicAuth(r.Header.Get("Authorization"), m.credentials); !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="TAK Server"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	endpoints := []ClientEndpoint{}

	if m.registry != nil {
		all, err := m.registry.ListAll()
		if err == nil {
			for _, c := range all {
				endpoints = append(endpoints, ClientEndpoint{
					CallSign:      c.Callsign,
					UID:           c.UID,
					LastEventTime: c.LastSeen.Format(time.RFC3339),
					LastStatus:    "Connected",
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(endpoints)
}
