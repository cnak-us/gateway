package marti

import (
	"encoding/json"
	"net/http"
	"time"
)

// Contact represents a connected TAK client.
type Contact struct {
	UID           string `json:"uid"`
	Callsign      string `json:"callsign"`
	LastEventTime string `json:"lastEventTime"`
	LastStatus    string `json:"lastStatus"`
}

// handleContactsAll returns all known contacts as a JSON array.
// Requires Basic Auth to prevent unauthenticated client enumeration.
func (m *MartiAPI) handleContactsAll(w http.ResponseWriter, r *http.Request) {
	if _, ok := validateBasicAuth(r.Header.Get("Authorization"), m.credentials); !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="TAK Server"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	contacts := []Contact{}

	if m.registry != nil {
		all, err := m.registry.ListAll()
		if err == nil {
			for _, c := range all {
				contacts = append(contacts, Contact{
					UID:           c.UID,
					Callsign:      c.Callsign,
					LastEventTime: c.LastSeen.Format(time.RFC3339),
					LastStatus:    "Connected",
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contacts)
}
