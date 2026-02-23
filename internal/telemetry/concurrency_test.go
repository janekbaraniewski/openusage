package telemetry

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestConcurrentIngestAndReadModel_NoLockErrors(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	base := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter",
			AccountID:  "openrouter",
			Status:     core.StatusOK,
			Metrics:    map[string]core.Metric{},
		},
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			msgID := "msg-" + time.Now().UTC().Add(time.Duration(i)*time.Millisecond).Format("150405.000") + "-" + string(rune('a'+(i%26)))
			in := int64(10 + (i % 7))
			out := int64(4 + (i % 3))
			total := in + out
			_, _ = store.Ingest(ctx, IngestRequest{
				SourceSystem:  SourceSystem("opencode"),
				SourceChannel: SourceChannelHook,
				OccurredAt:    time.Now().UTC(),
				ProviderID:    "openrouter",
				AccountID:     "openrouter",
				AgentName:     "opencode",
				EventType:     EventTypeMessageUsage,
				MessageID:     msgID,
				ModelRaw:      "qwen/qwen3-coder-flash",
				InputTokens:   &in,
				OutputTokens:  &out,
				TotalTokens:   &total,
				Requests:      int64Ptr(1),
			})
		}
	}()

	for i := 0; i < 300; i++ {
		if _, err := ApplyCanonicalTelemetryViewWithOptions(ctx, dbPath, base, ReadModelOptions{}); err != nil {
			t.Fatalf("read-model query failed under concurrent ingest: %v", err)
		}
	}

	wg.Wait()
}
