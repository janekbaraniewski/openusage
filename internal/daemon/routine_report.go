package daemon

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

const routineReportMaxBytes = 1 << 20
const routineReportMaxTokens = int64(1_000_000_000_000_000)

type routineUsageReport struct {
	RoutineID  string               `json:"routine_id"`
	RunID      string               `json:"run_id"`
	OccurredAt time.Time            `json:"occurred_at"`
	Models     []routineModelReport `json:"models"`
}

type routineModelReport struct {
	Model               string `json:"model"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int64  `json:"cache_creation_tokens,omitempty"`
}

func (s *Service) handleRoutineReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if provided == r.Header.Get("Authorization") || len(provided) != len(s.cfg.RoutineToken) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.cfg.RoutineToken)) != 1 {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, routineReportMaxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var report routineUsageReport
	if err := dec.Decode(&report); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid report JSON")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "request must contain one JSON object")
		return
	}
	reqs, err := routineReportRequests(report, s.cfg.RoutineAccountID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	tally, _ := s.ingestBatch(r.Context(), reqs)
	if tally.failed > 0 {
		writeJSONError(w, http.StatusInternalServerError, "failed to ingest report")
		return
	}
	if tally.ingested > 0 {
		s.markDataIngested()
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": tally.ingested + tally.deduped, "deduped": tally.deduped})
}

func routineReportRequests(report routineUsageReport, accountID string) ([]telemetry.IngestRequest, error) {
	report.RoutineID = strings.TrimSpace(report.RoutineID)
	report.RunID = strings.TrimSpace(report.RunID)
	if report.RoutineID == "" || report.RunID == "" {
		return nil, fmt.Errorf("routine_id and run_id are required")
	}
	if report.OccurredAt.IsZero() || len(report.Models) == 0 || len(report.Models) > 100 {
		return nil, fmt.Errorf("occurred_at and 1-100 model entries are required")
	}
	if accountID == "" {
		return nil, fmt.Errorf("no Claude Code account is configured")
	}
	reqs := make([]telemetry.IngestRequest, 0, len(report.Models))
	seen := make(map[string]struct{}, len(report.Models))
	for _, item := range report.Models {
		item.Model = strings.TrimSpace(item.Model)
		if item.Model == "" || item.InputTokens < 0 || item.OutputTokens < 0 || item.CacheReadTokens < 0 || item.CacheCreationTokens < 0 ||
			item.InputTokens > routineReportMaxTokens || item.OutputTokens > routineReportMaxTokens || item.CacheReadTokens > routineReportMaxTokens || item.CacheCreationTokens > routineReportMaxTokens {
			return nil, fmt.Errorf("model names and token counts must be valid and nonnegative")
		}
		if _, ok := seen[item.Model]; ok {
			return nil, fmt.Errorf("duplicate model %q", item.Model)
		}
		seen[item.Model] = struct{}{}
		id := report.RoutineID + "/" + report.RunID + "/" + item.Model
		in, out, cacheRead, cacheWrite, requests := item.InputTokens, item.OutputTokens, item.CacheReadTokens, item.CacheCreationTokens, int64(1)
		total := in + out + cacheRead + cacheWrite
		reqs = append(reqs, telemetry.IngestRequest{
			SourceSystem: "claude_code_routine_cloud", SourceChannel: telemetry.SourceChannelAPI,
			SourceSchemaVersion: "v1", OccurredAt: report.OccurredAt.UTC(),
			SessionID: report.RoutineID, TurnID: report.RunID, MessageID: id,
			ProviderID: "claude_code", AccountID: accountID, AgentName: "claude_code_routine_cloud",
			EventType: telemetry.EventTypeMessageUsage, ModelRaw: item.Model,
			TokenUsage: core.TokenUsage{InputTokens: &in, OutputTokens: &out, CacheReadTokens: &cacheRead, CacheWriteTokens: &cacheWrite, TotalTokens: &total, Requests: &requests},
			Payload:    map[string]string{"routine_id": report.RoutineID, "run_id": report.RunID},
		})
	}
	return reqs, nil
}
