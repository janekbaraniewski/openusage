package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func ingestParsedHookLocally(
	ctx context.Context,
	parsed HookParseResult,
	dbPath string,
	spoolDir string,
	spoolOnly bool,
) (HookResponse, error) {
	resp := HookResponse{
		Source:   parsed.SourceName,
		Enqueued: len(parsed.Requests),
		Warnings: append([]string(nil), parsed.Warnings...),
	}
	if len(parsed.Requests) == 0 {
		return resp, nil
	}

	if strings.TrimSpace(dbPath) == "" {
		resolved, resolveErr := telemetry.DefaultDBPath()
		if resolveErr != nil {
			return HookResponse{}, fmt.Errorf("resolve telemetry db path: %w", resolveErr)
		}
		dbPath = resolved
	}
	if strings.TrimSpace(spoolDir) == "" {
		resolved, resolveErr := telemetry.DefaultSpoolDir()
		if resolveErr != nil {
			return HookResponse{}, fmt.Errorf("resolve telemetry spool dir: %w", resolveErr)
		}
		spoolDir = resolved
	}

	store, err := telemetry.OpenStore(dbPath)
	if err != nil {
		return HookResponse{}, fmt.Errorf("open telemetry store: %w", err)
	}
	defer store.Close()

	pipeline := telemetry.NewPipeline(store, telemetry.NewSpool(spoolDir))
	if spoolOnly {
		enqueued, enqueueErr := pipeline.EnqueueRequests(parsed.Requests)
		if enqueueErr != nil {
			return HookResponse{}, fmt.Errorf("enqueue to telemetry spool: %w", enqueueErr)
		}
		resp.Enqueued = enqueued
		return resp, nil
	}

	retries := make([]telemetry.IngestRequest, 0, len(parsed.Requests))
	var firstIngestErr error
	for _, req := range parsed.Requests {
		resp.Processed++
		result, ingestErr := store.Ingest(ctx, req)
		if ingestErr != nil {
			if firstIngestErr == nil {
				firstIngestErr = ingestErr
			}
			retries = append(retries, req)
			continue
		}
		if result.Deduped {
			resp.Deduped++
		} else {
			resp.Ingested++
		}
	}

	if len(retries) == 0 {
		return resp, nil
	}
	if firstIngestErr != nil {
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("direct ingest failed for %d event(s): %v", len(retries), firstIngestErr))
	}

	enqueued, enqueueErr := pipeline.EnqueueRequests(retries)
	if enqueueErr != nil {
		resp.Failed += len(retries)
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("retry enqueue failed: %v", enqueueErr))
		return resp, nil
	}
	flush, warnings := FlushInBatches(ctx, pipeline, enqueued)
	resp.Processed += flush.Processed
	resp.Ingested += flush.Ingested
	resp.Deduped += flush.Deduped
	resp.Failed += flush.Failed
	resp.Warnings = append(resp.Warnings, warnings...)

	if remaining := len(retries) - flush.Processed; remaining > 0 {
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("%d event(s) remain queued in spool", remaining))
	}
	return resp, nil
}
