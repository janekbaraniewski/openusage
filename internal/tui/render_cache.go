package tui

import (
	"strconv"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type detailRenderCacheEntry struct {
	key     string
	content string
}

func (m *Model) invalidateTileBodyCache() {
	m.tileBodyCache = make(map[string][]string)
}

func (m *Model) invalidateDetailCache() {
	m.detailCache = detailRenderCacheEntry{}
}

func (m *Model) invalidateRenderCaches() {
	m.invalidateTileBodyCache()
	m.invalidateAnalyticsCache()
	m.invalidateDetailCache()
}

func (m *Model) cachedDetailContent(id string, snap core.UsageSnapshot, w int, activeTab int) string {
	key := strings.Join([]string{
		id,
		snap.ProviderID,
		strconv.Itoa(w),
		strconv.Itoa(activeTab),
		strconv.FormatInt(snap.Timestamp.UTC().Unix(), 10),
		strconv.Itoa(len(snap.Metrics)),
		strconv.Itoa(len(snap.DailySeries)),
		strconv.Itoa(len(snap.ModelUsage)),
		strconv.Itoa(len(snap.Resets)),
		strconv.Itoa(len(snap.Attributes)),
		strconv.Itoa(len(snap.Diagnostics)),
		strconv.Itoa(len(snap.Raw)),
		string(m.timeWindow),
		strconv.FormatFloat(m.warnThreshold, 'f', 4, 64),
		strconv.FormatFloat(m.critThreshold, 'f', 4, 64),
	}, "|")
	if m.detailCache.key == key {
		return m.detailCache.content
	}

	content := RenderDetailContent(snap, w, m.warnThreshold, m.critThreshold, activeTab)
	m.detailCache = detailRenderCacheEntry{
		key:     key,
		content: content,
	}
	return content
}
