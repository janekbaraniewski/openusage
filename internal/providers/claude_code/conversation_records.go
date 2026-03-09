package claude_code

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
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
}

func parseConversationRecords(path string) []conversationRecord {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var records []conversationRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry jsonlEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Type != "assistant" || entry.Message == nil {
			continue
		}
		ts, ok := parseJSONLTimestamp(entry.Timestamp)
		if !ok {
			continue
		}
		model := entry.Message.Model
		if model == "" {
			model = "unknown"
		}
		records = append(records, conversationRecord{
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
		})
	}
	return records
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
