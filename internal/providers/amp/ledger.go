package amp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ampLedgerRecord is one line of <data_dir>/ledger.jsonl. It records the
// authoritative credit cost for a single assistant response keyed by
// `toMessageId`.
type ampLedgerRecord struct {
	ToMessageID   string     `json:"to_message_id,omitempty"`
	ToMessageIDCC string     `json:"toMessageId,omitempty"`
	Model         string     `json:"model,omitempty"`
	Credits       float64    `json:"credits,omitempty"`
	Cost          float64    `json:"cost,omitempty"`
	Timestamp     string     `json:"timestamp,omitempty"`
	CreatedAt     string     `json:"created_at,omitempty"`
	CreatedAtCC   string     `json:"createdAt,omitempty"`
	Tokens        *ampTokens `json:"tokens,omitempty"`
	Usage         *ampTokens `json:"usage,omitempty"`
}

// effectiveToMessageID picks whichever id-shaped field this ledger row populated.
func (r ampLedgerRecord) effectiveToMessageID() string {
	if v := strings.TrimSpace(r.ToMessageID); v != "" {
		return v
	}
	return strings.TrimSpace(r.ToMessageIDCC)
}

// effectiveTimestamp prefers the explicit timestamp, then created_at /
// createdAt aliases.
func (r ampLedgerRecord) effectiveTimestamp() string {
	for _, candidate := range []string{r.Timestamp, r.CreatedAt, r.CreatedAtCC} {
		if v := strings.TrimSpace(candidate); v != "" {
			return v
		}
	}
	return ""
}

// effectiveTokens returns the first non-nil token bag from the ledger row.
func (r ampLedgerRecord) effectiveTokens() ampTokens {
	if r.Tokens != nil {
		return r.Tokens.normalised()
	}
	if r.Usage != nil {
		return r.Usage.normalised()
	}
	return ampTokens{}
}

// effectiveCost prefers `credits`, falling back to `cost` for callers that
// emit either keying.
func (r ampLedgerRecord) effectiveCost() float64 {
	if r.Credits != 0 {
		return r.Credits
	}
	return r.Cost
}

// loadLedgerRecords reads a ledger.jsonl file line-by-line and returns the
// records grouped by their `toMessageId`. Malformed lines are skipped (and
// counted) rather than failing the whole call — local readers must degrade
// gracefully on a partial write.
//
// Returns (records, skippedLines, error). The error is non-nil only when the
// file cannot be opened (and the file is not simply absent). An absent file
// returns (nil, 0, nil).
func loadLedgerRecords(path string) (map[string]ampLedgerRecord, int, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("amp: opening ledger %s: %w", filepath.Base(path), err)
	}
	defer f.Close()

	out := make(map[string]ampLedgerRecord)
	scanner := bufio.NewScanner(f)
	// Allow long ledger lines (some thread metadata gets fat).
	scanner.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)
	var skipped int

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec ampLedgerRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			skipped++
			continue
		}
		id := rec.effectiveToMessageID()
		if id == "" {
			// Unkeyed ledger rows cannot be reconciled, but they shouldn't
			// cause a hard skip — keep them out of the map but count.
			skipped++
			continue
		}
		// When multiple ledger rows reference the same message id, prefer
		// the one with the highest credit value (matches the per-field max-
		// merge convention used for the thread side).
		if existing, ok := out[id]; ok {
			rec = maxMergeLedgerRecord(existing, rec)
		}
		out[id] = rec
	}
	if err := scanner.Err(); err != nil {
		return out, skipped, fmt.Errorf("amp: scanning ledger %s: %w", filepath.Base(path), err)
	}
	return out, skipped, nil
}

