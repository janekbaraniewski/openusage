package export

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
)

// daemonHealthTimeout caps the probe used to decide whether the daemon is
// reachable. Kept short so `--source auto` falls back to direct mode quickly
// when the daemon is not running.
const daemonHealthTimeout = 1500 * time.Millisecond

// daemonReadModelTimeout caps the read-model request. The daemon usually
// serves from cache, so this should be plenty even on slow machines.
const daemonReadModelTimeout = 5 * time.Second

// daemonClient is the minimum daemon surface the export package uses. It is
// satisfied by *daemon.Client; tests substitute a stub.
type daemonClient interface {
	HealthInfo(ctx context.Context) (daemon.HealthResponse, error)
	ReadModel(ctx context.Context, request daemon.ReadModelRequest) (map[string]core.UsageSnapshot, error)
}

// daemonCollector pulls the current read-model from a running daemon.
type daemonCollector struct {
	socketPath  string
	newClient   func(socketPath string) daemonClient
	buildReqCfg func() (daemon.ReadModelRequest, error)
}

func newDaemonCollector(socketPath string) *daemonCollector {
	return &daemonCollector{
		socketPath: strings.TrimSpace(socketPath),
		newClient: func(path string) daemonClient {
			return daemon.NewClient(path)
		},
		buildReqCfg: daemon.BuildReadModelRequestFromConfig,
	}
}

// available reports whether the daemon is reachable at the configured socket.
// Used by SourceAuto to decide between daemon and direct collection.
func (c *daemonCollector) available(ctx context.Context) bool {
	if c == nil || c.socketPath == "" {
		return false
	}
	client := c.newClient(c.socketPath)
	probeCtx, cancel := context.WithTimeout(ctx, daemonHealthTimeout)
	defer cancel()
	_, err := client.HealthInfo(probeCtx)
	return err == nil
}

// collect connects to the daemon and returns the current read-model
// snapshots. Returns an actionable error if the daemon is unreachable; callers
// in `--source auto` mode should treat the error as a signal to fall back to
// direct mode, while `--source daemon` should surface it to the user.
func (c *daemonCollector) collect(ctx context.Context) ([]core.UsageSnapshot, error) {
	if c == nil || c.socketPath == "" {
		return nil, errors.New("export: telemetry daemon socket path is not configured")
	}

	client := c.newClient(c.socketPath)

	probeCtx, cancel := context.WithTimeout(ctx, daemonHealthTimeout)
	_, healthErr := client.HealthInfo(probeCtx)
	cancel()
	if healthErr != nil {
		return nil, fmt.Errorf(
			"export: telemetry daemon unreachable at %s: %w (start it with 'openusage telemetry daemon install' or rerun with --source direct)",
			c.socketPath, healthErr,
		)
	}

	req, reqErr := c.buildReqCfg()
	if reqErr != nil {
		return nil, fmt.Errorf("export: building read-model request: %w", reqErr)
	}

	readCtx, readCancel := context.WithTimeout(ctx, daemonReadModelTimeout)
	defer readCancel()
	snapMap, err := client.ReadModel(readCtx, req)
	if err != nil {
		return nil, fmt.Errorf("export: reading snapshots from daemon: %w", err)
	}

	out := make([]core.UsageSnapshot, 0, len(snapMap))
	for _, snap := range snapMap {
		out = append(out, snap)
	}
	sortSnapshots(out)
	return out, nil
}
