package daemon

import (
	"errors"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const APIVersion = "v1"

var errDaemonUnavailable = errors.New("telemetry daemon unavailable")

type Config struct {
	DBPath          string
	SpoolDir        string
	SocketPath      string
	CollectInterval time.Duration
	PollInterval    time.Duration
	Verbose         bool
}

type ReadModelAccount struct {
	AccountID  string `json:"account_id"`
	ProviderID string `json:"provider_id"`
}

type ReadModelRequest struct {
	Accounts      []ReadModelAccount `json:"accounts"`
	ProviderLinks map[string]string  `json:"provider_links"`
}

type ReadModelResponse struct {
	Snapshots map[string]core.UsageSnapshot `json:"snapshots"`
}

type HookResponse struct {
	Source    string   `json:"source"`
	Enqueued  int      `json:"enqueued"`
	Processed int      `json:"processed"`
	Ingested  int      `json:"ingested"`
	Deduped   int      `json:"deduped"`
	Failed    int      `json:"failed"`
	Warnings  []string `json:"warnings,omitempty"`
}

type HealthResponse struct {
	Status             string `json:"status"`
	DaemonVersion      string `json:"daemon_version,omitempty"`
	APIVersion         string `json:"api_version,omitempty"`
	IntegrationVersion string `json:"integration_version,omitempty"`
}

type cachedReadModelEntry struct {
	snapshots map[string]core.UsageSnapshot
	updatedAt time.Time
}

type ingestTally struct {
	processed int
	ingested  int
	deduped   int
	failed    int
}

type SnapshotHandler func(map[string]core.UsageSnapshot)

type DaemonStatus int

const (
	DaemonStatusUnknown      DaemonStatus = iota
	DaemonStatusConnecting                // attempting to reach daemon
	DaemonStatusNotInstalled              // service not installed
	DaemonStatusStarting                  // service installed, waiting for health
	DaemonStatusRunning                   // healthy and current
	DaemonStatusOutdated                  // healthy but wrong version
	DaemonStatusError                     // unrecoverable error
)

type DaemonState struct {
	Status      DaemonStatus
	Message     string
	InstallHint string
}

type StateHandler func(DaemonState)
