package telemetry

import (
	"context"
	"fmt"
	"time"
)

const defaultAutoCollectorInterval = 30 * time.Second

type AutoCollector struct {
	collectors []Collector
	pipeline   *Pipeline
	maxFlush   int
}

type AutoCollectResult struct {
	Collected    int
	CollectedBy  map[string]int
	Enqueued     int
	Flush        FlushResult
	CollectorErr []string
	FlushErr     []string
}

func NewAutoCollector(collectors []Collector, pipeline *Pipeline, maxFlush int) *AutoCollector {
	if maxFlush < 0 {
		maxFlush = 0
	}
	out := make([]Collector, 0, len(collectors))
	for _, collector := range collectors {
		if collector == nil {
			continue
		}
		out = append(out, collector)
	}
	return &AutoCollector{
		collectors: out,
		pipeline:   pipeline,
		maxFlush:   maxFlush,
	}
}

func (c *AutoCollector) CollectAndFlush(ctx context.Context) (AutoCollectResult, error) {
	if c == nil || c.pipeline == nil {
		return AutoCollectResult{}, fmt.Errorf("telemetry auto collector: pipeline is not configured")
	}

	result := AutoCollectResult{
		CollectedBy: make(map[string]int, len(c.collectors)),
	}

	for _, collector := range c.collectors {
		reqs, err := collector.Collect(ctx)
		if err != nil {
			result.CollectorErr = append(result.CollectorErr, fmt.Sprintf("%s: %v", collector.Name(), err))
			continue
		}
		result.Collected += len(reqs)
		result.CollectedBy[collector.Name()] += len(reqs)
		if len(reqs) == 0 {
			continue
		}

		enqueued, err := c.pipeline.EnqueueRequests(reqs)
		if err != nil {
			return result, fmt.Errorf("telemetry auto collector: enqueue %s events: %w", collector.Name(), err)
		}
		result.Enqueued += enqueued
	}

	flush, warnings := flushPipelineInBatches(ctx, c.pipeline, c.maxFlush)
	result.Flush = flush
	result.FlushErr = append(result.FlushErr, warnings...)
	return result, nil
}

func (c *AutoCollector) Run(ctx context.Context, interval time.Duration, onCycle func(AutoCollectResult, error)) {
	if interval <= 0 {
		interval = defaultAutoCollectorInterval
	}

	runCycle := func() {
		res, err := c.CollectAndFlush(ctx)
		if onCycle != nil {
			onCycle(res, err)
		}
	}

	runCycle()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCycle()
		}
	}
}

func flushPipelineInBatches(ctx context.Context, pipeline *Pipeline, maxTotal int) (FlushResult, []string) {
	var (
		accum    FlushResult
		warnings []string
	)

	remaining := maxTotal
	for {
		batchLimit := 10000
		if maxTotal > 0 {
			if remaining <= 0 {
				break
			}
			if remaining < batchLimit {
				batchLimit = remaining
			}
		}

		batch, err := pipeline.Flush(ctx, batchLimit)
		accum.Processed += batch.Processed
		accum.Ingested += batch.Ingested
		accum.Deduped += batch.Deduped
		accum.Failed += batch.Failed
		if err != nil {
			warnings = append(warnings, err.Error())
		}

		if maxTotal > 0 {
			remaining -= batch.Processed
		}

		// Nothing left in spool or no forward progress.
		if batch.Processed == 0 || (batch.Ingested == 0 && batch.Deduped == 0) {
			break
		}
	}

	return accum, warnings
}
