package webserve

import (
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const schemaVersion = "1"

// CatalogEntry is a display-name lookup for a registered provider.
type CatalogEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Envelope is the JSON payload served at GET /api/v1/snapshots.
type Envelope struct {
	SchemaVersion          string               `json:"schema_version"`
	GeneratedAt            time.Time            `json:"generated_at"`
	OpenUsageVersion       string               `json:"openusage_version"`
	Source                 string               `json:"source"`
	TimeWindow             string               `json:"time_window"`
	Theme                  string               `json:"theme"`
	RefreshIntervalSeconds int                  `json:"refresh_interval_seconds"`
	Catalog                []CatalogEntry       `json:"catalog"`
	Snapshots              []core.UsageSnapshot `json:"snapshots"`
}

// Options configures a Server.
type Options struct {
	ListenAddr     string
	AuthToken      string
	Source         string // auto | direct | daemon | demo
	TimeWindow     string
	Theme          string
	RefreshSeconds int
	Version        string
	Demo           bool
	AllowPublic    bool
	Collect        CollectFunc // optional override for tests
	Now            func() time.Time
}

// CollectFunc produces a snapshot envelope. Tests substitute a stub.
type CollectFunc func() (Envelope, error)
