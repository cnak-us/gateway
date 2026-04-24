package bridge

import "github.com/cnak-us/cnak/pkg/cot"

// escapeXML wraps cot.EscapeXML for use in tests.
func escapeXML(s string) string {
	return cot.EscapeXML(s)
}
