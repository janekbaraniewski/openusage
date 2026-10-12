package dsh

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Event types of the v4 session log that carry usage or the state needed to
// attribute it. Every other type is skipped without being decoded.
const (
	eventRequestContext    = "request/context"
	eventAssistantMessage  = "assistant/message"
	eventCompactionSummary = "compaction/summary"
	eventToolCall          = "tool/call"
	eventToolResult        = "tool/result"
)

// typePrefix is how every log line starts; it lets the reader discard
// irrelevant lines (message bodies, tool output) without parsing them.
var typePrefix = []byte(`{"type":"`)

// sessionHeader mirrors the first line of a session log.
type sessionHeader struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CreatedAt int64  `json:"createdAt"`
	CWD       string `json:"cwd"`
	IsSeeded  bool   `json:"isSeeded"`
}

// logRecord mirrors every later line: an envelope around a typed payload.
type logRecord struct {
	Type string          `json:"type"`
	Seq  int64           `json:"seq"`
	Time int64           `json:"time"`
	Data json.RawMessage `json:"data"`
}

// tokenUsage mirrors a usage object. inputTokens excludes cache reads and
// reasoningTokens is a subset of outputTokens, so totalTokens equals
// input + output + cache read + cache write.
type tokenUsage struct {
	InputTokens      *int64 `json:"inputTokens"`
	OutputTokens     *int64 `json:"outputTokens"`
	CacheReadTokens  *int64 `json:"cacheReadTokens"`
	CacheWriteTokens *int64 `json:"cacheWriteTokens"`
	ReasoningTokens  *int64 `json:"reasoningTokens"`
	TotalTokens      *int64 `json:"totalTokens"`
}

type requestContextData struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type assistantMessageData struct {
	Turn    *int64 `json:"turn"`
	Step    *int64 `json:"step"`
	Message struct {
		ID string `json:"id"`
	} `json:"message"`
	Usage *tokenUsage `json:"usage"`
	// Stream is decoded lazily: it holds the whole response and is only
	// needed when no request/context named the route.
	Stream json.RawMessage `json:"stream"`
}

type compactionSummaryData struct {
	CompactionID string      `json:"compactionId"`
	Provider     string      `json:"provider"`
	Model        string      `json:"model"`
	Usage        *tokenUsage `json:"usage"`
}

type toolCallData struct {
	Name      string          `json:"name"`
	CallID    string          `json:"callId"`
	Arguments json.RawMessage `json:"arguments"`
	Turn      *int64          `json:"turn"`
	Step      *int64          `json:"step"`
}

type toolResultData struct {
	Message struct {
		ToolCallID string `json:"toolCallId"`
		IsError    bool   `json:"isError"`
	} `json:"message"`
}

