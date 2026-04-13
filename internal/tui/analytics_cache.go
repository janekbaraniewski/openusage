package tui

import (
	"strconv"
	"strings"
)

type analyticsRenderCacheEntry struct {
	key     string
	hasData bool
	content string
}

func (m *Model) invalidateAnalyticsCache() {
	m.analyticsCache = analyticsRenderCacheEntry{}
}

func (m *Model) cachedAnalyticsPageContent(w int) (string, bool) {
	// Build a cache key from all state that affects rendering
	expandKey := ""
	for k, v := range m.analyticsModelExpand {
		if v {
			expandKey += k + ","
		}
	}
	key := strings.Join([]string{
		strconv.Itoa(w),
		strconv.Itoa(m.analyticsSortBy),
		strconv.Itoa(m.analyticsTab),
		strconv.Itoa(m.analyticsModelCursor),
		m.analyticsFilter.text,
		string(m.timeWindow),
		expandKey,
	}, "|")
	if m.analyticsCache.key == key {
		return m.analyticsCache.content, m.analyticsCache.hasData
	}

	data := extractCostData(m.visibleSnapshots(), m.analyticsFilter.text)
	sortProviders(data.providers, m.analyticsSortBy)
	sortModels(data.models, m.analyticsSortBy)
	summary := computeAnalyticsSummary(data)
	hasData := data.totalCost > 0 || len(data.models) > 0 || len(data.budgets) > 0 ||
		len(data.usageGauges) > 0 || len(data.tokenActivity) > 0 || len(data.timeSeries) > 0

	content := ""
	if hasData {
		content = m.renderAnalyticsTabContent(data, summary, w)
	}

	m.analyticsCache = analyticsRenderCacheEntry{
		key:     key,
		hasData: hasData,
		content: content,
	}
	return content, hasData
}
