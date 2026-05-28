package openclaw

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const scannerBufBytes = 1 << 20

// openClawIndexEntry mirrors the entries in sessions.json.
type openClawIndexEntry struct {
	SessionID   string `json:"sessionId"`
	SessionFile string `json:"sessionFile"`
}

// openClawTranscriptLine is the union of fields we extract from each JSONL
// record. Upstream uses camelCase.
type openClawTranscriptLine struct {
	Type       string             `json:"type"`
	CustomType string             `json:"customType,omitempty"`
	Data       *openClawModelDecl `json:"data,omitempty"`
	Message    *openClawMessage   `json:"message,omitempty"`
	ModelID    string             `json:"modelId,omitempty"`
	Provider   string             `json:"provider,omitempty"`
}

type openClawModelDecl struct {
	Provider string `json:"provider,omitempty"`
	ModelID  string `json:"modelId,omitempty"`
}

type openClawMessage struct {
	Role      string          `json:"role,omitempty"`
	Timestamp json.RawMessage `json:"timestamp,omitempty"`
	Provider  string          `json:"provider,omitempty"`
	Model     string          `json:"model,omitempty"`
	ModelID   string          `json:"modelId,omitempty"`
	Usage     *openClawUsage  `json:"usage,omitempty"`
	Cost      *openClawCost   `json:"cost,omitempty"`
}

type openClawUsage struct {
	Input       int64         `json:"input,omitempty"`
	Output      int64         `json:"output,omitempty"`
	CacheRead   int64         `json:"cacheRead,omitempty"`
	CacheWrite  int64         `json:"cacheWrite,omitempty"`
	TotalTokens int64         `json:"totalTokens,omitempty"`
	Cost        *openClawCost `json:"cost,omitempty"`
}

type openClawCost struct {
	Total float64 `json:"total,omitempty"`
}

// openClawEntry is the flattened representation we emit downstream.
type openClawEntry struct {
	SessionID  string
	Provider   string
	Model      string
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	CostUSD    float64
	HasCost    bool
	Timestamp  time.Time
}

// readOpenClawIndex parses sessions.json and returns absolute paths to every
// transcript file it lists, paired with the session id from the entry.
// Missing referenced files are dropped silently.
func readOpenClawIndex(path string) ([]openClawIndexEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("openclaw: reading %s: %w", path, err)
	}
	var raw map[string]openClawIndexEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil
	}

	parent := filepath.Dir(path)
	out := make([]openClawIndexEntry, 0, len(raw))
	for _, entry := range raw {
		if entry.SessionFile == "" {
			continue
		}
		abs := entry.SessionFile
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(parent, abs)
		}
		if !fileExists(abs) {
			continue
		}
		out = append(out, openClawIndexEntry{
			SessionID:   entry.SessionID,
			SessionFile: abs,
		})
	}
	return out, nil
}

// readOpenClawTranscript decodes a single JSONL transcript. sessionID may be
// empty, in which case we derive it from the file basename.
func readOpenClawTranscript(path, sessionID string) ([]openClawEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("openclaw: opening %s: %w", path, err)
	}
	defer f.Close()

	if sessionID == "" {
		sessionID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), scannerBufBytes)

	var (
		out             []openClawEntry
		currentProvider string
		currentModel    string
	)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec openClawTranscriptLine
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}

		// Custom model declaration: remember for subsequent messages.
		if rec.CustomType == "model" || rec.Type == "model" {
			if rec.Data != nil {
				if rec.Data.Provider != "" {
					currentProvider = rec.Data.Provider
				}
				if rec.Data.ModelID != "" {
					currentModel = rec.Data.ModelID
				}
			}
			continue
		}

		if rec.Type != "message" || rec.Message == nil {
			continue
		}
		msg := rec.Message
		if !strings.EqualFold(msg.Role, "assistant") {
			continue
		}
		if msg.Usage == nil {
			continue
		}

		provider := firstNonEmpty(msg.Provider, rec.Provider, currentProvider)
		model := firstNonEmpty(msg.Model, msg.ModelID, rec.ModelID, currentModel)

		entry := openClawEntry{
			SessionID:  sessionID,
			Provider:   provider,
			Model:      model,
			Input:      nonNeg(msg.Usage.Input),
			Output:     nonNeg(msg.Usage.Output),
			CacheRead:  nonNeg(msg.Usage.CacheRead),
			CacheWrite: nonNeg(msg.Usage.CacheWrite),
			Timestamp:  parseOpenClawTimestamp(msg.Timestamp),
		}
		if entry.Input == 0 && entry.Output == 0 && entry.CacheRead == 0 && entry.CacheWrite == 0 {
			continue
		}

		switch {
		case msg.Usage.Cost != nil && msg.Usage.Cost.Total > 0:
			entry.CostUSD = msg.Usage.Cost.Total
			entry.HasCost = true
		case msg.Cost != nil && msg.Cost.Total > 0:
			entry.CostUSD = msg.Cost.Total
			entry.HasCost = true
		}
		out = append(out, entry)
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("openclaw: scanning %s: %w", path, err)
	}
	return out, nil
}

func parseOpenClawTimestamp(raw json.RawMessage) time.Time {
	if len(raw) == 0 {
		return time.Time{}
	}
	var asInt int64
	if err := json.Unmarshal(raw, &asInt); err == nil && asInt > 0 {
		return time.UnixMilli(asInt).UTC()
	}
	var asStr string
	if err := json.Unmarshal(raw, &asStr); err == nil && asStr != "" {
		if t, err := time.Parse(time.RFC3339Nano, asStr); err == nil {
			return t.UTC()
		}
		if t, err := time.Parse(time.RFC3339, asStr); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func nonNeg(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
