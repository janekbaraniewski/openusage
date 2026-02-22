package telemetry

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Spool struct {
	dir string
}

type SpoolRecord struct {
	SpoolID       string          `json:"spool_id"`
	CreatedAt     time.Time       `json:"created_at"`
	SourceSystem  SourceSystem    `json:"source_system"`
	SourceChannel SourceChannel   `json:"source_channel"`
	Payload       json.RawMessage `json:"payload"`
	Attempt       int             `json:"attempt"`
	LastError     string          `json:"last_error,omitempty"`
}

type PendingRecord struct {
	Path   string
	Record SpoolRecord
}

func NewSpool(dir string) *Spool {
	return &Spool{dir: dir}
}

func DefaultSpoolDir() (string, error) {
	if base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); base != "" {
		return filepath.Join(base, "openusage", "telemetry-spool"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("telemetry spool: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "openusage", "telemetry-spool"), nil
}

func (s *Spool) Append(record SpoolRecord) (string, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", fmt.Errorf("telemetry spool: create dir: %w", err)
	}

	if record.SpoolID == "" {
		id, err := newUUID()
		if err != nil {
			return "", fmt.Errorf("telemetry spool: create spool_id: %w", err)
		}
		record.SpoolID = id
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	} else {
		record.CreatedAt = record.CreatedAt.UTC()
	}
	if len(record.Payload) == 0 {
		record.Payload = json.RawMessage("{}")
	}

	name := fmt.Sprintf("%019d_%s.jsonl", record.CreatedAt.UnixNano(), sanitizeFileComponent(record.SpoolID))
	path := filepath.Join(s.dir, name)
	return path, writeSpoolFile(path, record)
}

func (s *Spool) ReadOldest(limit int) ([]PendingRecord, error) {
	if limit == 0 {
		return nil, nil
	}
	files, err := filepath.Glob(filepath.Join(s.dir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("telemetry spool: glob files: %w", err)
	}
	sort.Strings(files)

	records := make([]PendingRecord, 0, len(files))
	malformed := 0
	for _, path := range files {
		if limit > 0 && len(records) >= limit {
			break
		}
		rec, ok := readSpoolFile(path)
		if !ok {
			malformed++
			continue
		}
		records = append(records, PendingRecord{Path: path, Record: rec})
	}

	if malformed > 0 {
		return records, fmt.Errorf("telemetry spool: skipped %d malformed file(s)", malformed)
	}
	return records, nil
}

func (s *Spool) Ack(path string) error {
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("telemetry spool: ack remove %s: %w", path, err)
	}
	return nil
}

func (s *Spool) MarkFailed(path, lastError string) error {
	rec, ok := readSpoolFile(path)
	if !ok {
		return fmt.Errorf("telemetry spool: read %s for mark failed", path)
	}
	rec.Attempt++
	rec.LastError = lastError
	return writeSpoolFile(path, rec)
}

func readSpoolFile(path string) (SpoolRecord, bool) {
	f, err := os.Open(path)
	if err != nil {
		return SpoolRecord{}, false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return SpoolRecord{}, false
	}
	line := strings.TrimSpace(scanner.Text())
	if line == "" {
		return SpoolRecord{}, false
	}

	var rec SpoolRecord
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return SpoolRecord{}, false
	}
	if rec.SpoolID == "" {
		return SpoolRecord{}, false
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	if len(rec.Payload) == 0 {
		rec.Payload = json.RawMessage("{}")
	}
	return rec, true
}

func writeSpoolFile(path string, rec SpoolRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("telemetry spool: marshal record: %w", err)
	}
	data = append(data, '\n')

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("telemetry spool: write tmp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("telemetry spool: rename tmp file: %w", err)
	}
	return nil
}

func sanitizeFileComponent(v string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		" ", "_",
	)
	return replacer.Replace(v)
}
