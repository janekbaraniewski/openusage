package cursor

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// Cursor CSV export schema versions, detected from the header row.
//
// v1 columns: Date, Kind, Model, Input (w/ Cache Write), Input (w/o Cache Write),
//
//	Cache Read, Output Tokens, Total Tokens, Cost
//
// v2 adds: Max Mode (boolean column between Model and the token columns)
//
// TODO(cursor): v3 may add Cloud Agent ID, Automation ID, Request ID columns
// once Cursor ships them in the public export. Per recon §7.2 those columns
// are speculative today, so v3 parsing is deliberately deferred.
const (
	cursorCSVVersionUnknown = 0
	cursorCSVV1             = 1
	cursorCSVV2             = 2
)

// cursorCSVRecord is one parsed row from a Cursor usage CSV export.
type cursorCSVRecord struct {
	Date              time.Time
	User              string
	Kind              string
	Model             string
	MaxMode           bool
	InputWithCache    int64
	InputWithoutCache int64
	CacheRead         int64
	OutputTokens      int64
	TotalTokens       int64
	Cost              float64
}

// detectCSVVersion inspects normalized header names and returns the schema
// version. Detection rules:
//   - presence of "max mode" header → v2
//   - otherwise, if the v1 required columns are present → v1
//   - otherwise → unknown
func detectCursorCSVVersion(headers []string) int {
	hasMaxMode := false
	hasDate := false
	hasModel := false
	hasCost := false
	for _, h := range headers {
		switch normalizeCSVHeader(h) {
		case "max mode":
			hasMaxMode = true
		case "date":
			hasDate = true
		case "model":
			hasModel = true
		case "cost":
			hasCost = true
		}
	}
	if !hasDate || !hasModel || !hasCost {
		return cursorCSVVersionUnknown
	}
	if hasMaxMode {
		return cursorCSVV2
	}
	return cursorCSVV1
}

func normalizeCSVHeader(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	// Strip parenthetical qualifiers like "(w/ cache write)" so callers can
	// match on the leading concept. We still index by exact normalized header
	// in headerIndex below, but this helper is reused for the v2 sentinel.
	if idx := strings.Index(h, "("); idx > 0 {
		h = strings.TrimSpace(h[:idx])
	}
	return h
}

// parseCursorCSVFile reads a Cursor CSV export from disk and returns parsed
// records together with the detected schema version. Empty files yield no
// records and no error. Malformed headers return an error.
func parseCursorCSVFile(path string) ([]cursorCSVRecord, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, cursorCSVVersionUnknown, fmt.Errorf("cursor: open csv: %w", err)
	}
	defer f.Close()
	return parseCursorCSV(f)
}

// parseCursorCSV reads from an io.Reader (split out for testability).
func parseCursorCSV(r io.Reader) ([]cursorCSVRecord, int, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1 // tolerate ragged rows

	headers, err := reader.Read()
	if err == io.EOF {
		// Empty file.
		return nil, cursorCSVVersionUnknown, nil
	}
	if err != nil {
		return nil, cursorCSVVersionUnknown, fmt.Errorf("cursor: read csv header: %w", err)
	}
	version := detectCursorCSVVersion(headers)
	if version == cursorCSVVersionUnknown {
		return nil, version, fmt.Errorf("cursor: unrecognized csv header: %v", headers)
	}

	idx := buildCursorCSVHeaderIndex(headers)
	var out []cursorCSVRecord
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, version, fmt.Errorf("cursor: read csv row: %w", err)
		}
		rec, ok := buildCursorCSVRecord(row, idx)
		if !ok {
			continue
		}
		out = append(out, rec)
	}
	return out, version, nil
}

// cursorCSVHeaderIndex maps logical fields to their column index. -1 means
// absent.
type cursorCSVHeaderIndex struct {
	date              int
	user              int
	kind              int
	model             int
	maxMode           int
	inputWithCache    int
	inputWithoutCache int
	cacheRead         int
	outputTokens      int
	totalTokens       int
	cost              int
}

func buildCursorCSVHeaderIndex(headers []string) cursorCSVHeaderIndex {
	idx := cursorCSVHeaderIndex{
		date: -1, user: -1, kind: -1, model: -1, maxMode: -1,
		inputWithCache: -1, inputWithoutCache: -1, cacheRead: -1,
		outputTokens: -1, totalTokens: -1, cost: -1,
	}
	for i, raw := range headers {
		h := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case h == "date":
			idx.date = i
		case h == "user":
			idx.user = i
		case h == "kind":
			idx.kind = i
		case h == "model":
			idx.model = i
		case h == "max mode":
			idx.maxMode = i
		case strings.HasPrefix(h, "input (w/ cache"), strings.HasPrefix(h, "input (w/cache"):
			idx.inputWithCache = i
		case strings.HasPrefix(h, "input (w/o cache"), strings.HasPrefix(h, "input (w/o-cache"):
			idx.inputWithoutCache = i
		case strings.HasPrefix(h, "cache read"):
			idx.cacheRead = i
		case strings.HasPrefix(h, "output"):
			idx.outputTokens = i
		case strings.HasPrefix(h, "total"):
			idx.totalTokens = i
		case h == "cost", strings.HasPrefix(h, "cost to you"):
			idx.cost = i
		}
	}
	return idx
}

