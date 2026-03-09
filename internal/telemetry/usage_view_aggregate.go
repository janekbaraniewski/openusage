package telemetry

import (
	"context"
	"database/sql"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func loadMaterializedUsageAgg(ctx context.Context, db *sql.DB, filter usageFilter, agg *telemetryUsageAgg) error {
	trace := func(label string) func() {
		start := time.Now()
		return func() { core.Tracef("[usage_view_perf]   %s: %dms", label, time.Since(start).Milliseconds()) }
	}

	done := trace("queryModelAgg")
	models, err := queryModelAgg(ctx, db, filter)
	done()
	if err != nil {
		return err
	}
	done = trace("querySourceAgg")
	sources, err := querySourceAgg(ctx, db, filter)
	done()
	if err != nil {
		return err
	}
	done = trace("queryProjectAgg")
	projects, err := queryProjectAgg(ctx, db, filter)
	done()
	if err != nil {
		return err
	}
	done = trace("queryToolAgg")
	tools, err := queryToolAgg(ctx, db, filter)
	done()
	if err != nil {
		return err
	}
	done = trace("queryProviderAgg")
	providers, err := queryProviderAgg(ctx, db, filter)
	done()
	if err != nil {
		return err
	}
	done = trace("queryLanguageAgg")
	languages, err := queryLanguageAgg(ctx, db, filter)
	done()
	if err != nil {
		return err
	}
	done = trace("queryActivityAgg")
	activity, err := queryActivityAgg(ctx, db, filter)
	done()
	if err != nil {
		return err
	}
	done = trace("queryCodeStatsAgg")
	codeStats, err := queryCodeStatsAgg(ctx, db, filter)
	done()
	if err != nil {
		return err
	}
	done = trace("queryDailyTotals")
	daily, err := queryDailyTotals(ctx, db, filter)
	done()
	if err != nil {
		return err
	}
	done = trace("queryDailyByDimension(model)")
	modelDaily, err := queryDailyByDimension(ctx, db, filter, "model")
	done()
	if err != nil {
		return err
	}
	done = trace("queryDailyByDimension(source)")
	sourceDaily, err := queryDailyByDimension(ctx, db, filter, "source")
	done()
	if err != nil {
		return err
	}
	done = trace("queryDailyByDimension(project)")
	projectDaily, err := queryDailyByDimension(ctx, db, filter, "project")
	done()
	if err != nil {
		return err
	}
	done = trace("queryDailyByDimension(client)")
	clientDaily, err := queryDailyByDimension(ctx, db, filter, "client")
	done()
	if err != nil {
		return err
	}
	done = trace("queryDailyClientTokens")
	clientTokens, err := queryDailyClientTokens(ctx, db, filter)
	done()
	if err != nil {
		return err
	}

	agg.Models = models
	agg.Providers = providers
	agg.Sources = sources
	agg.Projects = projects
	agg.Tools = tools
	agg.MCPServers = buildMCPAgg(tools)
	agg.Languages = languages
	agg.Activity = activity
	agg.CodeStats = codeStats
	agg.Daily = daily
	agg.ModelDaily = modelDaily
	agg.SourceDaily = sourceDaily
	agg.ProjectDaily = projectDaily
	agg.ClientDaily = clientDaily
	agg.ClientTokens = clientTokens
	return nil
}
