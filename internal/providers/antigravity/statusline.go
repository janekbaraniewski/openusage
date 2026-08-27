package antigravity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type statusLinePayload struct {
	CWD            string                     `json:"cwd"`
	SessionID      string                     `json:"session_id"`
	ConversationID string                     `json:"conversation_id"`
	TranscriptPath string                     `json:"transcript_path"`
	Model          statusLineModel            `json:"model"`
	Workspace      statusLineWorkspace        `json:"workspace"`
	Version        string                     `json:"version"`
	Product        string                     `json:"product"`
	ContextWindow  statusLineContextWindow    `json:"context_window"`
	Quota          map[string]statusLineQuota `json:"quota"`
	AgentState     string                     `json:"agent_state"`
	PlanTier       string                     `json:"plan_tier"`
	Email          string                     `json:"email"`
	ReceivedAt     time.Time                  `json:"received_at"`
}

type statusLineModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type statusLineWorkspace struct {
	CurrentDir string `json:"current_dir"`
	ProjectDir string `json:"project_dir"`
}

type statusLineContextWindow struct {
	TotalInputTokens    int64                  `json:"total_input_tokens"`
	TotalOutputTokens   int64                  `json:"total_output_tokens"`
	ContextWindowSize   *int64                 `json:"context_window_size"`
	UsedPercentage      *float64               `json:"used_percentage"`
	RemainingPercentage *float64               `json:"remaining_percentage"`
	CurrentUsage        statusLineCurrentUsage `json:"current_usage"`
}

type statusLineCurrentUsage struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheReadTokens     int64 `json:"cache_read_input_tokens"`
	CacheWriteTokens    int64 `json:"cache_creation_input_tokens"`
	AlternateCacheWrite int64 `json:"cache_write_tokens"`
}

func (u statusLineCurrentUsage) CacheWriteTokensValue() int64 {
	if u.CacheWriteTokens > 0 {
		return u.CacheWriteTokens
	}
	return u.AlternateCacheWrite
}

func (u statusLineCurrentUsage) TotalTokens() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokensValue()
}

type statusLineQuota struct {
	RemainingFraction *float64 `json:"remaining_fraction"`
	ResetTime         string   `json:"reset_time"`
	ResetInSeconds    *int64   `json:"reset_in_seconds"`
}

// UnmarshalJSON accepts the documented object form and a numeric fraction so
// a small upstream schema variation does not make the whole status line
// unusable.
func (q *statusLineQuota) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) || len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '{' {
		type quotaAlias statusLineQuota
		var decoded quotaAlias
		if err := json.Unmarshal(trimmed, &decoded); err != nil {
			return err
		}
		*q = statusLineQuota(decoded)
		return nil
	}
	var fraction float64
	if err := json.Unmarshal(trimmed, &fraction); err != nil {
		return err
	}
	q.RemainingFraction = &fraction
	return nil
}

func parseStatusLinePayload(data []byte) (statusLinePayload, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return statusLinePayload{}, fmt.Errorf("empty status-line payload")
	}
	var payload statusLinePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return statusLinePayload{}, fmt.Errorf("parse status-line JSON: %w", err)
	}
	if payload.Quota == nil {
		payload.Quota = make(map[string]statusLineQuota)
	}
	return payload, nil
}

// CaptureStatusLine validates and stores an Antigravity status-line payload,
// returning the one-line rendering that Antigravity should display.
func CaptureStatusLine(data []byte, path string) (string, error) {
	payload, err := parseStatusLinePayload(data)
	if err != nil {
		return "AGY", err
	}
	if strings.TrimSpace(path) == "" {
		path = DefaultStatusFilePath()
	}
	if strings.TrimSpace(path) == "" {
		return "AGY", fmt.Errorf("status-line state path is unavailable")
	}

	state, err := addReceivedAt(data, time.Now().UTC())
	if err != nil {
		return "AGY", err
	}
	if err := writeStatusFile(path, state); err != nil {
		return "AGY", err
	}

	// Safely auto-route to specific account status files when email or identifier is present
	dir := filepath.Dir(path)
	emailLower := strings.ToLower(payload.Email)
	if strings.Contains(emailLower, "mohammed") {
		_ = writeStatusFile(filepath.Join(dir, "antigravity-mohammed-status.json"), state)
	} else if strings.Contains(emailLower, "nurul") {
		_ = writeStatusFile(filepath.Join(dir, "antigravity-nurulz-status.json"), state)
	}

	return renderStatusLine(payload), nil
}

// RenderStatusLine returns a safe fallback line for a raw payload. It is kept
// separate from CaptureStatusLine so command failures never print JSON or a
// multi-line error into Antigravity's UI.
func RenderStatusLine(data []byte) string {
	payload, err := parseStatusLinePayload(data)
	if err != nil {
		return "AGY"
	}
	return renderStatusLine(payload)
}

func addReceivedAt(data []byte, receivedAt time.Time) ([]byte, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, fmt.Errorf("prepare status-line state: %w", err)
	}
	timestamp, err := json.Marshal(receivedAt)
	if err != nil {
		return nil, fmt.Errorf("encode status-line timestamp: %w", err)
	}
	object["received_at"] = timestamp
	state, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("encode status-line state: %w", err)
	}
	return state, nil
}

func writeStatusFile(path string, data []byte) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty status-line state path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create status-line state directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".antigravity-status-*.tmp")
	if err != nil {
		return fmt.Errorf("create status-line state file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set status-line state permissions: %w", err)
	}
	if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write status-line state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close status-line state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace status-line state: %w", err)
	}
	return nil
}

func renderStatusLine(payload statusLinePayload) string {
	model := strings.TrimSpace(payload.Model.DisplayName)
	if model == "" {
		model = strings.TrimSpace(payload.Model.ID)
	}
	parts := []string{"AGY"}
	if model != "" {
		parts = append(parts, model)
	}
	if remaining, ok := worstQuotaFraction(payload); ok {
		parts = append(parts, "quota "+formatPercent(remaining*100))
	}
	if _, remaining, ok := contextPercentages(payload.ContextWindow); ok {
		parts = append(parts, "context "+formatPercent(100-remaining))
	}
	return strings.Join(parts, " · ")
}

func formatPercent(value float64) string {
	if math.Abs(value-math.Round(value)) < 0.05 {
		return strconv.Itoa(int(math.Round(value))) + "%"
	}
	return strconv.FormatFloat(value, 'f', 1, 64) + "%"
}

func sanitizeMetricName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}
