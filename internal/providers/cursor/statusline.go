package cursor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type statusLinePayload struct {
	SessionID        string                     `json:"session_id"`
	SessionName      string                     `json:"session_name,omitempty"`
	TranscriptPath   string                     `json:"transcript_path,omitempty"`
	RenderWidthChars int                        `json:"render_width_chars,omitempty"`
	CWD              string                     `json:"cwd"`
	Autorun          bool                       `json:"autorun,omitempty"`
	Model            statusLineModel            `json:"model"`
	Workspace        statusLineWorkspace        `json:"workspace"`
	Version          string                     `json:"version"`
	OutputStyle      statusLineOutputStyle      `json:"output_style,omitempty"`
	ContextWindow    statusLineContextWindow    `json:"context_window"`
	Vim              *statusLineVim             `json:"vim,omitempty"`
	Worktree         *statusLineWorktree        `json:"worktree,omitempty"`
	Quota            map[string]statusLineQuota `json:"quota,omitempty"`
	AgentState       string                     `json:"agent_state,omitempty"`
	PlanTier         string                     `json:"plan_tier,omitempty"`
	Email            string                     `json:"email,omitempty"`
	AuthInfo         *statusLineAuthInfo        `json:"auth_info,omitempty"`
	ReceivedAt       time.Time                  `json:"received_at"`
}

type statusLineModel struct {
	ID           string `json:"id"`
	DisplayName  string `json:"display_name"`
	ParamSummary string `json:"param_summary,omitempty"`
	MaxMode      bool   `json:"max_mode,omitempty"`
	Effort       string `json:"effort,omitempty"`
	Fast         bool   `json:"fast,omitempty"`
}

type statusLineWorkspace struct {
	CurrentDir string   `json:"current_dir"`
	ProjectDir string   `json:"project_dir"`
	AddedDirs  []string `json:"added_dirs,omitempty"`
}

type statusLineContextWindow struct {
	TotalInputTokens    int64                   `json:"total_input_tokens"`
	TotalOutputTokens   *int64                  `json:"total_output_tokens,omitempty"`
	ContextWindowSize   *int64                  `json:"context_window_size,omitempty"`
	UsedPercentage      *float64                `json:"used_percentage,omitempty"`
	RemainingPercentage *float64                `json:"remaining_percentage,omitempty"`
	CurrentUsage        *statusLineCurrentUsage `json:"current_usage,omitempty"`
}

type statusLineCurrentUsage struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheReadTokens     int64 `json:"cache_read_input_tokens"`
	CacheWriteTokens    int64 `json:"cache_creation_input_tokens"`
	AlternateCacheWrite int64 `json:"cache_write_tokens"`
}

func (u *statusLineCurrentUsage) CacheWriteTokensValue() int64 {
	if u == nil {
		return 0
	}
	if u.CacheWriteTokens > 0 {
		return u.CacheWriteTokens
	}
	return u.AlternateCacheWrite
}

func (u *statusLineCurrentUsage) TotalTokens() int64 {
	if u == nil {
		return 0
	}
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokensValue()
}

type statusLineQuota struct {
	RemainingFraction *float64 `json:"remaining_fraction"`
	ResetTime         string   `json:"reset_time,omitempty"`
	ResetInSeconds    *int64   `json:"reset_in_seconds,omitempty"`
	Disabled          bool     `json:"disabled,omitempty"`
}

// UnmarshalJSON accepts the documented object form and numeric fraction forms.
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

type statusLineVim struct {
	Mode string `json:"mode"`
}

type statusLineWorktree struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type statusLineAuthInfo struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	UserID      any    `json:"userId"`
	AuthID      string `json:"authId"`
}

