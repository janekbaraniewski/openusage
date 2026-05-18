package amp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ampThread is the top-level structure of a thread JSON file at
// <data_dir>/threads/<thread_id>.json. Fields are tolerant of partial /
// upstream-renamed payloads — unknown fields are ignored.
type ampThread struct {
	ID         string       `json:"id,omitempty"`
	CreatedAt  string       `json:"created_at,omitempty"`
	CreatedRFC string       `json:"createdAt,omitempty"`
	Title      string       `json:"title,omitempty"`
	LedgerPath string       `json:"ledger_path,omitempty"`
	LedgerCC   string       `json:"ledgerPath,omitempty"`
	Messages   []ampMessage `json:"messages,omitempty"`
}

// effectiveCreatedAt returns whichever timestamp field the thread payload
// chose to populate. Threads in the wild use both snake_case and camelCase.
func (t ampThread) effectiveCreatedAt() string {
	if v := strings.TrimSpace(t.CreatedAt); v != "" {
		return v
	}
	return strings.TrimSpace(t.CreatedRFC)
}

// effectiveLedgerPath returns whichever ledger-path field was populated.
func (t ampThread) effectiveLedgerPath() string {
	if v := strings.TrimSpace(t.LedgerPath); v != "" {
		return v
	}
	return strings.TrimSpace(t.LedgerCC)
}

// ampMessage is one entry in the thread's message log. We only care about
// assistant-role messages that carry token-usage data, but we tolerate any
// role/shape on read.
type ampMessage struct {
	ID          string     `json:"id,omitempty"`
	MessageID   string     `json:"message_id,omitempty"`
	MessageIDCC string     `json:"messageId,omitempty"`
	Role        string     `json:"role,omitempty"`
	Model       string     `json:"model,omitempty"`
	Timestamp   string     `json:"timestamp,omitempty"`
	CreatedAt   string     `json:"created_at,omitempty"`
	CreatedAtCC string     `json:"createdAt,omitempty"`
	Usage       *ampTokens `json:"usage,omitempty"`
}

// effectiveID picks whichever id-shaped field the upstream payload chose.
// Used as the dedup key against the ledger.
func (m ampMessage) effectiveID() string {
	for _, candidate := range []string{m.ID, m.MessageID, m.MessageIDCC} {
		if v := strings.TrimSpace(candidate); v != "" {
			return v
		}
	}
	return ""
}

// effectiveTimestamp returns the first non-empty timestamp candidate.
func (m ampMessage) effectiveTimestamp() string {
	for _, candidate := range []string{m.Timestamp, m.CreatedAt, m.CreatedAtCC} {
		if v := strings.TrimSpace(candidate); v != "" {
			return v
		}
	}
	return ""
}

// ampTokens describes the per-message token counts. Token values may be
// negative in intermediate records; we clamp to 0 via clampNonNegative.
// Field aliases cover both snake_case and camelCase variants observed in
// captured payloads.
type ampTokens struct {
	Input         int64 `json:"input,omitempty"`
	InputAlt      int64 `json:"input_tokens,omitempty"`
	InputCC       int64 `json:"inputTokens,omitempty"`
	Output        int64 `json:"output,omitempty"`
	OutputAlt     int64 `json:"output_tokens,omitempty"`
	OutputCC      int64 `json:"outputTokens,omitempty"`
	CacheRead     int64 `json:"cache_read,omitempty"`
	CacheReadCC   int64 `json:"cacheReadInputTokens,omitempty"`
	CacheReadAlt  int64 `json:"cache_read_input_tokens,omitempty"`
	CacheWrite    int64 `json:"cache_write,omitempty"`
	CacheWriteCC  int64 `json:"cacheCreationInputTokens,omitempty"`
	CacheWriteAlt int64 `json:"cache_creation_input_tokens,omitempty"`
}

