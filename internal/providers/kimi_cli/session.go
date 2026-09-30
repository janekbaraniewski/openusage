package kimi_cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

// defaultModel is the fallback model name used when config.json doesn't
// declare one and individual wire records don't carry per-message model.
const defaultModel = "kimi-for-coding"

// defaultProvider tags model usage with the upstream provider hint.
const defaultProvider = "moonshot"

// kimiWireRecord mirrors one line of wire.jsonl. The shape is mixed across
// record kinds; we only decode the fields needed to recognise StatusUpdate
// frames carrying token_usage (Python Kimi CLI) and usage.record frames
// (Kimi Code CLI).
type kimiWireRecord struct {
	Timestamp float64          `json:"timestamp"`
	Message   *kimiWireMessage `json:"message,omitempty"`

	// Kimi Code CLI usage.record fields. The timestamp is epoch
	// milliseconds and the usage fields are camelCase, unlike the
	// StatusUpdate frames above.
	Type  string              `json:"type,omitempty"`
	Model string              `json:"model,omitempty"`
	Usage *kimiCodeTokenUsage `json:"usage,omitempty"`
	Time  int64               `json:"time,omitempty"`
}

type kimiWireMessage struct {
	Type    string           `json:"type,omitempty"`
	Payload *kimiWirePayload `json:"payload,omitempty"`
}

type kimiWirePayload struct {
	TokenUsage *kimiTokenUsage `json:"token_usage,omitempty"`
	MessageID  string          `json:"message_id,omitempty"`
	Model      string          `json:"model,omitempty"`
}

type kimiTokenUsage struct {
	InputOther         *int64 `json:"input_other,omitempty"`
	Output             *int64 `json:"output,omitempty"`
	InputCacheRead     *int64 `json:"input_cache_read,omitempty"`
	InputCacheCreation *int64 `json:"input_cache_creation,omitempty"`
}

// kimiCodeTokenUsage mirrors the usage object of a Kimi Code CLI
// usage.record frame.
type kimiCodeTokenUsage struct {
	InputOther         *int64 `json:"inputOther,omitempty"`
	Output             *int64 `json:"output,omitempty"`
	InputCacheRead     *int64 `json:"inputCacheRead,omitempty"`
	InputCacheCreation *int64 `json:"inputCacheCreation,omitempty"`
}

// kimiModelEntry is the flattened representation we emit downstream.
type kimiModelEntry struct {
	SessionID  string
	Provider   string
	Model      string
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	Timestamp  time.Time
}

// kimiConfig mirrors the subset of ~/.kimi/config.json we care about.
type kimiConfig struct {
	Model string `json:"model,omitempty"`
}

// readKimiConfigModel returns the default model declared in Kimi CLI's
// config.json, falling back to defaultModel when the file is missing,
// unreadable, or doesn't declare a model.
func readKimiConfigModel(path string) string {
	if path == "" {
		return defaultModel
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultModel
	}
	var cfg kimiConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultModel
	}
	if cfg.Model == "" {
		return defaultModel
	}
	return cfg.Model
}

// readKimiWireFile parses one wire.jsonl file and returns flattened per-record
// entries. Returns nil (no error) when the file is missing or empty so
// directory walks can keep going. Per-line malformed JSON is skipped
// silently; only I/O failures surface as errors.
func readKimiWireFile(path string) ([]kimiModelEntry, error) {
	return readKimiWireFileWithModel(path, defaultModel)
}

