package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

// TestChangeDetectorProviders verifies that the expected providers implement ChangeDetector.
func TestChangeDetectorProviders(t *testing.T) {
	expectedDetectors := map[string]bool{
		"claude_code": true,
		"cursor":      true,
		"codex":       true,
		"gemini_cli":  true,
		"copilot":     true,
		"ollama":      true,
	}

	for _, provider := range providers.AllProviders() {
		_, isDetector := provider.(core.ChangeDetector)
		id := provider.ID()

		if expectedDetectors[id] && !isDetector {
			t.Errorf("provider %q should implement ChangeDetector but doesn't", id)
		}
	}
}

// TestChangeDetectorReturnsTrue_WhenFileModified verifies the basic contract:
// if a file is modified after `since`, HasChanged returns true.
func TestChangeDetectorReturnsTrue_WhenFileModified(t *testing.T) {
	tmpDir := t.TempDir()
	projectsDir := filepath.Join(tmpDir, "projects")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a file, then check with a time before the write.
	testFile := filepath.Join(projectsDir, "test.jsonl")
	if err := os.WriteFile(testFile, []byte(`{"test":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Find the claude_code provider and test it.
	for _, provider := range providers.AllProviders() {
		if provider.ID() != "claude_code" {
			continue
		}
		detector := provider.(core.ChangeDetector)
		acct := core.AccountConfig{
			ID:       "test",
			Provider: "claude_code",
			RuntimeHints: map[string]string{
				"claude_dir": tmpDir,
			},
		}

		// Since time is before file creation — should report changed.
		changed, err := detector.HasChanged(acct, time.Now().Add(-1*time.Hour))
		if err != nil {
			t.Fatalf("HasChanged error: %v", err)
		}
		if !changed {
			t.Error("expected HasChanged=true when file is newer than since")
		}

		// Since time is after file creation — should report not changed.
		changed, err = detector.HasChanged(acct, time.Now().Add(1*time.Hour))
		if err != nil {
			t.Fatalf("HasChanged error: %v", err)
		}
		if changed {
			t.Error("expected HasChanged=false when file is older than since")
		}
	}
}

// TestChangeDetectorReturnsFalse_WhenNoFiles verifies that if data dirs don't exist,
// HasChanged returns false (not an error).
func TestChangeDetectorReturnsFalse_WhenNoFiles(t *testing.T) {
	for _, provider := range providers.AllProviders() {
		detector, ok := provider.(core.ChangeDetector)
		if !ok {
			continue
		}

		acct := core.AccountConfig{
			ID:       "test",
			Provider: provider.ID(),
			RuntimeHints: map[string]string{
				"claude_dir":   "/nonexistent/path",
				"config_dir":   "/nonexistent/path",
				"sessions_dir": "/nonexistent/path",
			},
			ProviderPaths: map[string]string{
				"tracking_db": "/nonexistent/path/tracking.db",
				"state_db":    "/nonexistent/path/state.db",
			},
		}

		changed, err := detector.HasChanged(acct, time.Now().Add(-1*time.Hour))
		if err != nil {
			t.Errorf("provider %q: HasChanged should not error for missing paths, got: %v", provider.ID(), err)
		}
		if changed {
			t.Errorf("provider %q: HasChanged should return false for missing paths", provider.ID())
		}
	}
}

func TestSnapshotResetPassed_ReturnsTrueWhenResetBoundaryCrossed(t *testing.T) {
	since := time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC)
	now := since.Add(5 * time.Minute)
	snap := core.UsageSnapshot{
		Resets: map[string]time.Time{
			"usage_five_hour": since.Add(2 * time.Minute),
		},
	}

	if !snapshotResetPassed(snap, since, now) {
		t.Fatal("expected reset boundary to force refresh")
	}
}

func TestSnapshotResetPassed_IgnoresFutureAndHistoricalResets(t *testing.T) {
	since := time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC)
	now := since.Add(5 * time.Minute)
	snap := core.UsageSnapshot{
		Resets: map[string]time.Time{
			"old":    since.Add(-time.Minute),
			"future": since.Add(20 * time.Minute),
		},
	}

	if snapshotResetPassed(snap, since, now) {
		t.Fatal("expected no refresh when resets are outside the interval")
	}
}