func buildCursorCSVRecord(row []string, idx cursorCSVHeaderIndex) (cursorCSVRecord, bool) {
	get := func(i int) string {
		if i < 0 || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	rec := cursorCSVRecord{
		User:              get(idx.user),
		Kind:              get(idx.kind),
		Model:             get(idx.model),
		MaxMode:           parseCursorCSVBool(get(idx.maxMode)),
		InputWithCache:    parseCursorCSVInt(get(idx.inputWithCache)),
		InputWithoutCache: parseCursorCSVInt(get(idx.inputWithoutCache)),
		CacheRead:         parseCursorCSVInt(get(idx.cacheRead)),
		OutputTokens:      parseCursorCSVInt(get(idx.outputTokens)),
		TotalTokens:       parseCursorCSVInt(get(idx.totalTokens)),
		Cost:              parseCursorCSVCost(get(idx.cost)),
	}
	if dateStr := get(idx.date); dateStr != "" {
		if t, ok := parseCursorCSVDate(dateStr); ok {
			rec.Date = t
		}
	}
	if rec.Model == "" && rec.TotalTokens == 0 && rec.Cost == 0 {
		return rec, false
	}
	return rec, true
}

func parseCursorCSVInt(s string) int64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	if s == "" || s == "-" || s == "N/A" {
		return 0
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func parseCursorCSVBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "1", "on":
		return true
	}
	return false
}

// parseCursorCSVCost handles "Included", "-", "NaN", and locale-formatted
// dollar amounts. Returns 0 on any unparseable input.
func parseCursorCSVCost(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	lower := strings.ToLower(s)
	if lower == "included" || lower == "-" || lower == "nan" || lower == "n/a" {
		return 0
	}
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// parseCursorCSVDate accepts a few formats Cursor has emitted in the wild:
// RFC3339, "2006-01-02 15:04:05", and bare dates.
func parseCursorCSVDate(s string) (time.Time, bool) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
		"01/02/2006",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// applyCursorCSVToSnapshot aggregates CSV records into snapshot metrics.
// Emits all-time totals plus per-model cost/requests metrics, mirroring the
// shape produced by readTrackingDB so the merge surface stays consistent.
func applyCursorCSVToSnapshot(records []cursorCSVRecord, snap *core.UsageSnapshot) {
	if len(records) == 0 || snap == nil {
		return
	}
	var totalCost float64
	var totalRequests int64
	var totalInput, totalOutput, totalCacheRead int64
	perModelCost := make(map[string]float64)
	perModelRequests := make(map[string]int64)

	for _, r := range records {
		totalCost += r.Cost
		totalRequests++
		totalInput += r.InputWithCache + r.InputWithoutCache
		totalOutput += r.OutputTokens
		totalCacheRead += r.CacheRead
		if r.Model != "" {
			perModelCost[r.Model] += r.Cost
			perModelRequests[r.Model]++
		}
	}

	costPtr := totalCost
	reqPtr := float64(totalRequests)
	snap.Metrics["composer_cost"] = core.Metric{Used: &costPtr, Unit: "USD", Window: "all-time"}
	snap.Metrics["composer_requests"] = core.Metric{Used: &reqPtr, Unit: "requests", Window: "all-time"}
	if totalInput > 0 {
		v := float64(totalInput)
		snap.Metrics["composer_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "all-time"}
	}
	if totalOutput > 0 {
		v := float64(totalOutput)
		snap.Metrics["composer_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "all-time"}
	}
	if totalCacheRead > 0 {
		v := float64(totalCacheRead)
		snap.Metrics["composer_cache_read_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "all-time"}
	}

	for model, cost := range perModelCost {
		key := "model_" + sanitizeCursorMetricName(model) + "_cost"
		c := cost
		snap.Metrics[key] = core.Metric{Used: &c, Unit: "USD", Window: "all-time"}
	}
	for model, reqs := range perModelRequests {
		key := "model_" + sanitizeCursorMetricName(model) + "_requests"
		r := float64(reqs)
		snap.Metrics[key] = core.Metric{Used: &r, Unit: "requests", Window: "all-time"}
	}

	if snap.Raw == nil {
		snap.Raw = make(map[string]string)
	}
	snap.Raw["csv_record_count"] = strconv.Itoa(len(records))
}

func sanitizeCursorMetricName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, "/", "_")
	return name
}
