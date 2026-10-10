package claude_code

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"
)

type conversationRecord struct {
	lineNumber int
	timestamp  time.Time
	model      string
	usage      *jsonlUsage
	requestID  string
	messageID  string
	sessionID  string
	cwd        string
	sourcePath string
	content    []jsonlContent
	agentID    string
	costUSD    *float64 // pre-computed cost from the JSONL entry, if present
}

func parseConversationRecords(path string) []conversationRecord {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Resolve the agent label once per file. Detection touches sidecar
	// metadata and (optionally) the parent transcript, so we want to amortise
	// the I/O across every record in the file.
	agentLabel := detectAgentType(path)

	var records []conversationRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		if rec, ok := conversationRecordFromLine(scanner.Bytes(), lineNumber, path, agentLabel); ok {
			records = append(records, rec)
		}
	}
	return mergeStreamingDuplicates(records)
}

// conversationRecordFromLine decodes one JSONL line into an assistant
// conversationRecord. The dashboard parser and both telemetry parsers (full
// file and append-only) share it so every path runs the same records through
// mergeStreamingDuplicates.
func conversationRecordFromLine(line []byte, lineNumber int, path, agentLabel string) (conversationRecord, bool) {
	if len(line) == 0 {
		return conversationRecord{}, false
	}
	var entry jsonlEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return conversationRecord{}, false
	}
	if entry.Type != "assistant" || entry.Message == nil {
		return conversationRecord{}, false
	}
	ts, ok := parseJSONLTimestamp(entry.Timestamp)
	if !ok {
		return conversationRecord{}, false
	}
	model := entry.Message.Model
	if model == "" {
		model = "unknown"
	}
	return conversationRecord{
		lineNumber: lineNumber,
		timestamp:  ts,
		model:      model,
		usage:      entry.Message.Usage,
		requestID:  entry.RequestID,
		messageID:  entry.Message.ID,
		sessionID:  entry.SessionID,
		cwd:        entry.CWD,
		sourcePath: path,
		content:    entry.Message.Content,
		agentID:    agentLabel,
		costUSD:    entry.CostUSD,
	}, true
}

// mergeStreamingDuplicates collapses records that share a non-empty
// `messageId:requestId` composite key into a single record. Claude Code
// streams one assistant message as several lines, one content block per line
// (thinking, text, then each tool_use), and every line repeats the message's
// usage: input and cache counts are identical across lines while
// output_tokens grows to the final total on the last line. So:
//
//   - token fields take the per-field MAX (summing would multiply input and
//     cache tokens by the line count; first-wins undercounts output), and
//   - content blocks are unioned in line order, because keeping only the
//     first line drops every tool_use that arrives on a later line.
//
// Records without enough information to form a composite key are passed
// through unchanged; downstream block-level dedup still applies.
func mergeStreamingDuplicates(records []conversationRecord) []conversationRecord {
	if len(records) < 2 {
		return records
	}
	// Track the slot for each composite key so we can mutate the existing
	// record in place while preserving the original ordering.
	indexByKey := make(map[string]int, len(records))
	out := records[:0]
	for _, rec := range records {
		key := streamingMergeKey(rec)
		if key == "" || rec.usage == nil {
			out = append(out, rec)
			continue
		}
		if existingIdx, ok := indexByKey[key]; ok {
			mergeUsageMax(out[existingIdx].usage, rec.usage)
			out[existingIdx].content = appendNewContentBlocks(out[existingIdx].content, rec.content)
			// Prefer the earliest timestamp so block boundaries stay
			// stable; line number and source path stay with the first
			// record we encountered.
			if rec.timestamp.Before(out[existingIdx].timestamp) {
				out[existingIdx].timestamp = rec.timestamp
			}
			// Keep the largest reported cost across the streamed partials so
			// the "display"/"auto" cost modes match the final turn cost.
			if rec.costUSD != nil {
				if out[existingIdx].costUSD == nil || *rec.costUSD > *out[existingIdx].costUSD {
					out[existingIdx].costUSD = rec.costUSD
				}
			}
			continue
		}
		indexByKey[key] = len(out)
		out = append(out, rec)
	}
	return out
}

// appendNewContentBlocks appends the blocks of a later streamed line that the
// merged record does not hold yet. Blocks with an id (tool_use) match by id,
// id-less blocks by type, name and input, so a line repeated verbatim in the
// same file does not count its tool call twice.
func appendNewContentBlocks(dst, src []jsonlContent) []jsonlContent {
	for _, block := range src {
		if !containsContentBlock(dst, block) {
			dst = append(dst, block)
		}
	}
	return dst
}

