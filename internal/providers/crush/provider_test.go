package crush

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// TestProvider_Fetch_DBPathsRawKeys verifies that multi-path discovery
// produces one printable Raw key per DB and that no joined-string key
// with non-printable separators is exposed.
func TestProvider_Fetch_DBPathsRawKeys(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.db")
	pathB := filepath.Join(dir, "b.db")
	for _, p := range []string{pathA, pathB} {
		if err := os.WriteFile(p, []byte("not a real sqlite db"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	provider := New()
	acct := core.AccountConfig{ID: "crush", Provider: "crush", Auth: "local"}
	acct.SetPath(PathHintDBsKey, strings.Join([]string{pathA, pathB}, string(os.PathListSeparator)))

	snap, _ := provider.Fetch(context.Background(), acct)

	if got := snap.Raw["db_paths.0"]; got != pathA {
		t.Errorf("Raw[db_paths.0] = %q, want %q", got, pathA)
	}
	if got := snap.Raw["db_paths.1"]; got != pathB {
		t.Errorf("Raw[db_paths.1] = %q, want %q", got, pathB)
	}
	if joined, exists := snap.Raw["db_paths"]; exists {
		t.Errorf("legacy joined Raw[db_paths] still set: %q", joined)
	}
	if got := snap.Raw["db_count"]; got != "2" {
		t.Errorf("Raw[db_count] = %q, want %q", got, "2")
	}
}
