package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/active"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type activeComputation struct {
	selection active.Selection
	input     active.SelectInput
	byKey     map[string]core.UsageSnapshot
}

// computeActive resolves the current active provider and narrates its quota
// position from daemon telemetry and the current read model.
func (s *Service) computeActive(ctx context.Context) (active.Selection, error) {
	computed, err := s.computeActiveDetails(ctx)
	if err != nil {
		return active.Selection{}, err
	}
	return computed.selection, nil
}

// computeActiveDetails builds the exact selector input used for the public
// active response. The explainer consumes this same input, so diagnostics do
// not drift into a second selection algorithm.
func (s *Service) computeActiveDetails(ctx context.Context) (activeComputation, error) {
	if s == nil || s.store == nil {
		return activeComputation{}, fmt.Errorf("daemon: telemetry store unavailable")
	}

	lastEvents, err := s.store.LastEventTimes(ctx)
	if err != nil {
		return activeComputation{}, fmt.Errorf("daemon: reading last event times: %w", err)
	}
	raw, _, err := s.store.MetaGet(ctx, active.PinMetaKey)
	if err != nil {
		return activeComputation{}, fmt.Errorf("daemon: reading pin state: %w", err)
	}
	pinState, err := active.DecodePinState(raw)
	if err != nil {
		return activeComputation{}, err
	}
	pinnedKey, pinAlive := active.LivePin(pinState, lastEvents)
	if !pinAlive && strings.TrimSpace(pinState.Key) != "" {
		if err := s.store.MetaClearIfValue(ctx, active.PinMetaKey, raw); err != nil {
			return activeComputation{}, fmt.Errorf("daemon: clearing released pin: %w", err)
		}
	}

	req, err := BuildReadModelRequestFromConfig()
	if err != nil {
		return activeComputation{}, fmt.Errorf("daemon: building read-model request: %w", err)
	}
	snapshots, err := s.computeReadModel(ctx, req)
	if err != nil {
		return activeComputation{}, fmt.Errorf("daemon: reading snapshots: %w", err)
	}

	input, byKey := buildActiveSelectionInput(snapshots, lastEvents, pinnedKey)
	if pinnedKey != "" && !activeInputHasCandidate(input, pinnedKey) {
		// A configured provider can disappear independently of telemetry (for
		// example, an account was removed from settings). Treat that as a
		// released pin rather than keeping stale state forever.
		if err := s.store.MetaClearIfValue(ctx, active.PinMetaKey, raw); err != nil {
			return activeComputation{}, fmt.Errorf("daemon: clearing missing pin: %w", err)
		}
		pinnedKey = ""
		input.PinnedKey = ""
	}
	return activeComputation{
		selection: buildActiveSelectionFromInput(input, byKey, s.now().UTC()),
		input:     input,
		byKey:     byKey,
	}, nil
}

func activeInputHasCandidate(input active.SelectInput, key string) bool {
	for _, candidate := range input.Candidates {
		if candidate.Key == key {
			return true
		}
	}
	return false
}

// buildActiveSelection keeps ranking independent from storage and configuration
// loading, making the core daemon decision easy to test and explain later.
func buildActiveSelection(
	snapshots map[string]core.UsageSnapshot,
	lastEvents map[string]time.Time,
	pinnedKey string,
	now time.Time,
) active.Selection {
	input, byKey := buildActiveSelectionInput(snapshots, lastEvents, pinnedKey)
	return buildActiveSelectionFromInput(input, byKey, now)
}

func buildActiveSelectionInput(
	snapshots map[string]core.UsageSnapshot,
	lastEvents map[string]time.Time,
	pinnedKey string,
) (active.SelectInput, map[string]core.UsageSnapshot) {
	candidates := make([]active.Candidate, 0, len(snapshots))
	byKey := make(map[string]core.UsageSnapshot, len(snapshots))
	for _, snap := range snapshots {
		providerID := strings.TrimSpace(snap.ProviderID)
		if providerID == "" {
			continue
		}
		accountID := strings.TrimSpace(snap.AccountID)
		if accountID == "" {
			accountID = "default"
		}
		key := providerID + ":" + accountID
		candidate := active.Candidate{
			Key:        key,
			ProviderID: providerID,
			AccountID:  accountID,
			Display:    active.DisplayName(providerID),
		}
		if ts, ok := lastEvents[key]; ok {
			t := ts
			candidate.LastEventAt = &t
		}
		candidates = append(candidates, candidate)
		byKey[key] = snap
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Key < candidates[j].Key })
	return active.SelectInput{
		Candidates:    candidates,
		PinnedKey:     pinnedKey,
		PriorityOrder: active.DefaultPriorityOrder,
	}, byKey
}