func containsContentBlock(blocks []jsonlContent, block jsonlContent) bool {
	for _, existing := range blocks {
		if existing.Type != block.Type {
			continue
		}
		if existing.ID != "" || block.ID != "" {
			if existing.ID == block.ID {
				return true
			}
			continue
		}
		if existing.Name == block.Name && reflect.DeepEqual(existing.Input, block.Input) {
			return true
		}
	}
	return false
}

func streamingMergeKey(r conversationRecord) string {
	if r.messageID == "" && r.requestID == "" {
		return ""
	}
	return r.messageID + ":" + r.requestID
}

func mergeUsageMax(dst, src *jsonlUsage) {
	if dst == nil || src == nil {
		return
	}
	dst.InputTokens = maxInt(dst.InputTokens, src.InputTokens)
	dst.OutputTokens = maxInt(dst.OutputTokens, src.OutputTokens)
	dst.CacheReadInputTokens = maxInt(dst.CacheReadInputTokens, src.CacheReadInputTokens)
	dst.CacheCreationInputTokens = maxInt(dst.CacheCreationInputTokens, src.CacheCreationInputTokens)
	dst.ReasoningTokens = maxInt(dst.ReasoningTokens, src.ReasoningTokens)

	if src.CacheCreation != nil {
		if dst.CacheCreation == nil {
			copy := *src.CacheCreation
			dst.CacheCreation = &copy
		} else {
			dst.CacheCreation.Ephemeral5mInputTokens = maxInt(dst.CacheCreation.Ephemeral5mInputTokens, src.CacheCreation.Ephemeral5mInputTokens)
			dst.CacheCreation.Ephemeral1hInputTokens = maxInt(dst.CacheCreation.Ephemeral1hInputTokens, src.CacheCreation.Ephemeral1hInputTokens)
		}
	}
	if src.ServerToolUse != nil {
		if dst.ServerToolUse == nil {
			copy := *src.ServerToolUse
			dst.ServerToolUse = &copy
		} else {
			dst.ServerToolUse.WebSearchRequests = maxInt(dst.ServerToolUse.WebSearchRequests, src.ServerToolUse.WebSearchRequests)
			dst.ServerToolUse.WebFetchRequests = maxInt(dst.ServerToolUse.WebFetchRequests, src.ServerToolUse.WebFetchRequests)
		}
	}
	if dst.ServiceTier == "" && src.ServiceTier != "" {
		dst.ServiceTier = src.ServiceTier
	}
	if dst.InferenceGeo == "" && src.InferenceGeo != "" {
		dst.InferenceGeo = src.InferenceGeo
	}
}

func maxInt(a, b int) int {
	if a >= b {
		return a
	}
	return b
}

func conversationUsageDedupKey(record conversationRecord) string {
	if record.requestID != "" {
		return "req:" + record.requestID
	}
	if record.messageID != "" {
		return "msg:" + record.messageID
	}
	if record.usage == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%d|%d|%d|%d|%d",
		record.sessionID,
		record.timestamp.UTC().Format(time.RFC3339Nano),
		record.usage.InputTokens,
		record.usage.OutputTokens,
		record.usage.CacheReadInputTokens,
		record.usage.CacheCreationInputTokens,
		record.usage.ReasoningTokens,
	)
}

func conversationToolDedupKey(record conversationRecord, idx int, item jsonlContent) string {
	base := record.requestID
	if base == "" {
		base = record.messageID
	}
	if base == "" {
		base = record.sessionID + "|" + record.timestamp.UTC().Format(time.RFC3339Nano)
	}
	if item.ID != "" {
		return base + "|tool|" + item.ID
	}
	name := strings.ToLower(strings.TrimSpace(item.Name))
	if name == "" {
		name = "unknown"
	}
	return fmt.Sprintf("%s|tool|%s|%d", base, name, idx)
}

func conversationTotalTokens(usage *jsonlUsage) int64 {
	if usage == nil {
		return 0
	}
	return int64(
		usage.InputTokens +
			usage.OutputTokens +
			usage.CacheReadInputTokens +
			usage.CacheCreationInputTokens +
			usage.ReasoningTokens,
	)
}
