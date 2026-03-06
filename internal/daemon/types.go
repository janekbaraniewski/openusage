package daemon

import (
	"errors"
	"sync"
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
	TimeWindow    string             `json:"time_window,omitempty"`
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
	ProviderRegistry   string `json:"provider_registry_hash,omitempty"`
}

type cachedReadModelEntry struct {
	snapshots  map[string]core.UsageSnapshot
	updatedAt  time.Time
	timeWindow string
}

// readModelCache encapsulates the read-model caching layer with
// thread-safe access and in-flight deduplication.
type readModelCache struct {
	mu       sync.RWMutex
	entries  map[string]cachedReadModelEntry
	inFlight map[string]bool
}

func newReadModelCache() *readModelCache {
	return &readModelCache{
		entries:  make(map[string]cachedReadModelEntry),
		inFlight: make(map[string]bool),
	}
}

func (c *readModelCache) get(cacheKey, timeWindow string) (map[string]core.UsageSnapshot, time.Time, bool) {
	if cacheKey == "" {
		return nil, time.Time{}, false
	}
	c.mu.RLock()
	entry, ok := c.entries[cacheKey]
	if !ok || len(entry.snapshots) == 0 || entry.timeWindow != timeWindow {
		c.mu.RUnlock()
		return nil, time.Time{}, false
	}
	cloned := core.DeepCloneSnapshots(entry.snapshots)
	c.mu.RUnlock()
	return cloned, entry.updatedAt, true
}

func (c *readModelCache) set(cacheKey string, snapshots map[string]core.UsageSnapshot, timeWindow string) {
	if cacheKey == "" || len(snapshots) == 0 {
		return
	}
	c.mu.Lock()
	c.entries[cacheKey] = cachedReadModelEntry{
		snapshots:  core.DeepCloneSnapshots(snapshots),
		updatedAt:  time.Now().UTC(),
		timeWindow: timeWindow,
	}
	c.mu.Unlock()
}

// beginRefresh marks a cache key as in-flight. Returns false if already refreshing.
func (c *readModelCache) beginRefresh(cacheKey string) bool {
	if cacheKey == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inFlight[cacheKey] {
		return false
	}
	c.inFlight[cacheKey] = true
	return true
}

func (c *readModelCache) endRefresh(cacheKey string) {
	c.mu.Lock()
	delete(c.inFlight, cacheKey)
	c.mu.Unlock()
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
