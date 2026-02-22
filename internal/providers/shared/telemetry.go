package shared

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type TelemetryEventType string

const (
	TelemetryEventTypeTurnCompleted TelemetryEventType = "turn_completed"
	TelemetryEventTypeMessageUsage  TelemetryEventType = "message_usage"
	TelemetryEventTypeToolUsage     TelemetryEventType = "tool_usage"
	TelemetryEventTypeRawEnvelope   TelemetryEventType = "raw_envelope"
)

type TelemetryStatus string

const (
	TelemetryStatusOK      TelemetryStatus = "ok"
	TelemetryStatusError   TelemetryStatus = "error"
	TelemetryStatusAborted TelemetryStatus = "aborted"
	TelemetryStatusUnknown TelemetryStatus = "unknown"
)

type TelemetryChannel string

const (
	TelemetryChannelHook   TelemetryChannel = "hook"
	TelemetryChannelSSE    TelemetryChannel = "sse"
	TelemetryChannelJSONL  TelemetryChannel = "jsonl"
	TelemetryChannelAPI    TelemetryChannel = "api"
	TelemetryChannelSQLite TelemetryChannel = "sqlite"
)

var ErrHookUnsupported = errors.New("hook parsing not supported")

type TelemetryCollectOptions struct {
	Paths     map[string]string
	PathLists map[string][]string
}

func (o TelemetryCollectOptions) Path(key string, fallback string) string {
	if o.Paths == nil {
		return strings.TrimSpace(fallback)
	}
	if value := strings.TrimSpace(o.Paths[key]); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func (o TelemetryCollectOptions) PathsFor(key string, fallback []string) []string {
	if o.PathLists == nil {
		return fallback
	}
	values, ok := o.PathLists[key]
	if !ok || len(values) == 0 {
		return fallback
	}
	return values
}

type TelemetrySource interface {
	System() string
	Collect(ctx context.Context, opts TelemetryCollectOptions) ([]TelemetryEvent, error)
	ParseHookPayload(raw []byte, opts TelemetryCollectOptions) ([]TelemetryEvent, error)
}

type TelemetryEvent struct {
	SchemaVersion string
	Channel       TelemetryChannel
	OccurredAt    time.Time
	AccountID     string
	WorkspaceID   string
	SessionID     string
	TurnID        string
	MessageID     string
	ToolCallID    string
	ProviderID    string
	AgentName     string
	EventType     TelemetryEventType
	ModelRaw      string

	InputTokens      *int64
	OutputTokens     *int64
	ReasoningTokens  *int64
	CacheReadTokens  *int64
	CacheWriteTokens *int64
	TotalTokens      *int64
	CostUSD          *float64
	Requests         *int64

	ToolName string
	Status   TelemetryStatus
	Payload  map[string]any
}

func Int64Ptr(v int64) *int64 {
	vv := v
	return &vv
}

func Float64Ptr(v float64) *float64 {
	vv := v
	return &vv
}

func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func ParseTimestampString(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts.UTC(), nil
		}
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return UnixAuto(n), nil
	}
	return time.Time{}, strconv.ErrSyntax
}

func UnixAuto(ts int64) time.Time {
	switch {
	case ts > 1_000_000_000_000_000:
		return time.UnixMicro(ts).UTC()
	case ts > 1_000_000_000_000:
		return time.UnixMilli(ts).UTC()
	default:
		return time.Unix(ts, 0).UTC()
	}
}

func ParseFlexibleTimestamp(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if t, err := ParseTimestampString(value); err == nil {
		return t.Unix(), true
	}
	return 0, false
}

func SanitizeWorkspace(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	base := filepath.Base(cwd)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return cwd
	}
	return base
}

func ExpandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func CollectFilesByExt(roots []string, exts map[string]bool) []string {
	var files []string
	for _, root := range roots {
		root = ExpandHome(root)
		if root == "" {
			continue
		}
		info, err := os.Stat(root)
		if err != nil || info == nil {
			continue
		}
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(root))
			if exts[ext] {
				files = append(files, root)
			}
			continue
		}
		_ = filepath.Walk(root, func(path string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil || fi == nil || fi.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if exts[ext] {
				files = append(files, path)
			}
			return nil
		})
	}
	return uniqueStrings(files)
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
