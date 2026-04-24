package natsutil

import "strings"

// SanitizeSubjectToken replaces characters that are special in NATS subject
// tokens (`.` is a level separator, `*` and `>` are wildcards, space is
// invalid) with underscores. This prevents group names or other user-supplied
// values from injecting extra subject levels or matching unintended subjects.
func SanitizeSubjectToken(s string) string {
	r := strings.NewReplacer(
		".", "_",
		"*", "_",
		">", "_",
		" ", "_",
	)
	return r.Replace(s)
}
