package detect

import "strings"

// MaskKey returns a redacted form of a secret suitable for logging:
// "first4...last4" for keys longer than 12 characters, "****" otherwise.
// Whitespace around the input is trimmed before measuring length so that
// values pulled from rc files (which may have trailing newlines) mask the
// same way as values pulled from env vars.
//
// This is the single source of truth for credential redaction across both
// the internal/detect package and the cmd/openusage detect subcommand.
func MaskKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 12 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// maskKey is a package-private alias kept for terse use inside the detect
// package's own logging. New code may use either.
func maskKey(key string) string { return MaskKey(key) }