func buildActiveSelectionFromInput(
	input active.SelectInput,
	byKey map[string]core.UsageSnapshot,
	now time.Time,
) active.Selection {
	winner, source, found := active.Select(input)
	if !found {
		return active.Selection{
			Severity: active.SeverityUnknown,
			Status:   "no_data",
		}
	}

	facts := active.BuildFacts(byKey[winner.Key], now)
	label, severity := active.Narrate(facts, now)
	return active.Selection{
		Selected: winner.Key,
		Display:  winner.Display,
		Pinned:   source == "pinned",
		Severity: severity,
		Label:    label,
		Facts:    facts,
		Source:   source,
		Status:   "ok",
	}
}

// setPin stores a pin, or clears it when key is empty.
func (s *Service) setPin(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		if err := s.store.MetaSet(ctx, active.PinMetaKey, ""); err != nil {
			return fmt.Errorf("daemon: clearing pin: %w", err)
		}
		return nil
	}
	encoded, err := (active.PinState{Key: key, PinnedAt: s.now().UTC()}).Encode()
	if err != nil {
		return err
	}
	if err := s.store.MetaSet(ctx, active.PinMetaKey, encoded); err != nil {
		return fmt.Errorf("daemon: storing pin: %w", err)
	}
	return nil
}

func (s *Service) handleActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sel, err := s.computeActive(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sel)
}

func (s *Service) handleActiveExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	computed, err := s.computeActiveDetails(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ActiveExplainResponse{
		Explanation: active.Explain(computed.input),
	})
}

func (s *Service) handleActiveList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	computed, err := s.computeActiveDetails(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status := computed.selection.Status
	if strings.TrimSpace(status) == "" {
		status = "no_data"
	}
	candidates := candidateSeverities(computed.input.Candidates, computed.byKey, s.now().UTC())
	writeJSON(w, http.StatusOK, active.CandidateList{
		Selected:   computed.selection.Selected,
		Pinned:     computed.input.PinnedKey,
		Status:     status,
		Candidates: candidates,
	})
}

func candidateSeverities(
	candidates []active.Candidate,
	byKey map[string]core.UsageSnapshot,
	now time.Time,
) []active.Candidate {
	decorated := make([]active.Candidate, len(candidates))
	copy(decorated, candidates)
	for i := range decorated {
		snap, ok := byKey[decorated[i].Key]
		if !ok {
			decorated[i].Severity = active.SeverityUnknown
			continue
		}
		facts := active.BuildFacts(snap, now)
		_, decorated[i].Severity = active.Narrate(facts, now)
	}
	return decorated
}

func (s *Service) handleActiveDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	computed, err := s.computeActiveDetails(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, buildActiveDetailResponse(computed.selection, computed.byKey, s.now().UTC()))
}

