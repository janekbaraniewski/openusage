package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/antigravity"
)

func TestEnrichReadModelSnapshots_HydratesUnknownFromStatusFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "antigravity-mohammed-status.json")
	payload := `{
		"agent_state": "idle",
		"email": "mohammed@example.com",
		"model": {"id": "gemini-2.5-pro", "display_name": "Gemini 2.5 Pro"},
		"product": "antigravity",
		"quota": {
			"gemini": {"remaining_fraction": 0.96, "reset_in_seconds": 3600}
		},
		"received_at": "` + time.Now().UTC().Format(time.RFC3339Nano) + `"
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write status file: %v", err)
	}

	svc := &Service{
		providerByID: map[string]core.UsageProvider{
			"antigravity": antigravity.New(),
		},
	}
	accounts := []core.AccountConfig{{
		ID:       "antigravity-mohammed",
		Provider: "antigravity",
		RuntimeHints: map[string]string{
			"status_file": path,
		},
	}}
	snaps := map[string]core.UsageSnapshot{
		"antigravity-mohammed": {
			ProviderID: "antigravity",
			AccountID:  "antigravity-mohammed",
			Status:     core.StatusUnknown,
		},
	}

	got := svc.enrichReadModelSnapshots(context.Background(), accounts, core.DefaultModelNormalizationConfig(), snaps)
	snap := got["antigravity-mohammed"]
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %q, want OK", snap.Status)
	}
	if len(snap.Metrics) == 0 {
		t.Fatalf("expected quota metrics after hydration, got metrics=%v", snap.Metrics)
	}
}

func TestOverlayPollStateSnapshots_PrefersPolledData(t *testing.T) {
	now := time.Now().UTC()
	rem := 70.33
	svc := &Service{
		pollState: map[string]*providerPollState{
			"antigravity-physics": {
				hasSnap: true,
				lastSnap: core.UsageSnapshot{
					ProviderID: "antigravity",
					AccountID:  "antigravity-physics",
					Status:     core.StatusOK,
					Timestamp:  now,
					Metrics: map[string]core.Metric{
						"quota_gemini_weekly": {Remaining: &rem},
					},
				},
			},
		},
	}
	snaps := map[string]core.UsageSnapshot{
		"antigravity-physics": {
			ProviderID: "antigravity",
			AccountID:  "antigravity-physics",
			Status:     core.StatusUnknown,
			Timestamp:  now.Add(-time.Hour),
		},
	}

	got := svc.overlayPollStateSnapshots(snaps)
	if got["antigravity-physics"].Status != core.StatusOK {
		t.Fatalf("status = %q, want OK", got["antigravity-physics"].Status)
	}
}
