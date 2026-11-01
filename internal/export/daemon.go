package export

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
)

// daemonReadModelTimeout caps the read-model request. The daemon usually
// serves from cache, so this should be plenty even on slow machines.
const daemonReadModelTimeout = 5 * time.Second

// errDaemonUnreachable marks a failure to connect to the daemon socket at
// all (not running), as opposed to a daemon that answered badly or slowly.
var errDaemonUnreachable = errors.New("telemetry daemon unreachable")

// errDaemonPending marks a daemon that is up but still computing its first
// read model for this request. Auto mode must not fall back to a direct
// collect for this: the daemon answers from cache on the next render.
var errDaemonPending = errors.New("telemetry daemon is still computing usage data")

// daemonClient is the minimum daemon surface the export package uses. It is
// satisfied by *daemon.Client; tests substitute a stub.
type daemonClient interface {
	FetchReadModel(ctx context.Context, request daemon.ReadModelRequest) (daemon.ReadModelResponse, error)
}

// daemonCollector pulls the current read-model from a running daemon.
type daemonCollector struct {
	socketPath string
	newClient  func(socketPath string) daemonClient
}

func newDaemonCollector(socketPath string) *daemonCollector {
	return &daemonCollector{
		socketPath: strings.TrimSpace(socketPath),
		newClient: func(path string) daemonClient {
			return daemon.NewClient(path)
		},
	}
}

// collect asks the daemon for the current read-model snapshots in a single
// round trip. The request carries no accounts: the daemon resolves and caches
// them itself, so the client never runs account auto-detection (about a
// second) and never does a separate /healthz probe. A daemon that is not
// running fails at connect time with errDaemonUnreachable, which callers in
// `--source auto` treat as the signal to use direct mode.
func (c *daemonCollector) collect(ctx context.Context) ([]core.UsageSnapshot, error) {
	if c == nil || c.socketPath == "" {
		return nil, errors.New("export: telemetry daemon socket path is not configured")
	}

	client := c.newClient(c.socketPath)

	readCtx, readCancel := context.WithTimeout(ctx, daemonReadModelTimeout)
	defer readCancel()
	resp, err := client.FetchReadModel(readCtx, daemon.ReadModelRequest{})
	if err != nil {
		if isDialError(err) {
			return nil, fmt.Errorf(
				"export: %w at %s: %v (start it with 'openusage telemetry daemon install' or rerun with --source direct)",
				errDaemonUnreachable, c.socketPath, err,
			)
		}
		return nil, fmt.Errorf("export: reading snapshots from daemon: %w", err)
	}
	if resp.Pending && !daemon.SnapshotsHaveUsableData(resp.Snapshots) {
		return nil, fmt.Errorf("export: %w; retry shortly", errDaemonPending)
	}

	out := make([]core.UsageSnapshot, 0, len(resp.Snapshots))
	for _, snap := range resp.Snapshots {
		out = append(out, snap)
	}
	sortSnapshots(out)
	return out, nil
}

// isDialError reports whether err came from failing to connect to the socket.
func isDialError(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}
