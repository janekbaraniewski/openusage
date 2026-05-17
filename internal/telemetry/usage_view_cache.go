package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// usageViewCache caches the fully aggregated telemetryUsageAgg for a given
// filter, keyed by (dbPath, providerIDs, accountID, since, todaySince).
//
// Each entry stores a content fingerprint (MAX(rowid), COUNT(*)) captured at
// the time the aggregate was built. On lookup a cheap probe query runs the
// same MAX/COUNT against the underlying usage_events table; if the fingerprint
// matches we skip the expensive materialize + 14 aggregate queries.
//
// Retention pruning lowers COUNT(*) so prunes invalidate cleanly. Inserts
// raise MAX(rowid) so new events invalidate cleanly.
//
// The cache is process-global. It is bounded by a fixed maximum number of
// entries; on overflow the oldest entry by insertion order is dropped. The
// expected cardinality is small (one entry per provider × account × window
// class, typically <50), so a simple cap is sufficient.
const usageViewCacheMaxEntries = 64

type usageViewCacheEntry struct {
	agg        *telemetryUsageAgg
	maxRowID   int64
	count      int64
	insertedAt time.Time
}

type usageViewCacheStore struct {
	mu      sync.Mutex
	entries map[string]*usageViewCacheEntry
	// order tracks insertion order for FIFO eviction; we keep it cheap and
	// simple rather than building a full LRU.
	order []string
}

var globalUsageViewCache = &usageViewCacheStore{
	entries: make(map[string]*usageViewCacheEntry),
}

// usageViewCacheNamespace identifies the database backing a cache entry. It
// flows from ApplyCanonicalTelemetryViewWithOptions down into the loader so
// the cache survives the per-call sql.Open/Close lifecycle.
type usageViewCacheNamespace string

func usageViewCacheKey(namespace usageViewCacheNamespace, filter usageFilter) string {
	providers := strings.Join(normalizeProviderIDs(filter.ProviderIDs), ",")
	since := int64(0)
	if !filter.Since.IsZero() {
		since = filter.Since.UTC().UnixNano()
	}
	today := int64(0)
	if !filter.TodaySince.IsZero() {
		today = filter.TodaySince.UTC().UnixNano()
	}
	return fmt.Sprintf("%s|%s|%s|%d|%d", namespace, providers, strings.TrimSpace(filter.AccountID), since, today)
}

func (s *usageViewCacheStore) lookup(key string) (*usageViewCacheEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	return entry, ok
}

func (s *usageViewCacheStore) store(key string, entry *usageViewCacheEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.entries[key]; !exists {
		s.order = append(s.order, key)
		for len(s.order) > usageViewCacheMaxEntries {
			drop := s.order[0]
			s.order = s.order[1:]
			delete(s.entries, drop)
		}
	}
	s.entries[key] = entry
}

// reset is exposed for tests so they can run against a clean cache.
func (s *usageViewCacheStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string]*usageViewCacheEntry)
	s.order = nil
}

// probeUsageEventsFingerprint runs a cheap MAX(rowid) + COUNT(*) query against
// usage_events using the same provider/account filter as the materialization
// step. It deliberately ignores the Since cutoff so the probe stays index-
// driven: the cache key already includes Since, so an entry only matches when
// Since is identical between builder and reader.
func probeUsageEventsFingerprint(ctx context.Context, db *sql.DB, filter usageFilter) (int64, int64, error) {
	providers := normalizeProviderIDs(filter.ProviderIDs)
	if len(providers) == 0 {
		return 0, 0, nil
	}

	args := make([]any, 0, len(providers)+1)
	var b strings.Builder
	b.WriteString(`SELECT COALESCE(MAX(rowid), 0), COUNT(*)
		FROM usage_events
		WHERE event_type IN ('message_usage', 'tool_usage')
		  AND provider_id IN (`)
	for i, p := range providers {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("?")
		args = append(args, p)
	}
	b.WriteString(")")

	accountID := strings.TrimSpace(filter.AccountID)
	if accountID != "" {
		b.WriteString(" AND account_id = ?")
		args = append(args, accountID)
	}

	var maxRowID, count int64
	if err := db.QueryRowContext(ctx, b.String(), args...).Scan(&maxRowID, &count); err != nil {
		return 0, 0, fmt.Errorf("probe usage_events fingerprint: %w", err)
	}
	return maxRowID, count, nil
}

// loadUsageViewCached returns a cached aggregate if the fingerprint is
// unchanged. The hit bool is true on cache hit; on miss the caller must
// fall back to a full materialize+aggregate pass and then call
// storeUsageViewCache to populate the cache.
func loadUsageViewCached(
	ctx context.Context,
	db *sql.DB,
	namespace usageViewCacheNamespace,
	filter usageFilter,
) (*telemetryUsageAgg, int64, int64, bool, error) {
	if namespace == "" {
		return nil, 0, 0, false, nil
	}
	key := usageViewCacheKey(namespace, filter)
	entry, hit := globalUsageViewCache.lookup(key)
	maxRowID, count, err := probeUsageEventsFingerprint(ctx, db, filter)
	if err != nil {
		return nil, 0, 0, false, err
	}
	if !hit || entry == nil {
		return nil, maxRowID, count, false, nil
	}
	if entry.maxRowID != maxRowID || entry.count != count {
		core.Tracef("[usage_view_cache] miss (fingerprint changed) providers=%v account=%s maxRowID=%d→%d count=%d→%d",
			filter.ProviderIDs, filter.AccountID, entry.maxRowID, maxRowID, entry.count, count)
		return nil, maxRowID, count, false, nil
	}
	core.Tracef("[usage_view_cache] hit providers=%v account=%s maxRowID=%d count=%d age=%s",
		filter.ProviderIDs, filter.AccountID, entry.maxRowID, entry.count, time.Since(entry.insertedAt))
	return entry.agg, maxRowID, count, true, nil
}

func storeUsageViewCache(
	namespace usageViewCacheNamespace,
	filter usageFilter,
	agg *telemetryUsageAgg,
	maxRowID, count int64,
) {
	if namespace == "" || agg == nil {
		return
	}
	key := usageViewCacheKey(namespace, filter)
	globalUsageViewCache.store(key, &usageViewCacheEntry{
		agg:        agg,
		maxRowID:   maxRowID,
		count:      count,
		insertedAt: time.Now(),
	})
}
