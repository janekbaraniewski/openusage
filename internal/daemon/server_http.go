package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
		configReq, configErr := BuildReadModelRequestFromConfig()
		if configErr != nil || len(configReq.Accounts) == 0 {
			writeJSON(w, http.StatusOK, ReadModelResponse{Snapshots: map[string]core.UsageSnapshot{}})
			return
		}
		req = configReq
	}

	cacheKey := ReadModelRequestKey(req)
	if cached, cachedAt, ok := s.rmCache.get(cacheKey); ok {
		core.Tracef("[read_model] cache hit key=%s age=%s providers=%d", cacheKey, time.Since(cachedAt).Round(time.Millisecond), len(cached))
		for id, snap := range cached {
			core.Tracef("[read_model]   %s: %d metrics", id, len(snap.Metrics))
		}
		writeJSON(w, http.StatusOK, ReadModelResponse{Snapshots: cached})
		if time.Since(cachedAt) > 2*time.Second {
			s.refreshReadModelCacheAsync(s.serviceContext(r.Context()), cacheKey, req, 60*time.Second)
		}
		return
	}

	computeCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	snapshots, err := s.computeReadModel(computeCtx, req)
	cancel()
	if err == nil && len(snapshots) > 0 {
		s.rmCache.set(cacheKey, snapshots)
		writeJSON(w, http.StatusOK, ReadModelResponse{Snapshots: snapshots})
		return
	}

	if err != nil && s.shouldLog("read_model_cache_miss_compute_error", 8*time.Second) {
		s.warnf("read_model_cache_miss_compute_error", "error=%v", err)
	}

	s.refreshReadModelCacheAsync(s.serviceContext(r.Context()), cacheKey, req, 60*time.Second)
	snapshots = ReadModelTemplatesFromRequest(req, DisabledAccountsFromConfig())
	writeJSON(w, http.StatusOK, ReadModelResponse{Snapshots: snapshots})
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
