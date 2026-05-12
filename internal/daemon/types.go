package daemon

import (
	"errors"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
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
	Export          config.ExportConfig
}

type ReadModelAccount struct {
	AccountID  string `json:"account_id"`
	ProviderID string `json:"provider_id"`
}

type ReadModelRequest struct {
	Accounts      []ReadModelAccount `json:"accounts"`
	ProviderLinks map[string]string  `json:"provider_links"`
	TimeWindow    core.TimeWindow    `json:"time_window,omitempty"`
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
	snapshots map[string]core.UsageSnapshot
	updatedAt time.Time
}

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

func (c *readModelCache) get(cacheKey string) (map[string]core.UsageSnapshot, time.Time, bool) {
	if cacheKey == "" {
		return nil, time.Time{}, false
	}
	c.mu.RLock()
	entry, ok := c.entries[cacheKey]
	if !ok || len(entry.snapshots) == 0 {
		c.mu.RUnlock()
		return nil, time.Time{}, false
	}
	// Return direct reference — snapshots are deep-cloned on set() and
	// treated as immutable once cached. Consumers must not mutate.
	c.mu.RUnlock()
	return entry.snapshots, entry.updatedAt, true
}

func (c *readModelCache) set(cacheKey string, snapshots map[string]core.UsageSnapshot) {
	if cacheKey == "" || len(snapshots) == 0 {
		return
	}
	now := time.Now().UTC()
	c.mu.Lock()
	c.entries[cacheKey] = cachedReadModelEntry{
		snapshots: core.DeepCloneSnapshots(snapshots),
		updatedAt: now,
	}
	// Evict stale entries to prevent unbounded growth.
	const maxEntries = 50
	const maxAge = 5 * time.Minute
	if len(c.entries) > maxEntries {
		// First pass: remove stale entries.
		for k, e := range c.entries {
			if now.Sub(e.updatedAt) > maxAge {
				delete(c.entries, k)
			}
		}
		// If still over limit, find and remove oldest in a single pass.
		for len(c.entries) > maxEntries {
			oldestKey := ""
			oldestTime := now.Add(time.Hour) // sentinel
			for k, e := range c.entries {
				if e.updatedAt.Before(oldestTime) {
					oldestKey = k
					oldestTime = e.updatedAt
				}
			}
			if oldestKey == "" {
				break
			}
			delete(c.entries, oldestKey)
		}
	}
	c.mu.Unlock()
}

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

// providerPollState tracks per-account state for change detection and adaptive backoff.
type providerPollState struct {
	lastFetchAt time.Time
	lastSnap    core.UsageSnapshot
	hasSnap     bool
}

type SnapshotFrame struct {
	Snapshots  map[string]core.UsageSnapshot
	TimeWindow core.TimeWindow
}

type SnapshotHandler func(SnapshotFrame)

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