// readKimiWireFileWithModel is the variant used by the provider; it allows
// the caller to inject the model name resolved from config.json.
func readKimiWireFileWithModel(path, fallbackModel string) ([]kimiModelEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("kimi_cli: opening %s: %w", path, err)
	}
	defer f.Close()

	// Layout is <root>/<group>/<uuid>/wire.jsonl; combine both to avoid
	// collisions when the same UUID appears under different groups. Kimi
	// Code CLI nests logs deeper at <root>/<group>/<uuid>/agents/<agent>/
	// wire.jsonl, so climb out of the agents directory to keep the same
	// <group>/<uuid> session id.
	uuidDir := filepath.Dir(path)
	if filepath.Base(filepath.Dir(uuidDir)) == "agents" {
		uuidDir = filepath.Dir(filepath.Dir(uuidDir))
	}
	sessionID := filepath.Base(uuidDir)
	if group := filepath.Base(filepath.Dir(uuidDir)); group != "" && group != "." && group != string(filepath.Separator) {
		sessionID = group + "/" + sessionID
	}
	if fallbackModel == "" {
		fallbackModel = defaultModel
	}

	scanner := bufio.NewScanner(f)
	// Wire records can include long tool-call payloads; bump the limit so
	// we don't truncate large StatusUpdate frames.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var out []kimiModelEntry
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec kimiWireRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		entry, ok := rec.toEntry(sessionID, fallbackModel)
		if !ok {
			continue
		}
		out = append(out, entry)
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("kimi_cli: scanning %s: %w", path, err)
	}
	return out, nil
}

// toEntry flattens one decoded wire record into a usage entry, recognising
// both kimi-cli StatusUpdate frames and Kimi Code CLI usage.record frames.
// ok is false for records that carry no token usage.
func (rec kimiWireRecord) toEntry(sessionID, fallbackModel string) (entry kimiModelEntry, ok bool) {
	if rec.Type == "usage.record" && rec.Usage != nil {
		input := derefInt64(rec.Usage.InputOther)
		output := derefInt64(rec.Usage.Output)
		cacheRead := derefInt64(rec.Usage.InputCacheRead)
		cacheWrite := derefInt64(rec.Usage.InputCacheCreation)
		if input == 0 && output == 0 && cacheRead == 0 && cacheWrite == 0 {
			return kimiModelEntry{}, false
		}
		model := rec.Model
		if model == "" {
			model = fallbackModel
		}
		return kimiModelEntry{
			SessionID:  sessionID,
			Provider:   defaultProvider,
			Model:      model,
			Input:      input,
			Output:     output,
			CacheRead:  cacheRead,
			CacheWrite: cacheWrite,
			Timestamp:  millisToTime(rec.Time),
		}, true
	}

	if rec.Message == nil || rec.Message.Type != "StatusUpdate" {
		return kimiModelEntry{}, false
	}
	if rec.Message.Payload == nil || rec.Message.Payload.TokenUsage == nil {
		return kimiModelEntry{}, false
	}

	usage := rec.Message.Payload.TokenUsage
	input := derefInt64(usage.InputOther)
	output := derefInt64(usage.Output)
	cacheRead := derefInt64(usage.InputCacheRead)
	cacheWrite := derefInt64(usage.InputCacheCreation)
	if input == 0 && output == 0 && cacheRead == 0 && cacheWrite == 0 {
		return kimiModelEntry{}, false
	}

	model := rec.Message.Payload.Model
	if model == "" {
		model = fallbackModel
	}

	return kimiModelEntry{
		SessionID:  sessionID,
		Provider:   defaultProvider,
		Model:      model,
		Input:      input,
		Output:     output,
		CacheRead:  cacheRead,
		CacheWrite: cacheWrite,
		Timestamp:  floatToTime(rec.Timestamp),
	}, true
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	v := *p
	if v < 0 {
		return 0
	}
	return v
}

// millisToTime converts an epoch-milliseconds timestamp (Kimi Code CLI
// usage.record "time" field) into a UTC time.Time. Returns the zero time
// for non-positive inputs.
func millisToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// floatToTime converts a float-seconds-since-epoch timestamp (with sub-second
// precision) into a UTC time.Time. Returns the zero time for non-positive
// inputs.
func floatToTime(ts float64) time.Time {
	if ts <= 0 || math.IsNaN(ts) || math.IsInf(ts, 0) {
		return time.Time{}
	}
	sec := int64(ts)
	nsec := int64(math.Round((ts - float64(sec)) * 1e9))
	if nsec < 0 {
		nsec = 0
	}
	return time.Unix(sec, nsec).UTC()
}
