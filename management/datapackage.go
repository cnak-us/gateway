package management

import (
	"archive/zip"
	"bytes"
	"fmt"

	"github.com/google/uuid"
	"github.com/cnak-us/gateway/ca"
)

// GenerateDataPackage creates a TAK data package ZIP for client onboarding.
// The package contains a connection profile and CA trust store that ATAK/WinTAK/iTAK
// can import to configure a TLS connection to the gateway.
func GenerateDataPackage(hostname string, caCertPEM []byte, trustStorePassword string) ([]byte, error) {
	// Generate trust store PKCS#12
	trustStoreP12, err := ca.GenerateTrustStoreP12(caCertPEM, trustStorePassword)
	if err != nil {
		return nil, fmt.Errorf("generating trust store: %w", err)
	}

	uid := uuid.New().String()

	// Build connection preferences XML.
	// caLocation0 must match the zip entry path for the trust store.
	// caPassword0 must match the password used when generating the P12.
	configPref := fmt.Sprintf(`<?xml version='1.0' standalone='yes'?>
<preferences>
  <preference version="1" name="cot_streams">
    <entry key="count" class="class java.lang.Integer">1</entry>
    <entry key="description0" class="class java.lang.String">CNAK TAK Gateway</entry>
    <entry key="enabled0" class="class java.lang.Boolean">true</entry>
    <entry key="connectString0" class="class java.lang.String">%s:8089:ssl</entry>
    <entry key="useAuth0" class="class java.lang.Boolean">true</entry>
    <entry key="cacheCreds0" class="class java.lang.String">Cache credentials</entry>
    <entry key="enrollForCertificateWithTrust0" class="class java.lang.Boolean">true</entry>
    <entry key="caLocation0" class="class java.lang.String">cert/truststore-root.p12</entry>
    <entry key="caPassword0" class="class java.lang.String">%s</entry>
  </preference>
</preferences>`, hostname, trustStorePassword)

	// Build manifest XML
	manifest := fmt.Sprintf(`<MissionPackageManifest version="2">
  <Configuration>
    <Parameter name="uid" value="%s"/>
    <Parameter name="name" value="CNAK-Connection.zip"/>
    <Parameter name="onReceiveDelete" value="true"/>
  </Configuration>
  <Contents>
    <Content ignore="false" zipEntry="cert/config.pref"/>
    <Content ignore="false" zipEntry="cert/truststore-root.p12"/>
  </Contents>
</MissionPackageManifest>`, uid)

	// Create ZIP archive
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	files := map[string][]byte{
		"cert/config.pref":         []byte(configPref),
		"cert/truststore-root.p12": trustStoreP12,
		"MANIFEST/manifest.xml":    []byte(manifest),
	}

	for name, data := range files {
		f, err := w.Create(name)
		if err != nil {
			return nil, fmt.Errorf("creating zip entry %s: %w", name, err)
		}
		if _, err := f.Write(data); err != nil {
			return nil, fmt.Errorf("writing zip entry %s: %w", name, err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("closing zip writer: %w", err)
	}

	return buf.Bytes(), nil
}
