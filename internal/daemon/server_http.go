package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/integrations"
	"github.com/janekbaraniewski/openusage/internal/version"
)

func (s *Service) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":                 "ok",
		"daemon_version":         strings.TrimSpace(version.Version),
		"api_version":            APIVersion,
		"integration_version":    integrations.IntegrationVersion,
		"provider_registry_hash": ProviderRegistryHash(),
	})
}

func (s *Service) handleHook(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sourceName := strings.TrimPrefix(strings.TrimSpace(r.URL.Path), "/v1/hook/")
	sourceName = strings.TrimSpace(strings.Trim(sourceName, "/"))
	if sourceName == "" {
		writeJSONError(w, http.StatusBadRequest, "missing hook source")
		return
	}

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read payload failed")
		return
	}
	if len(strings.TrimSpace(string(payload))) == 0 {
		writeJSONError(w, http.StatusBadRequest, "empty payload")
		return
	}

	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	parsed, err := ParseHookRequests(sourceName, accountID, payload)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(parsed.Requests) == 0 {
		writeJSON(w, http.StatusOK, HookResponse{Source: sourceName, Warnings: parsed.Warnings})
		return
	}

	tally, _ := s.ingestBatch(r.Context(), parsed.Requests)
	if tally.ingested > 0 {
		s.markDataIngested()
	}
	warnings := append([]string(nil), parsed.Warnings...)
	if tally.failed > 0 {
		warnings = append(warnings, fmt.Sprintf("%d ingest failures", tally.failed))
	}

	writeJSON(w, http.StatusOK, HookResponse{
		Source:    sourceName,
		Enqueued:  len(parsed.Requests),
		Processed: tally.processed,
		Ingested:  tally.ingested,
		Deduped:   tally.deduped,
		Failed:    tally.failed,
		Warnings:  warnings,
	})

	durationMs := time.Since(started).Milliseconds()
	logLevel := "hook_ingest"
	shouldLog := tally.failed > 0 || s.shouldLog("hook_ingest_"+sourceName, 3*time.Second)
	if !shouldLog {
		return
	}
	if tally.failed > 0 {
		s.warnf(logLevel,
			"source=%s account_id=%q duration_ms=%d enqueued=%d processed=%d ingested=%d deduped=%d failed=%d",
			sourceName, parsed.EffectiveAccountID, durationMs,
			len(parsed.Requests), tally.processed, tally.ingested, tally.deduped, tally.failed,
		)
		return
	}
	s.infof(logLevel,
		"source=%s account_id=%q duration_ms=%d enqueued=%d processed=%d ingested=%d deduped=%d failed=%d",
		sourceName, parsed.EffectiveAccountID, durationMs,
		len(parsed.Requests), tally.processed, tally.ingested, tally.deduped, tally.failed,
	)
}

func (s *Service) handleReadModel(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ReadModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("decode read-model request: %v", err))
		return
	}

	if len(req.Accounts) == 0 {
		// Clients without their own account list (TUI, tmux, statusline)
		// get the daemon's cached account set; resolving accounts here would
		// run auto-detection (about a second) on every request.
		configReq, _, configErr := s.cachedReadModelRequest(r.Context())
		if configErr != nil || len(configReq.Accounts) == 0 {
			writeJSON(w, http.StatusOK, ReadModelResponse{Snapshots: map[string]core.UsageSnapshot{}})
			return
		}
		if req.TimeWindow != "" {
			configReq.TimeWindow = req.TimeWindow
		}
		if len(req.ProviderLinks) > 0 {
			configReq.ProviderLinks = req.ProviderLinks
		}
		req = configReq
	}

	cacheKey := ReadModelRequestKey(req)
	if cached, cachedAt, cachedVersion, ok := s.rmCache.get(cacheKey); ok {
		core.Tracef("[read_model] cache hit key=%s age=%s providers=%d", cacheKey, time.Since(cachedAt).Round(time.Millisecond), len(cached))
		for id, snap := range cached {
			core.Tracef("[read_model]   %s: %d metrics", id, len(snap.Metrics))
		}
		writeJSON(w, http.StatusOK, ReadModelResponse{Snapshots: cached})
		// Refresh opportunistically only when the data version advanced after
		// this entry was built. The staleness floor still debounces bursts.
		if shouldRefreshCachedReadModel(cachedAt, cachedVersion, s.dataVersion.Load(), time.Now()) {
			s.refreshReadModelCacheAsync(s.serviceContext(r.Context()), cacheKey, req, 60*time.Second)
		}
		return
	}

	// Cache miss: compute in the background (detached from this request so
	// a short client deadline never throws the work away) and wait only as
	// long as the client's budget allows. Whoever asks next is served from
	// cache.
	s.refreshReadModelCacheAsync(s.serviceContext(r.Context()), cacheKey, req, 60*time.Second)
	if done := s.rmCache.refreshDone(cacheKey); done != nil {
		timer := time.NewTimer(readModelBudget(r))
		select {
		case <-done:
		case <-timer.C:
		case <-r.Context().Done():
		}
		timer.Stop()
	}
	if cached, _, _, ok := s.rmCache.get(cacheKey); ok {
		writeJSON(w, http.StatusOK, ReadModelResponse{Snapshots: cached})
		return
	}

	// Re-arm the data-ingested flag so the periodic refresh loop tries
	// again instead of sticking on stale empty templates. Without this,
	// a single failed compute can leave the read-model cache permanently
	// empty until the next ingest event.
	s.markDataIngested()
	snapshots := ReadModelTemplatesFromRequest(req, s.disabledAccounts())
	writeJSON(w, http.StatusOK, ReadModelResponse{Snapshots: snapshots, Pending: true})
	durationMs := time.Since(started).Milliseconds()
	if durationMs >= 1200 && s.shouldLog("read_model_slow", 30*time.Second) {
		s.infof(
			"read_model_slow",
			"duration_ms=%d requested_accounts=%d returned_snapshots=%d provider_links=%d",
			durationMs,
			len(req.Accounts),
			len(snapshots),
			len(req.ProviderLinks),
		)
	}
}

// readModelBudgetHeader carries how long the client will wait for a
// read-model answer, in milliseconds. tmux renders have about 800ms.
const readModelBudgetHeader = "X-Openusage-Budget-Ms"

// maxReadModelBudget caps how long a cache miss holds the request open.
const maxReadModelBudget = 5 * time.Second

// readModelBudget returns how long a cache miss may wait for the compute.
// Clients that send no budget get the previous fixed 5s.
func readModelBudget(r *http.Request) time.Duration {
	raw := strings.TrimSpace(r.Header.Get(readModelBudgetHeader))
	if raw == "" {
		return maxReadModelBudget
	}
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || ms < 0 {
		return maxReadModelBudget
	}
	return min(time.Duration(ms)*time.Millisecond, maxReadModelBudget)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