// maxMergeLedgerRecord folds two ledger records that reference the same
// message id. We keep the higher credit value and the higher per-field
// token counts; the timestamp uses whichever record provided one.
func maxMergeLedgerRecord(a, b ampLedgerRecord) ampLedgerRecord {
	out := a
	if b.effectiveCost() > a.effectiveCost() {
		out.Credits = b.effectiveCost()
		out.Cost = 0
	}
	if a.Model == "" && b.Model != "" {
		out.Model = b.Model
	}
	if a.effectiveTimestamp() == "" && b.effectiveTimestamp() != "" {
		out.Timestamp = b.effectiveTimestamp()
		out.CreatedAt = ""
		out.CreatedAtCC = ""
	}
	tokensA := a.effectiveTokens()
	tokensB := b.effectiveTokens()
	merged := ampTokens{
		Input:      maxInt64(tokensA.Input, tokensB.Input),
		Output:     maxInt64(tokensA.Output, tokensB.Output),
		CacheRead:  maxInt64(tokensA.CacheRead, tokensB.CacheRead),
		CacheWrite: maxInt64(tokensA.CacheWrite, tokensB.CacheWrite),
	}
	out.Tokens = &merged
	out.Usage = nil
	return out
}

// reconcileWithLedger merges a set of thread-derived events with their
// matching ledger records and includes any ledger records that did not match
// a thread message. The returned slice is sorted chronologically.
//
// When a thread event matches a ledger record by message id:
//   - cost is taken from the ledger (authoritative billing unit)
//   - tokens are per-field max-merged across both sources
//   - timestamp prefers the ledger record when it carries an explicit one,
//     otherwise the thread's
//   - model prefers the ledger value when present, otherwise the thread's
func reconcileWithLedger(events []ampEvent, ledger map[string]ampLedgerRecord) []ampEvent {
	if len(ledger) == 0 {
		// Nothing to merge; just hand back a stable, sorted copy.
		out := append([]ampEvent(nil), events...)
		sortEventsChronological(out)
		return out
	}

	matched := make(map[string]struct{}, len(events))
	merged := make([]ampEvent, 0, len(events))

	for _, evt := range events {
		if evt.MessageID == "" {
			merged = append(merged, evt)
			continue
		}
		rec, ok := ledger[evt.MessageID]
		if !ok {
			merged = append(merged, evt)
			continue
		}
		merged = append(merged, mergeEventWithLedger(evt, rec))
		matched[evt.MessageID] = struct{}{}
	}

	// Include any ledger-only records that did not pair to a thread message.
	for id, rec := range ledger {
		if _, ok := matched[id]; ok {
			continue
		}
		merged = append(merged, eventFromLedgerOnly(id, rec))
	}

	sortEventsChronological(merged)
	return merged
}

// mergeEventWithLedger applies the ledger record onto the thread-side event.
func mergeEventWithLedger(evt ampEvent, rec ampLedgerRecord) ampEvent {
	ledgerTokens := rec.effectiveTokens()
	evt.Tokens = ampTokens{
		Input:      maxInt64(evt.Tokens.Input, ledgerTokens.Input),
		Output:     maxInt64(evt.Tokens.Output, ledgerTokens.Output),
		CacheRead:  maxInt64(evt.Tokens.CacheRead, ledgerTokens.CacheRead),
		CacheWrite: maxInt64(evt.Tokens.CacheWrite, ledgerTokens.CacheWrite),
	}
	if cost := rec.effectiveCost(); cost > 0 {
		evt.CreditCost = cost
	}
	if rec.Model != "" {
		evt.Model = strings.TrimSpace(rec.Model)
	}
	if ts, ok := parseRFC3339Any(rec.effectiveTimestamp()); ok {
		evt.Timestamp = ts
	}
	evt.Source = "merged"
	return evt
}

// eventFromLedgerOnly synthesises an ampEvent for a ledger record that had no
// matching thread message. Used when the ledger advances ahead of the thread
// log (e.g. the user reloaded the thread JSON before the next poll).
func eventFromLedgerOnly(messageID string, rec ampLedgerRecord) ampEvent {
	evt := ampEvent{
		MessageID:  messageID,
		Model:      strings.TrimSpace(rec.Model),
		Tokens:     rec.effectiveTokens(),
		CreditCost: rec.effectiveCost(),
		Source:     "ledger",
	}
	if ts, ok := parseRFC3339Any(rec.effectiveTimestamp()); ok {
		evt.Timestamp = ts
	}
	return evt
}

// sortEventsChronological orders events by timestamp ascending, with
// MessageID as a stable tie-breaker.
func sortEventsChronological(events []ampEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		ti := events[i].Timestamp
		tj := events[j].Timestamp
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return events[i].MessageID < events[j].MessageID
	})
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
