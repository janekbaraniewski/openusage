package qwen_cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type qwenLine struct {
	Type          string            `json:"type,omitempty"`
	Model         string            `json:"model,omitempty"`
	Timestamp     string            `json:"timestamp,omitempty"`
	SessionID     string            `json:"sessionId,omitempty"`
	UsageMetadata *qwenUsageMetadata `json:"usageMetadata,omitempty"`
}

type qwenUsageMetadata struct {
	PromptTokenCount        *int64 `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount    *int64 `json:"candidatesTokenCount,omitempty"`
	ThoughtsTokenCount      *int64 `json:"thoughtsTokenCount,omitempty"`
	CachedContentTokenCount *int64 `json:"cachedContentTokenCount,omitempty"`
}

type qwenModelEntry struct {
	SessionID string
	Provider  string
	Model     string
	Input     int64
	Output    int64
	Reasoning int64
	Cached    int64
	Timestamp time.Time
}

func readQwenChatFile(path string) ([]qwenModelEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("qwen_cli: opening %s: %w", path, err)
	}
	defer f.Close()

	info, statErr := f.Stat()
	var mtime time.Time
	if statErr == nil {
		mtime = info.ModTime().UTC()
	}

	derivedID := deriveSessionID(path)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var out []qwenModelEntry
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var line qwenLine
		if err := json.Unmarshal(raw, &line); err != nil {
			continue
		}
		if line.Type != "assistant" || line.UsageMetadata == nil {
			continue
		}

		in := nonNeg(line.UsageMetadata.PromptTokenCount)
		out_ := nonNeg(line.UsageMetadata.CandidatesTokenCount)
		reason := nonNeg(line.UsageMetadata.ThoughtsTokenCount)
		cached := nonNeg(line.UsageMetadata.CachedContentTokenCount)
		if in+out_+reason+cached == 0 {
			continue
		}

		model := strings.TrimSpace(line.Model)
		if model == "" {
			model = defaultModel
		}

		ts := parseTimestamp(line.Timestamp)
		if ts.IsZero() {
			ts = mtime
		}

		sid := strings.TrimSpace(line.SessionID)
		if sid == "" {
			sid = derivedID
		}

		out = append(out, qwenModelEntry{
			SessionID: sid,
			Provider:  defaultProvider,
			Model:     model,
			Input:     in,
			Output:    out_,
			Reasoning: reason,
			Cached:    cached,
			Timestamp: ts,
		})
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("qwen_cli: scanning %s: %w", path, err)
	}
	return out, nil
}

func deriveSessionID(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	// Walk up to the nearest non-"chats" directory; that's the project segment.
	parent := filepath.Dir(path)
	project := filepath.Base(parent)
	if project == chatsSubdir {
		project = filepath.Base(filepath.Dir(parent))
	}
	if project == "" || project == "." || project == string(filepath.Separator) {
		return stem
	}
	return project + "-" + stem
}

func parseTimestamp(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func nonNeg(p *int64) int64 {
	if p == nil {
		return 0
	}
	v := *p
	if v < 0 {
		return 0
	}
	return v
}
