package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Pipeline struct {
	store *Store
	spool *Spool
}

type FlushResult struct {
	Processed int
	Ingested  int
	Deduped   int
	Failed    int
}

func NewPipeline(store *Store, spool *Spool) *Pipeline {
	return &Pipeline{store: store, spool: spool}
}

func (p *Pipeline) EnqueueRequests(reqs []IngestRequest) (int, error) {
	if p == nil || p.spool == nil {
		return 0, fmt.Errorf("telemetry pipeline: spool is not configured")
	}
	count := 0
	for _, req := range reqs {
		payload, err := json.Marshal(req)
		if err != nil {
			return count, fmt.Errorf("telemetry pipeline: marshal request: %w", err)
		}
		if _, err := p.spool.Append(SpoolRecord{
			SourceSystem:  req.SourceSystem,
			SourceChannel: req.SourceChannel,
			Payload:       payload,
		}); err != nil {
			return count, fmt.Errorf("telemetry pipeline: append spool record: %w", err)
		}
		count++
	}
	return count, nil
}

func (p *Pipeline) Flush(ctx context.Context, limit int) (FlushResult, error) {
	if p == nil || p.spool == nil || p.store == nil {
		return FlushResult{}, fmt.Errorf("telemetry pipeline: store/spool is not configured")
	}

	pending, readErr := p.spool.ReadOldest(limit)
	result := FlushResult{Processed: len(pending)}

	for _, item := range pending {
		var req IngestRequest
		if err := json.Unmarshal(item.Record.Payload, &req); err != nil {
			result.Failed++
			_ = p.spool.MarkFailed(item.Path, truncateErr("decode request", err))
			continue
		}

		ingestResult, err := p.store.Ingest(ctx, req)
		if err != nil {
			result.Failed++
			_ = p.spool.MarkFailed(item.Path, truncateErr("ingest", err))
			continue
		}

		if ingestResult.Deduped {
			result.Deduped++
		} else {
			result.Ingested++
		}
		if err := p.spool.Ack(item.Path); err != nil {
			result.Failed++
			_ = p.spool.MarkFailed(item.Path, truncateErr("ack", err))
		}
	}

	if readErr != nil {
		return result, readErr
	}
	return result, nil
}

func truncateErr(prefix string, err error) string {
	if err == nil {
		return prefix
	}
	msg := strings.TrimSpace(err.Error())
	if len(msg) > 400 {
		msg = msg[:397] + "..."
	}
	if prefix == "" {
		return msg
	}
	return prefix + ": " + msg
}
