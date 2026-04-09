package shared

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
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
	DefaultCollectOptions() TelemetryCollectOptions
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

	core.TokenUsage

	ToolName string
	Status   TelemetryStatus
	Payload  map[string]any
}

var timestampLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.000Z",
	"2006-01-02T15:04:05Z",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func ParseTimestampString(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range timestampLayouts {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts.UTC(), nil
		}
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return UnixAuto(n), nil
	}
	return time.Time{}, strconv.ErrSyntax
}

func FlexParseTime(value string) time.Time {
	t, err := ParseTimestampString(strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return t
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

// CollectFilesWithStat is like CollectFilesByExt but returns os.FileInfo
// for each file, enabling mtime+size cache invalidation.
func CollectFilesWithStat(roots []string, exts map[string]bool) map[string]os.FileInfo {
	result := make(map[string]os.FileInfo)
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
				result[root] = info
			}
			continue
		}
		_ = filepath.Walk(root, func(path string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil || fi == nil || fi.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if exts[ext] {
				result[path] = fi
			}
			return nil
		})
	}
	return result
}

func uniqueStrings(in []string) []string {
	return core.SortedCompactStrings(in)
}

// ExtractFilePathsFromPayload walks a JSON-like structure and extracts file path
// candidates from values stored under path-related keys. This is used by telemetry
// adapters to extract tool target file paths for language inference.
func ExtractFilePathsFromPayload(input any) []string {
	pathKeyHints := map[string]bool{
		"path": true, "paths": true, "file": true, "files": true, "filepath": true, "file_path": true,
		"cwd": true, "directory": true, "dir": true, "glob": true, "pattern": true, "target": true,
		"from": true, "to": true, "include": true, "exclude": true,
	}

	candidates := make(map[string]bool)
	var walk func(value any, hinted bool)
	walk = func(value any, hinted bool) {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				k := strings.ToLower(strings.TrimSpace(key))
				childHinted := hinted || pathKeyHints[k] || strings.Contains(k, "path") || strings.Contains(k, "file")
				walk(child, childHinted)
			}
		case []any:
			for _, child := range v {
				walk(child, hinted)
			}
		case string:
			if !hinted {
				return
			}
			for _, token := range extractPathTokens(v) {
				candidates[token] = true
			}
		}
	}
	walk(input, false)

	return core.SortedStringKeys(candidates)
}

func extractPathTokens(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		fields = []string{raw}
	}
	var out []string
	for _, field := range fields {
		token := strings.Trim(field, "\"'`()[]{}<>,:;")
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "file://") {
			continue
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		if !strings.Contains(token, "/") && !strings.Contains(token, "\\") && !strings.Contains(token, ".") {
			continue
		}
		token = strings.TrimPrefix(token, "./")
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		out = append(out, token)
	}
	return lo.Uniq(out)
}
