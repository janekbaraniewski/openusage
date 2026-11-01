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

// invalidateAnalyticsCache resets the shared analytics cache entry in place.
// See invalidateDetailCache for why the entry lives behind a pointer.
func (m *Model) invalidateAnalyticsCache() {
	if m.analyticsCache == nil {
		m.analyticsCache = &analyticsRenderCacheEntry{}
		return
	}
	*m.analyticsCache = analyticsRenderCacheEntry{}
}

func (m *Model) cachedAnalyticsPageContent(w int) (string, bool) {
	key := strings.Join([]string{
		strconv.Itoa(w),
		strconv.Itoa(m.analyticsSortBy),
		m.analyticsFilter.text,
		string(m.timeWindow),
		strconv.Itoa(ActiveThemeIndex()),
		// Charts fall back to today's date when snapshots carry no
		// timestamp, so an entry cannot outlive the day.
		m.viewNow().UTC().Format("2006-01-02"),
		// Visible providers and their order (settings toggles/reorders).
		strings.Join(m.sortedIDs, ","),
	}, "|")
	if m.analyticsCache != nil && m.analyticsCache.key == key {
		return m.analyticsCache.content, m.analyticsCache.hasData
	}

	data := extractCostData(m.visibleSnapshots(), m.analyticsFilter.text, m.timeWindow)
	sortProviders(data.providers, m.analyticsSortBy)
	sortModels(data.models, m.analyticsSortBy)
	summary := computeAnalyticsSummary(data)
	hasData := data.totalCost > 0 || len(data.models) > 0 || len(data.budgets) > 0 ||
		len(data.usageGauges) > 0 || len(data.tokenActivity) > 0 || len(data.timeSeries) > 0

	content := ""
	if hasData {
		content = m.renderAnalyticsPageContent(data, summary, w)
	}

	if m.analyticsCache != nil {
		*m.analyticsCache = analyticsRenderCacheEntry{
			key:     key,
			hasData: hasData,
			content: content,
		}
	}
	return content, hasData
}