// streamResponse mirrors replayState.response of a stream's finish chunk.
type streamResponse struct {
	Kind     string `json:"kind"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// sessionData is everything one session log contributes.
type sessionData struct {
	ID        string
	CWD       string
	CreatedAt time.Time
	Messages  []messageEntry
	Tools     []toolEntry
}

// messageEntry is one model response that reported token usage.
type messageEntry struct {
	// ID is unique within the session and stable across re-reads.
	ID       string
	Turn     *int64
	Time     time.Time
	Model    string
	Upstream string

	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	Reasoning  int64
	Total      int64
}

// toolEntry is one completed tool call.
type toolEntry struct {
	CallID   string
	Name     string
	Turn     *int64
	Time     time.Time
	Model    string
	Upstream string
	IsError  bool
	// File is the target of a file tool, kept for language attribution.
	File string
}

// readSessionFile decompresses and parses one session log. It returns nil
// (no error) when the file is missing or does not start with a session
// header. A log that ends mid-frame, as one being appended to can, yields
// the events read so far; malformed lines are skipped individually.
func readSessionFile(path string) (*sessionData, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("dsh: opening %s: %w", path, err)
	}
	defer f.Close()

	dec, err := zstd.NewReader(f, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, fmt.Errorf("dsh: reading %s: %w", path, err)
	}
	defer dec.Close()

	reader := bufio.NewReaderSize(dec, 256*1024)

	headerLine, err := readLine(reader)
	if len(bytes.TrimSpace(headerLine)) == 0 {
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("dsh: reading %s: %w", path, err)
		}
		return nil, nil
	}
	var header sessionHeader
	if jsonErr := json.Unmarshal(headerLine, &header); jsonErr != nil || header.Type != "session" {
		return nil, nil
	}

	session := &sessionData{
		ID:  strings.TrimSpace(header.ID),
		CWD: strings.TrimSpace(header.CWD),
	}
	if session.ID == "" {
		session.ID = filepath.Base(filepath.Dir(path))
	}
	if header.CreatedAt > 0 {
		session.CreatedAt = time.UnixMilli(header.CreatedAt).UTC()
	}

	parser := sessionParser{session: session, header: header, pending: make(map[string]pendingTool)}
	for err == nil {
		var line []byte
		line, err = readLine(reader)
		if len(line) > 0 {
			parser.consume(line)
		}
	}
	return session, nil
}

// readLine returns the next line without its terminator. Lines are read
// whole, however long: a single assistant message can run to megabytes.
func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadBytes('\n')
	return bytes.TrimRight(line, "\r\n"), err
}

// peekType extracts the event type of a line that starts with the usual
// `{"type":"…"` prefix. ok is false when the line has another shape and must
// be decoded to find out.
func peekType(line []byte) (eventType string, ok bool) {
	if !bytes.HasPrefix(line, typePrefix) {
		return "", false
	}
	rest := line[len(typePrefix):]
	end := bytes.IndexByte(rest, '"')
	if end < 0 || bytes.IndexByte(rest[:end], '\\') >= 0 {
		return "", false
	}
	return string(rest[:end]), true
}

func relevantEventType(eventType string) bool {
	switch eventType {
	case eventRequestContext, eventAssistantMessage, eventCompactionSummary, eventToolCall, eventToolResult:
		return true
	}
	return false
}

type pendingTool struct {
	entry     toolEntry
	inherited bool
}

// sessionParser folds log lines into a sessionData, tracking the route that
// is in effect and the tool calls still waiting for their result.
type sessionParser struct {
	session *sessionData
	header  sessionHeader

	routeProvider string
	routeModel    string

	pending map[string]pendingTool
}

func (p *sessionParser) consume(line []byte) {
	if eventType, ok := peekType(line); ok && !relevantEventType(eventType) {
		return
	}
	var rec logRecord
	if err := json.Unmarshal(line, &rec); err != nil || !relevantEventType(rec.Type) || len(rec.Data) == 0 {
		return
	}

	switch rec.Type {
	case eventRequestContext:
		var data requestContextData
		if json.Unmarshal(rec.Data, &data) != nil {
			return
		}
		p.routeProvider = strings.TrimSpace(data.Provider)
		p.routeModel = strings.TrimSpace(data.Model)

	case eventAssistantMessage:
		var data assistantMessageData
		if json.Unmarshal(rec.Data, &data) != nil || data.Usage == nil || p.inherited(rec) {
			return
		}
		model, upstream := p.routeModel, upstreamProvider(p.routeProvider, "")
		if model == "" || upstream == "" {
			response := finishResponse(data.Stream)
			if model == "" {
				model = strings.TrimSpace(response.Model)
			}
			if upstream == "" {
				upstream = upstreamProvider(response.Provider, response.Kind)
			}
		}
		id := strings.TrimSpace(data.Message.ID)
		if id == "" {
			id = fmt.Sprintf("seq:%d", rec.Seq)
		}
		p.addMessage(id, data.Turn, rec, model, upstream, *data.Usage)

	case eventCompactionSummary:
		var data compactionSummaryData
		if json.Unmarshal(rec.Data, &data) != nil || data.Usage == nil || p.inherited(rec) {
			return
		}
		model := strings.TrimSpace(data.Model)
		upstream := upstreamProvider(data.Provider, "")
		if model == "" {
			model = p.routeModel
		}
		if upstream == "" {
			upstream = upstreamProvider(p.routeProvider, "")
		}
		id := strings.TrimSpace(data.CompactionID)
		if id == "" {
			id = fmt.Sprintf("seq:%d", rec.Seq)
		}
		p.addMessage("compaction:"+id, nil, rec, model, upstream, *data.Usage)

	case eventToolCall:
		var data toolCallData
		if json.Unmarshal(rec.Data, &data) != nil {
			return
		}
		callID := strings.TrimSpace(data.CallID)
		name := strings.TrimSpace(data.Name)
		if callID == "" || name == "" {
			return
		}
		p.pending[callID] = pendingTool{
			inherited: p.inherited(rec),
			entry: toolEntry{
				CallID:   callID,
				Name:     name,
				Turn:     data.Turn,
				Time:     p.eventTime(rec),
				Model:    p.routeModel,
				Upstream: upstreamProvider(p.routeProvider, ""),
				File:     toolTargetFile(data.Arguments),
			},
		}

	case eventToolResult:
		var data toolResultData
		if json.Unmarshal(rec.Data, &data) != nil {
			return
		}
		callID := strings.TrimSpace(data.Message.ToolCallID)
		call, ok := p.pending[callID]
		if !ok {
			return
		}
		delete(p.pending, callID)
		if call.inherited {
			return
		}
		call.entry.IsError = data.Message.IsError
		p.session.Tools = append(p.session.Tools, call.entry)
	}
}

func (p *sessionParser) addMessage(id string, turn *int64, rec logRecord, model, upstream string, usage tokenUsage) {
	entry := messageEntry{
		ID:         id,
		Turn:       turn,
		Time:       p.eventTime(rec),
		Model:      model,
		Upstream:   upstream,
		Input:      nonNegative(usage.InputTokens),
		Output:     nonNegative(usage.OutputTokens),
		CacheRead:  nonNegative(usage.CacheReadTokens),
		CacheWrite: nonNegative(usage.CacheWriteTokens),
		Reasoning:  nonNegative(usage.ReasoningTokens),
		Total:      nonNegative(usage.TotalTokens),
	}
	if entry.Total == 0 {
		entry.Total = entry.Input + entry.Output + entry.CacheRead + entry.CacheWrite
	}
	if entry.Total == 0 {
		return
	}
	p.session.Messages = append(p.session.Messages, entry)
}

// inherited reports whether an event was copied in from the session this one
// was seeded from. A seeded log opens with its source's history, ids and
// timestamps intact, so those events predate the session's own creation and
// are already counted under the source session.
func (p *sessionParser) inherited(rec logRecord) bool {
	return p.header.IsSeeded && rec.Time > 0 && p.header.CreatedAt > 0 && rec.Time < p.header.CreatedAt
}

func (p *sessionParser) eventTime(rec logRecord) time.Time {
	if rec.Time > 0 {
		return time.UnixMilli(rec.Time).UTC()
	}
	return p.session.CreatedAt
}

// finishResponse pulls the response metadata out of a message's stream. The
// stream mixes chunk shapes and null entries, so each one is decoded on its
// own and anything unexpected is ignored.
func finishResponse(stream json.RawMessage) streamResponse {
	if len(stream) == 0 {
		return streamResponse{}
	}
	var entries []struct {
		Chunk json.RawMessage `json:"chunk"`
	}
	if json.Unmarshal(stream, &entries) != nil {
		return streamResponse{}
	}
	for _, entry := range entries {
		if len(entry.Chunk) == 0 || entry.Chunk[0] != '{' {
			continue
		}
		var chunk struct {
			Type        string `json:"type"`
			ReplayState struct {
				Response streamResponse `json:"response"`
			} `json:"replayState"`
		}
		if json.Unmarshal(entry.Chunk, &chunk) != nil || chunk.Type != "finish" {
			continue
		}
		return chunk.ReplayState.Response
	}
	return streamResponse{}
}

// toolTargetFile returns the file_path argument of a tool call. Arguments are
// logged as a JSON-encoded string; an inline object is accepted too.
func toolTargetFile(arguments json.RawMessage) string {
	raw := bytes.TrimSpace(arguments)
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var encoded string
		if json.Unmarshal(raw, &encoded) != nil {
			return ""
		}
		raw = []byte(strings.TrimSpace(encoded))
	}
	if len(raw) == 0 || raw[0] != '{' {
		return ""
	}
	var args struct {
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal(raw, &args) != nil {
		return ""
	}
	return strings.TrimSpace(args.FilePath)
}

func nonNegative(p *int64) int64 {
	if p == nil || *p < 0 {
		return 0
	}
	return *p
}
