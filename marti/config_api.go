package marti

import (
	"encoding/json"
	"net/http"
)

// handleVersionConfig returns server version and configuration information.
func (m *MartiAPI) handleVersionConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"version": "3",
		"type":    "ServerConfig",
		"data": map[string]any{
			"version":   "tak-gateway-1.0.0",
			"api":       "3",
			"hostname":  r.Host,
			"tls":       true,
			"tlsPort":   m.cfg.TLSStreamingPort,
			"httpsPort": m.cfg.HTTPSAPIPort,
		},
	})
}