func (s *Service) handleActivePin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req ActivePinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "decode pin request: "+err.Error())
		return
	}
	if err := s.setPin(r.Context(), req.Key); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func buildActiveDetailResponse(
	selection active.Selection,
	byKey map[string]core.UsageSnapshot,
	now time.Time,
) active.DetailResponse {
	response := active.DetailResponse{
		Selection: selection,
		Rows:      make([]active.DetailRow, 0),
		Status:    selection.Status,
	}
	if selection.Status == "" {
		response.Status = "no_data"
	}

	snap, ok := byKey[selection.Selected]
	if !ok {
		return response
	}
	response.Message = snap.Message
	keys := core.SortedStringKeys(snap.Metrics)
	for _, key := range keys {
		if !core.IncludeDetailMetricKey(key) {
			continue
		}
		metric := snap.Metrics[key]
		if activeDetailIsCost(key, metric) {
			// The shell integrations deliberately never surface spend.
			continue
		}
		resetAt := metricResetAt(snap, key, metric)
		response.Rows = append(response.Rows, active.DetailRow{
			Name:      key,
			Display:   formatActiveMetricDisplay(metric, resetAt, now),
			Primary:   activeDetailIsPrimary(key),
			Limit:     metric.Limit,
			Remaining: metric.Remaining,
			Used:      metric.Used,
			Unit:      metric.Unit,
			Window:    metric.Window,
			ResetAt:   resetAt,
		})
	}
	return response
}

func metricResetAt(snap core.UsageSnapshot, name string, metric core.Metric) *time.Time {
	resetKey := strings.TrimSpace(metric.ResetKey)
	if resetKey == "" {
		resetKey = name
	}
	reset, ok := snap.Resets[resetKey]
	if !ok {
		return nil
	}
	reset = reset.UTC()
	return &reset
}

func formatActiveMetricDisplay(metric core.Metric, resetAt *time.Time, now time.Time) string {
	unit := strings.TrimSpace(metric.Unit)
	var value string
	switch {
	case metric.Limit != nil && metric.Used != nil:
		if unit == "%" || strings.EqualFold(unit, "percent") || strings.EqualFold(unit, "percentage") {
			value = fmt.Sprintf("%.0f%% / %.0f%%", *metric.Used, *metric.Limit)
		} else {
			value = formatActiveNumber(*metric.Used) + " / " + formatActiveNumber(*metric.Limit)
		}
	case metric.Limit != nil && metric.Remaining != nil:
		value = formatActiveNumber(*metric.Remaining) + " / " + formatActiveNumber(*metric.Limit) + " left"
	case metric.Used != nil:
		value = formatActiveNumber(*metric.Used)
	case metric.Remaining != nil:
		value = formatActiveNumber(*metric.Remaining) + " left"
	}
	if value == "" {
		return ""
	}
	if unit != "" && unit != "%" && !strings.Contains(value, unit) {
		value += " " + unit
	}
	if window := strings.TrimSpace(metric.Window); window != "" {
		value += " · " + window
	}
	if resetAt != nil {
		resetLabel := resetAt.Local().Format("Jan 2 15:04")
		if !resetAt.After(now) {
			resetLabel = "now"
		}
		value += " · reset " + resetLabel
	}
	return value
}

func formatActiveNumber(value float64) string {
	// Raw float64 arithmetic produces values like 115.45194054314815, which
	// are unreadable in a status-bar row. Whole numbers stay whole; anything
	// fractional is rounded to two places.
	if value == math.Trunc(value) {
		return strconv.FormatFloat(value, 'f', 0, 64)
	}
	return strconv.FormatFloat(value, 'f', 2, 64)
}

// activeDetailIsCost reports whether a metric expresses spend. The
// active-detail endpoint feeds status-bar popups, which by design never show
// money.
func activeDetailIsCost(key string, metric core.Metric) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if strings.Contains(lower, "cost") || strings.Contains(lower, "_usd") || strings.HasSuffix(lower, "usd") || strings.Contains(lower, "spend") {
		return true
	}
	unit := strings.ToLower(strings.TrimSpace(metric.Unit))
	return unit == "usd" || unit == "$" || strings.Contains(unit, "dollar")
}

// activeDetailPrimaryKeys are the metric-name fragments a compact consumer
// renders. Matching is by fragment so it holds across providers rather than
// hardcoding one vendor's metric names.
var activeDetailPrimaryKeys = []string{
	"rate_limit_primary",
	"plan_percent_used",
	"quota_runout_hours",
	"quota_burn_rate",
	"messages_today",
	"sessions_today",
	"tool_calls_today",
}

// activeDetailIsPrimary reports whether a row belongs in a compact popup.
func activeDetailIsPrimary(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, want := range activeDetailPrimaryKeys {
		if lower == want {
			return true
		}
	}
	return false
}
