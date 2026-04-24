package management

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	qrcode "github.com/skip2/go-qrcode"
)

// handleEnrollmentQRCode generates a QR code PNG encoding a tak:// enrollment URI.
// ATAK 5.1+ can scan this QR code to auto-configure server enrollment.
func (a *ManagementAPI) handleEnrollmentQRCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hostname string `json:"hostname"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Hostname == "" || req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "hostname, username, and password are required")
		return
	}

	enrollURI := fmt.Sprintf("tak://com.atakmap.app/enroll?host=%s&username=%s&token=%s",
		url.QueryEscape(req.Hostname),
		url.QueryEscape(req.Username),
		url.QueryEscape(req.Password),
	)

	size := 512
	png, err := qrcode.Encode(enrollURI, qrcode.Medium, size)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate QR code: "+err.Error())
		return
	}

	a.auditor.LogQRCodeGenerated("management-api", req.Username, req.Hostname, r.RemoteAddr)

	// Return base64 JSON if the caller prefers JSON, otherwise raw PNG.
	if r.Header.Get("Accept") == "application/json" {
		writeJSON(w, http.StatusOK, map[string]string{
			"uri":    enrollURI,
			"qrcode": base64.StdEncoding.EncodeToString(png),
		})
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="enroll-%s.png"`, req.Username))
	w.Write(png)
}
