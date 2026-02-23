package main

import (
	"context"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

const (
	defaultCodexSessionsDir     = "~/.codex/sessions"
	defaultClaudeProjectsDir    = "~/.claude/projects"
	defaultClaudeProjectsAltDir = "~/.config/claude/projects"
	defaultOpenCodeDBPath       = "~/.local/share/opencode/opencode.db"
)

func defaultTelemetryOptionsForSource(sourceSystem string) shared.TelemetryCollectOptions {
	return telemetryOptionsForSource(
		sourceSystem,
		defaultCodexSessionsDir,
		defaultClaudeProjectsDir,
		defaultClaudeProjectsAltDir,
		nil,
		"",
		defaultOpenCodeDBPath,
	)
}

func telemetryOptionsForSource(
	sourceSystem string,
	codexSessions string,
	claudeProjects string,
	claudeProjectsAlt string,
	opencodeEventsDirs []string,
	opencodeEventsFile string,
	opencodeDB string,
) shared.TelemetryCollectOptions {
	opts := shared.TelemetryCollectOptions{
		Paths:     map[string]string{},
		PathLists: map[string][]string{},
	}

	switch sourceSystem {
	case "codex":
		opts.Paths["sessions_dir"] = codexSessions
	case "claude_code":
		opts.Paths["projects_dir"] = claudeProjects
		opts.Paths["alt_projects_dir"] = claudeProjectsAlt
	case "opencode":
		opts.Paths["events_file"] = opencodeEventsFile
		opts.Paths["db_path"] = opencodeDB
		opts.PathLists["events_dirs"] = opencodeEventsDirs
	}
	return opts
}

func flushInBatches(ctx context.Context, pipeline *telemetry.Pipeline, maxTotal int) (telemetry.FlushResult, []string) {
	var (
		accum    telemetry.FlushResult
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

		// Stop when there is nothing left to process or no forward progress can be made.
		if batch.Processed == 0 || (batch.Ingested == 0 && batch.Deduped == 0) {
			break
		}
	}

	return accum, warnings
}
