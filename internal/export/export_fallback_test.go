package export

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
)

// slowReadModelClient is healthy but its read-model never answers before the
// caller's deadline, like an overloaded daemon.
type slowReadModelClient struct{}

func (slowReadModelClient) FetchReadModel(ctx context.Context, _ daemon.ReadModelRequest) (daemon.ReadModelResponse, error) {
	<-ctx.Done()
	return daemon.ReadModelResponse{}, ctx.Err()
}

type pendingReadModelClient struct{}

func (pendingReadModelClient) FetchReadModel(context.Context, daemon.ReadModelRequest) (daemon.ReadModelResponse, error) {
	return daemon.ReadModelResponse{Snapshots: map[string]core.UsageSnapshot{}, Pending: true}, nil
}

func noDirect(called *bool) *directCollector {
	return &directCollector{
		loadConfig: func() (config.Config, error) {
			*called = true
			return config.Config{}, nil
		},
		resolveAccts: func(*config.Config) []core.AccountConfig { return nil },
	}
}

type capturingReadModelClient struct{ got *daemon.ReadModelRequest }

func (c capturingReadModelClient) FetchReadModel(_ context.Context, req daemon.ReadModelRequest) (daemon.ReadModelResponse, error) {
	*c.got = req
	return daemon.ReadModelResponse{Snapshots: map[string]core.UsageSnapshot{
		"a": {ProviderID: "openai", AccountID: "a", Status: core.StatusOK},
	}}, nil
}

// The client must never resolve accounts itself: account auto-detection
// spawns subprocesses, can rewrite settings.json and takes over a second,
// which alone exceeds tmux's render budget. The daemon collector has no
// account resolver at all and must send an account-less request so the
// daemon answers from its own cached account set.
func TestDaemonCollectorSendsNoAccounts(t *testing.T) {
	var got daemon.ReadModelRequest
	c := &daemonCollector{
		socketPath: "/nonexistent.sock",
		newClient:  func(string) daemonClient { return capturingReadModelClient{got: &got} },
	}
	snaps, err := c.collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(snaps))
	}
	if len(got.Accounts) != 0 {
		t.Fatalf("client sent %d accounts; it must leave resolution to the daemon", len(got.Accounts))
	}
}

// A daemon still computing its first read model answers from cache on the
// next render, so auto mode must not start an expensive direct collect.
func TestCollectAutoSkipsDirectFallbackWhenDaemonPending(t *testing.T) {
	directCalled := false
	r := &runner{
		dmn: &daemonCollector{
			socketPath: "/nonexistent.sock",
			newClient:  func(string) daemonClient { return pendingReadModelClient{} },
		},
		direct: noDirect(&directCalled),
		stderr: io.Discard,
	}
	if _, _, err := r.collect(context.Background(), SourceAuto); err == nil {
		t.Fatal("expected a pending error")
	}
	if directCalled {
		t.Fatal("direct fallback ran while the daemon was computing")
	}
}

// With no daemon listening, auto mode falls back to direct in one step
// (no separate health probe).
func TestCollectAutoFallsBackWhenDaemonUnreachable(t *testing.T) {
	directCalled := false
	r := &runner{
		dmn:    newDaemonCollector(filepath.Join(t.TempDir(), "missing.sock")),
		direct: noDirect(&directCalled),
		stderr: io.Discard,
	}
	_, src, _ := r.collect(context.Background(), SourceAuto)
	if !directCalled || src != SourceDirect {
		t.Fatalf("expected direct fallback, got source=%s direct=%v", src, directCalled)
	}
}

// TestCollectAutoSkipsDirectFallbackWhenBudgetSpent locks in the power fix:
// once a slow daemon read has consumed the caller's deadline, auto mode must
// not start a direct collect (a full local-history parse on every tmux
// render) whose result could no longer be used.
func TestCollectAutoSkipsDirectFallbackWhenBudgetSpent(t *testing.T) {
	directCalled := false
	r := &runner{
		dmn: &daemonCollector{
			socketPath: "/nonexistent.sock",
			newClient:  func(string) daemonClient { return slowReadModelClient{} },
		},
		direct: &directCollector{
			loadConfig: func() (config.Config, error) {
				directCalled = true
				return config.Config{}, nil
			},
			resolveAccts: func(*config.Config) []core.AccountConfig { return nil },
		},
		stderr: io.Discard,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := r.collect(ctx, SourceAuto); err == nil {
		t.Fatal("expected an error when the daemon read exhausted the deadline")
	}
	if directCalled {
		t.Fatal("direct fallback ran after the caller's deadline had already passed")
	}
}