// normalised returns a token bundle with all aliases folded into the canonical
// fields and negative values clamped to zero.
func (t ampTokens) normalised() ampTokens {
	return ampTokens{
		Input:      clampNonNegative(firstNonZero(t.Input, t.InputAlt, t.InputCC)),
		Output:     clampNonNegative(firstNonZero(t.Output, t.OutputAlt, t.OutputCC)),
		CacheRead:  clampNonNegative(firstNonZero(t.CacheRead, t.CacheReadAlt, t.CacheReadCC)),
		CacheWrite: clampNonNegative(firstNonZero(t.CacheWrite, t.CacheWriteAlt, t.CacheWriteCC)),
	}
}

// total returns input + output + cache for use as a "rough total" sort tie-
// breaker. Callers that need to surface a single total tokens metric should
// derive it explicitly.
func (t ampTokens) total() int64 {
	return t.Input + t.Output + t.CacheRead + t.CacheWrite
}

// firstNonZero returns the first non-zero value among the supplied candidates,
// or 0 when all are zero.
func firstNonZero(values ...int64) int64 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

// clampNonNegative replaces negative values with zero, per the spec note that
// intermediate records may carry negative token counts.
func clampNonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// ampEvent is the normalised, in-memory representation of a single
// thread-message billing event. Both the per-thread parser and the ledger
// reconciler emit ampEvent values, which are then deduplicated and merged.
type ampEvent struct {
	MessageID  string
	Model      string
	Timestamp  time.Time
	Tokens     ampTokens
	CreditCost float64 // 0 when the message has no ledger match; ledger sets this.
	Source     string  // "thread", "ledger", or "merged"
	ThreadID   string
	SourcePath string
}

// parseAmpThreadFile reads one thread JSON file and returns the assistant-
// role billing events extracted from it. Malformed thread files produce a
// wrapping error; individual missing fields are tolerated by leaning on
// the multi-name decoder shapes above.
func parseAmpThreadFile(path string) ([]ampEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("amp: reading thread file %s: %w", filepath.Base(path), err)
	}
	if len(data) == 0 {
		return nil, nil
	}

	var thread ampThread
	if err := json.Unmarshal(data, &thread); err != nil {
		return nil, fmt.Errorf("amp: parsing thread file %s: %w", filepath.Base(path), err)
	}

	threadID := strings.TrimSpace(thread.ID)
	if threadID == "" {
		threadID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	threadCreatedAt := thread.effectiveCreatedAt()

	// File mtime as last-resort timestamp.
	var fileMTime time.Time
	if info, err := os.Stat(path); err == nil {
		fileMTime = info.ModTime()
	}

	events := make([]ampEvent, 0, len(thread.Messages))
	for _, msg := range thread.Messages {
		if !strings.EqualFold(strings.TrimSpace(msg.Role), "assistant") {
			continue
		}
		if msg.Usage == nil {
			continue
		}
		tokens := msg.Usage.normalised()
		if tokens.total() == 0 {
			// Skip purely-empty usage rows; they contribute nothing to
			// any metric and just bloat the dedup map.
			continue
		}
		ts := resolveEventTimestamp(msg.effectiveTimestamp(), threadCreatedAt, fileMTime)
		events = append(events, ampEvent{
			MessageID:  msg.effectiveID(),
			Model:      strings.TrimSpace(msg.Model),
			Timestamp:  ts,
			Tokens:     tokens,
			Source:     "thread",
			ThreadID:   threadID,
			SourcePath: path,
		})
	}
	return events, nil
}

// resolveEventTimestamp implements the three-step fallback chain: explicit
// RFC3339-parsed timestamp on the message, then the thread's createdAt,
// then the file mtime.
func resolveEventTimestamp(messageTS, threadTS string, fileMTime time.Time) time.Time {
	if ts, ok := parseRFC3339Any(messageTS); ok {
		return ts
	}
	if ts, ok := parseRFC3339Any(threadTS); ok {
		return ts
	}
	return fileMTime
}

// parseRFC3339Any accepts both RFC3339 and RFC3339Nano. Empty / unparseable
// values return zero-value time and false.
func parseRFC3339Any(value string) (time.Time, bool) {
	v := strings.TrimSpace(value)
	if v == "" {
		return time.Time{}, false
	}
	if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return ts, true
	}
	if ts, err := time.Parse(time.RFC3339, v); err == nil {
		return ts, true
	}
	return time.Time{}, false
}
