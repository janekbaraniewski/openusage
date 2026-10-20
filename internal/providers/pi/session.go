package pi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxLineBytes = 1 << 20

type piSessionHeader struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp,omitempty"`
	CWD       string `json:"cwd,omitempty"`
}

type piMessageLine struct {
	Type      string         `json:"type"`
	ID        string         `json:"id,omitempty"`
	Timestamp string         `json:"timestamp,omitempty"`
	Message   *piMessageBody `json:"message,omitempty"`
}

type piMessageBody struct {
	Role     string   `json:"role,omitempty"`
	Model    string   `json:"model,omitempty"`
	Provider string   `json:"provider,omitempty"`
	Usage    *piUsage `json:"usage,omitempty"`
}

type piUsage struct {
	Input      *int64  `json:"input,omitempty"`
	Output     *int64  `json:"output,omitempty"`
	CacheRead  *int64  `json:"cacheRead,omitempty"`
	CacheWrite *int64  `json:"cacheWrite,omitempty"`
	Cost       *piCost `json:"cost,omitempty"`
}

// piCost is the USD breakdown Pi computes from its model price table.
type piCost struct {
	Total float64 `json:"total,omitempty"`
}

type piSessionMeta struct {
	SessionID      string
	CWD            string
	WorkspaceLabel string
	HeaderTime     time.Time
}

type piModelEntry struct {
	SessionID      string
	WorkspaceLabel string
	Provider       string
	Model          string
	Input          int64
	Output         int64
	CacheRead      int64
	CacheWrite     int64
	CostUSD        float64
	HasCost        bool
	Timestamp      time.Time
}

// readPiSessionFile parses one JSONL session file. The first line must be a
// session header; otherwise the file is skipped. Malformed message lines are
// dropped individually so partial corruption never poisons a whole session.
func readPiSessionFile(path string) ([]piModelEntry, piSessionMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, piSessionMeta{}, nil
		}
		return nil, piSessionMeta{}, fmt.Errorf("pi: opening %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)

	if !scanner.Scan() {
		return nil, piSessionMeta{}, nil
	}
	var header piSessionHeader
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil || header.Type != "session" {
		return nil, piSessionMeta{}, nil
	}

	meta := piSessionMeta{
		SessionID:      strings.TrimSpace(header.ID),
		CWD:            strings.TrimSpace(header.CWD),
		WorkspaceLabel: workspaceLabel(header.CWD),
	}
	if header.Timestamp != "" {
		if t, perr := time.Parse(time.RFC3339Nano, header.Timestamp); perr == nil {
			meta.HeaderTime = t.UTC()
		}
	}

	var fallback time.Time
	if info, statErr := os.Stat(path); statErr == nil {
		fallback = info.ModTime().UTC()
	}

	var out []piModelEntry
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var line piMessageLine
		if err := json.Unmarshal(raw, &line); err != nil {
			continue
		}
		if line.Type != "message" || line.Message == nil {
			continue
		}
		if line.Message.Role != "assistant" || line.Message.Usage == nil {
			continue
		}

		entry := piModelEntry{
			SessionID:      meta.SessionID,
			WorkspaceLabel: meta.WorkspaceLabel,
			Provider:       strings.TrimSpace(line.Message.Provider),
			Model:          strings.TrimSpace(line.Message.Model),
			Input:          nonNegative(line.Message.Usage.Input),
			Output:         nonNegative(line.Message.Usage.Output),
			CacheRead:      nonNegative(line.Message.Usage.CacheRead),
			CacheWrite:     nonNegative(line.Message.Usage.CacheWrite),
		}
		if entry.Input == 0 && entry.Output == 0 && entry.CacheRead == 0 && entry.CacheWrite == 0 {
			continue
		}
		if c := line.Message.Usage.Cost; c != nil && c.Total > 0 {
			entry.CostUSD = c.Total
			entry.HasCost = true
		}

		if line.Timestamp != "" {
			if t, perr := time.Parse(time.RFC3339Nano, line.Timestamp); perr == nil {
				entry.Timestamp = t.UTC()
			}
		}
		if entry.Timestamp.IsZero() {
			entry.Timestamp = fallback
		}

		out = append(out, entry)
	}

	if err := scanner.Err(); err != nil {
		return out, meta, nil
	}
	return out, meta, nil
}

func workspaceLabel(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	clean := filepath.ToSlash(cwd)
	clean = strings.TrimRight(clean, "/")
	if clean == "" {
		return ""
	}
	idx := strings.LastIndex(clean, "/")
	if idx < 0 {
		return clean
	}
	return clean[idx+1:]
}

func nonNegative(p *int64) int64 {
	if p == nil {
		return 0
	}
	if *p < 0 {
		return 0
	}
	return *p
}