type statusLineOutputStyle struct {
	Name string `json:"name"`
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

// CaptureStatusLine validates and stores a Cursor status-line payload,
// returning the one-line rendering that Cursor should display.
func CaptureStatusLine(data []byte, path string) (string, error) {
	payload, err := parseStatusLinePayload(data)
	if err != nil {
		return "Cursor", err
	}
	if strings.TrimSpace(path) == "" {
		path = DefaultStatusFilePath()
	}
	if strings.TrimSpace(path) == "" {
		return "Cursor", fmt.Errorf("status-line state path is unavailable")
	}

	state, err := addReceivedAt(data, time.Now().UTC())
	if err != nil {
		return "Cursor", err
	}
	if err := writeStatusFile(path, state); err != nil {
		return "Cursor", err
	}

	dir := filepath.Dir(path)
	if box := findContainerForPayload(payload); box != "" {
		_ = writeStatusFile(filepath.Join(dir, fmt.Sprintf("cursor-%s-status.json", box)), state)
	}

	email := payload.Email
	if email == "" && payload.AuthInfo != nil {
		email = payload.AuthInfo.Email
	}
	if slug := extractAccountSlug(email); slug != "" {
		_ = writeStatusFile(filepath.Join(dir, fmt.Sprintf("cursor-%s-status.json", slug)), state)
		if strings.Contains(slug, "physics") && slug != "physics" {
			_ = writeStatusFile(filepath.Join(dir, "cursor-physics-status.json"), state)
		}
		if strings.Contains(slug, "nurul") && slug != "nurulz" {
			_ = writeStatusFile(filepath.Join(dir, "cursor-nurulz-status.json"), state)
		}
	}

	return renderStatusLine(payload), nil
}

func findContainerForPayload(payload statusLinePayload) string {
	if boxEnv := strings.TrimSpace(strings.ToLower(os.Getenv("AGENT_CONTAINER"))); boxEnv != "" {
		return boxEnv
	}
	if curAcct := strings.TrimSpace(strings.ToLower(os.Getenv("CURSOR_ACCOUNT"))); curAcct != "" {
		return curAcct
	}
	if agAcct := strings.TrimSpace(strings.ToLower(os.Getenv("AGENT_ACCOUNT"))); agAcct != "" {
		return agAcct
	}
	for _, checkPath := range []string{payload.CWD, payload.Workspace.CurrentDir, payload.TranscriptPath} {
		if box := extractContainerBox(checkPath); box != "" {
			return box
		}
	}
	// Check containers by matching authInfo.email
	email := strings.ToLower(strings.TrimSpace(payload.Email))
	if email == "" && payload.AuthInfo != nil {
		email = strings.ToLower(strings.TrimSpace(payload.AuthInfo.Email))
	}
	home, _ := os.UserHomeDir()
	if home != "" && email != "" {
		for _, dirName := range []string{".agent-containers", ".cursor-containers"} {
			cDir := filepath.Join(home, dirName)
			entries, err := os.ReadDir(cDir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				boxCfgPath := filepath.Join(cDir, entry.Name(), ".cursor", "cli-config.json")
				if data, err := os.ReadFile(boxCfgPath); err == nil {
					var cfg struct {
						AuthInfo struct {
							Email string `json:"email"`
						} `json:"authInfo"`
					}
					if json.Unmarshal(data, &cfg) == nil && strings.EqualFold(cfg.AuthInfo.Email, email) {
						return strings.ToLower(entry.Name())
					}
				}
			}
		}
	}
	return ""
}

func extractContainerBox(path string) string {
	path = filepath.ToSlash(path)
	patterns := []string{"/.agent-containers/", "/.cursor-containers/", "/.agy-containers/"}
	for _, pattern := range patterns {
		idx := strings.Index(path, pattern)
		if idx != -1 {
			sub := path[idx+len(pattern):]
			parts := strings.Split(sub, "/")
			if len(parts) > 0 && parts[0] != "" {
				return strings.ToLower(parts[0])
			}
		}
	}
	return ""
}

func extractAccountSlug(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return ""
	}
	parts := strings.Split(email, "@")
	username := parts[0]
	var b strings.Builder
	for _, r := range username {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// RenderStatusLine returns a safe fallback line for a raw payload.
func RenderStatusLine(data []byte) string {
	payload, err := parseStatusLinePayload(data)
	if err != nil {
		return "Cursor"
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

func writeStatusFile(path string, state []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create status-line dir: %w", err)
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, state, 0o600); err != nil {
		return fmt.Errorf("write temporary status-line file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit status-line file: %w", err)
	}
	return nil
}

func renderStatusLine(payload statusLinePayload) string {
	model := strings.TrimSpace(payload.Model.DisplayName)
	if model == "" {
		model = strings.TrimSpace(payload.Model.ID)
	}
	if model == "" {
		model = "Cursor"
	}

	if param := strings.TrimSpace(payload.Model.ParamSummary); param != "" {
		if !strings.HasPrefix(param, "(") {
			param = "(" + param + ")"
		}
		model += " " + param
	}

	var parts []string
	parts = append(parts, "Cursor", model)

	if worst, ok := worstQuotaFraction(payload); ok {
		parts = append(parts, fmt.Sprintf("quota %.0f%%", math.Round(worst*100)))
	}

	used, _, hasPercent := contextPercentages(payload.ContextWindow)
	if hasPercent {
		parts = append(parts, fmt.Sprintf("context %.0f%%", math.Round(used)))
	} else if payload.ContextWindow.TotalInputTokens > 0 {
		parts = append(parts, fmt.Sprintf("in %s", formatTokens(payload.ContextWindow.TotalInputTokens)))
	}

	return strings.Join(parts, " · ")
}

func formatTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return strconv.FormatInt(n, 10)
}
