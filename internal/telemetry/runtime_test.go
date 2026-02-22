package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

type runtimeTestCollector struct {
	name string
	reqs []IngestRequest
	err  error
}

func (c runtimeTestCollector) Name() string { return c.name }

func (c runtimeTestCollector) Collect(context.Context) ([]IngestRequest, error) {
	if c.err != nil {
		return nil, c.err
	}
	out := make([]IngestRequest, len(c.reqs))
	copy(out, c.reqs)
	return out, nil
}

func TestAutoCollector_CollectAndFlush(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	pipeline := NewPipeline(store, NewSpool(t.TempDir()))
	base := IngestRequest{
		SourceSystem:  SourceSystem("opencode"),
		SourceChannel: SourceChannelHook,
		OccurredAt:    time.Date(2026, 2, 22, 12, 0, 0, 0, time.UTC),
		ProviderID:    "openrouter",
		AccountID:     "zen",
		AgentName:     "opencode",
		EventType:     EventTypeMessageUsage,
		MessageID:     "msg-1",
		ModelRaw:      "qwen/qwen3-coder-flash",
		InputTokens:   int64Ptr(10),
		OutputTokens:  int64Ptr(5),
		TotalTokens:   int64Ptr(15),
	}

	collector := NewAutoCollector([]Collector{
		runtimeTestCollector{name: "first", reqs: []IngestRequest{base}},
		runtimeTestCollector{name: "second", reqs: []IngestRequest{base}},
	}, pipeline, 0)

	got, err := collector.CollectAndFlush(context.Background())
	if err != nil {
		t.Fatalf("collect and flush: %v", err)
	}
	if got.Collected != 2 {
		t.Fatalf("collected = %d, want 2", got.Collected)
	}
	if got.Enqueued != 2 {
		t.Fatalf("enqueued = %d, want 2", got.Enqueued)
	}
	if got.Flush.Ingested != 1 {
		t.Fatalf("ingested = %d, want 1", got.Flush.Ingested)
	}
	if got.Flush.Deduped != 1 {
		t.Fatalf("deduped = %d, want 1", got.Flush.Deduped)
	}

	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.RawEvents != 2 {
		t.Fatalf("raw events = %d, want 2", stats.RawEvents)
	}
	if stats.CanonicalEvents != 1 {
		t.Fatalf("canonical events = %d, want 1", stats.CanonicalEvents)
	}
}

func TestAutoCollector_CollectAndFlush_WithCollectorError(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	pipeline := NewPipeline(store, NewSpool(t.TempDir()))
	collector := NewAutoCollector([]Collector{
		runtimeTestCollector{name: "broken", err: context.DeadlineExceeded},
	}, pipeline, 0)

	got, err := collector.CollectAndFlush(context.Background())
	if err != nil {
		t.Fatalf("collect and flush: %v", err)
	}
	if len(got.CollectorErr) != 1 {
		t.Fatalf("collector errors = %d, want 1", len(got.CollectorErr))
	}
}
