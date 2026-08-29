package webserve

import (
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
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
	UsageMode              string               `json:"usage_mode"`
	Catalog                []CatalogEntry       `json:"catalog"`
	ThemeTokens            ThemeTokens          `json:"theme_tokens"`
	Views                  []AccountView        `json:"views"`
	Snapshots              []core.UsageSnapshot `json:"snapshots"`
}

// ThemeTokens carries palette values for the browser dashboard.
type ThemeTokens struct {
	Name     string `json:"name"`
	Icon     string `json:"icon,omitempty"`
	Base     string `json:"base"`
	Mantle   string `json:"mantle"`
	Surface0 string `json:"surface0"`
	Surface1 string `json:"surface1"`
	Surface2 string `json:"surface2"`
	Text     string `json:"text"`
	Subtext  string `json:"subtext"`
	Dim      string `json:"dim"`
	Accent   string `json:"accent"`
	Blue     string `json:"blue"`
	Sapphire string `json:"sapphire"`
	Green    string `json:"green"`
	Yellow   string `json:"yellow"`
	Red      string `json:"red"`
	Peach    string `json:"peach"`
	Teal     string `json:"teal"`
	Lavender string `json:"lavender"`
	Mauve    string `json:"mauve"`
}

// AccountView is a TUI-projected account payload for the web dashboard.
type AccountView struct {
	Key            string          `json:"key"`
	ProviderID     string          `json:"provider_id"`
	ProviderName   string          `json:"provider_name"`
	AccountID      string          `json:"account_id"`
	Status         string          `json:"status"`
	StatusBadge    string          `json:"status_badge"`
	StatusIcon     string          `json:"status_icon"`
	AccentColor    string          `json:"accent_color"`
	Summary        string          `json:"summary"`
	Detail         string          `json:"detail,omitempty"`
	TagEmoji       string          `json:"tag_emoji,omitempty"`
	TagLabel       string          `json:"tag_label,omitempty"`
	GaugePercent   float64         `json:"gauge_percent,omitempty"`
	Message        string          `json:"message,omitempty"`
	Timestamp      time.Time       `json:"timestamp"`
	TileLines      []string        `json:"tile_lines"`
	DetailSections []DetailSection `json:"detail_sections"`
	Resets         []ResetPill     `json:"resets,omitempty"`
	DailyCost      []core.TimePoint `json:"daily_cost,omitempty"`
}

type DetailSection struct {
	Title string   `json:"title"`
	Icon  string   `json:"icon,omitempty"`
	Lines []string `json:"lines"`
}

type ResetPill struct {
	Label    string `json:"label"`
	Duration string `json:"duration"`
	Urgent   bool   `json:"urgent,omitempty"`
}

// Options configures a Server.
type Options struct {
	ListenAddr     string
	AuthToken      string
	Source         string // auto | direct | daemon | demo
	TimeWindow     string
	Theme          string
	UsageMode      string
	WarnThreshold  float64
	CritThreshold  float64
	RefreshSeconds int
	Version        string
	Demo           bool
	AllowPublic    bool
	Config         *config.Config
	Collect        CollectFunc // optional override for tests
	Now            func() time.Time
}

// CollectFunc produces a snapshot envelope. Tests substitute a stub.
type CollectFunc func() (Envelope, error)
