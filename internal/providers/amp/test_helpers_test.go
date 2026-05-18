package amp

import (
	"os"
	"time"
)

// writeFileImpl is a shared helper for test files in this package that need
// to drop a small fixture into a tempdir.
func writeFileImpl(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// mustParseTime parses an RFC3339 timestamp or panics. Test-only convenience.
func mustParseTime(s string) time.Time {
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return ts
}
