package exporter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestNew_EmptyTarget(t *testing.T) {
	_, err := New(config.ExportConfig{Target: ""})
	if err == nil {
		t.Fatal("expected error for empty target, got nil")
	}
}

func TestNew_DefaultInterval(t *testing.T) {
	e, err := New(config.ExportConfig{Target: "http://example.com", IntervalSeconds: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.interval != 60*time.Second {
		t.Errorf("expected 60s interval, got %v", e.interval)
	}
}

func TestNew_NegativeInterval(t *testing.T) {
	e, err := New(config.ExportConfig{Target: "http://example.com", IntervalSeconds: -5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.interval != 60*time.Second {
		t.Errorf("expected 60s interval, got %v", e.interval)
	}
}

func TestNew_MachineName_FromConfig(t *testing.T) {
	e, err := New(config.ExportConfig{Target: "http://example.com", MachineName: "my-machine"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.machine != "my-machine" {
		t.Errorf("expected machine %q, got %q", "my-machine", e.machine)
	}
}

func TestNew_MachineName_FromHostname(t *testing.T) {
	expectedHostname, _ := os.Hostname()
	e, err := New(config.ExportConfig{Target: "http://example.com", MachineName: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.machine != expectedHostname {
		t.Errorf("expected hostname %q, got %q", expectedHostname, e.machine)
	}
}

func TestIngest_ReplaceLatest(t *testing.T) {
	e, _ := New(config.ExportConfig{Target: "http://example.com", MachineName: "test"})

	snaps := map[string]core.UsageSnapshot{
		"a": {ProviderID: "a"},
		"b": {ProviderID: "b"},
	}
	e.Ingest(snaps)

	e.mu.RLock()
	count := len(e.latest)
	e.mu.RUnlock()

	if count != 2 {
		t.Errorf("expected 2 snapshots, got %d", count)
	}

	// Replace with a single snapshot
	e.Ingest(map[string]core.UsageSnapshot{
		"c": {ProviderID: "c"},
	})

	e.mu.RLock()
	count = len(e.latest)
	e.mu.RUnlock()

	if count != 1 {
		t.Errorf("expected 1 snapshot after replace, got %d", count)
	}
}

func TestStart_EmptySnapshotsNoPOST(t *testing.T) {
	var mu sync.Mutex
	postCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		postCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	e, _ := New(config.ExportConfig{
		Target:          srv.URL,
		MachineName:     "test-machine",
		IntervalSeconds: 1,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	// Do NOT call Ingest — latest remains empty
	e.Start(ctx)

	mu.Lock()
	got := postCount
	mu.Unlock()

	if got != 0 {
		t.Errorf("expected 0 POST calls with empty snapshots, got %d", got)
	}
}

func TestStart_PushesEnvelope(t *testing.T) {
	var mu sync.Mutex
	var received []core.RemoteEnvelope

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/push" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var env core.RemoteEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			t.Errorf("failed to unmarshal envelope: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		received = append(received, env)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	e, _ := New(config.ExportConfig{
		Target:          srv.URL,
		MachineName:     "my-host",
		IntervalSeconds: 1,
	})

	e.Ingest(map[string]core.UsageSnapshot{
		"openai": {ProviderID: "openai"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	e.Start(ctx)

	mu.Lock()
	envs := received
	mu.Unlock()

	if len(envs) == 0 {
		t.Fatal("expected at least one envelope to be pushed")
	}

	env := envs[0]
	if env.Machine != "my-host" {
		t.Errorf("expected machine %q, got %q", "my-host", env.Machine)
	}
	if len(env.Snapshots) != 1 {
		t.Errorf("expected 1 snapshot, got %d", len(env.Snapshots))
	}
	if env.Snapshots[0].ProviderID != "openai" {
		t.Errorf("expected provider ID %q, got %q", "openai", env.Snapshots[0].ProviderID)
	}
}

func TestStart_HTTPErrorLogsAndContinues(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	e, _ := New(config.ExportConfig{
		Target:          srv.URL,
		MachineName:     "test-machine",
		IntervalSeconds: 1,
	})

	e.Ingest(map[string]core.UsageSnapshot{
		"x": {ProviderID: "x"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	// Should not panic despite HTTP errors
	e.Start(ctx)

	mu.Lock()
	got := callCount
	mu.Unlock()

	if got < 2 {
		t.Errorf("expected at least 2 push attempts despite errors, got %d", got)
	}
}
