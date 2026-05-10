package detect

// maskKey returns a redacted form of a secret suitable for logging:
// "abcd...wxyz" for keys longer than 12 characters, "****" otherwise.
// Centralised here so every detector logs credentials the same way.
func maskKey(key string) string {
	if len(key) <= 12 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
