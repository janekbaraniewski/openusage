package export

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
)

// slowReadModelClient is healthy but its read-model never answers before the
// caller's deadline, like an overloaded daemon.
type slowReadModelClient struct{}

func (slowReadModelClient) HealthInfo(context.Context) (daemon.HealthResponse, error) {
	return daemon.HealthResponse{}, nil
}

func (slowReadModelClient) ReadModel(ctx context.Context, _ daemon.ReadModelRequest) (map[string]core.UsageSnapshot, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestCollectAutoSkipsDirectFallbackWhenBudgetSpent locks in the power fix:
// once a slow daemon read has consumed the caller's deadline, auto mode must
// not start a direct collect (a full local-history parse on every tmux
// render) whose result could no longer be used.
func TestCollectAutoSkipsDirectFallbackWhenBudgetSpent(t *testing.T) {
	directCalled := false
	r := &runner{
		dmn: &daemonCollector{
			socketPath:  "/nonexistent.sock",
			newClient:   func(string) daemonClient { return slowReadModelClient{} },
			buildReqCfg: func() (daemon.ReadModelRequest, error) { return daemon.ReadModelRequest{}, nil },
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
