package pricing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// litellmCacheName / openrouterCacheName are the cache slot names used by
// the resolver. Exported for tests that want to seed cache contents.
const (
	litellmCacheName    = "litellm"
	openrouterCacheName = "openrouter"
)

// StaleRetryInterval is how long the resolver serves a stale cache copy
// after an upstream failure before dialling out again. It keeps a down
// upstream from turning every Lookup into a failed network round-trip.
const StaleRetryInterval = 5 * time.Minute

// Resolver chains the upstream pricing sources together. A single Resolver
// owns the disk cache and the in-memory tables, refreshing on demand.
//
// Resolver is safe for concurrent use.
type Resolver struct {
	cache          *DiskCache
	litellm        *LiteLLMFetcher
	openrouter     *OpenRouterFetcher
	staleOnFailure bool

	overrides customOverridesCache

	// now is the clock used for TTL arithmetic. Tests swap it out; in
	// production it is time.Now.
	now func() time.Time

	mu             sync.Mutex
	liteLLMTable   map[string]Price
	openRouter     map[string]Price
	liteLLMLoaded  bool
	openRouterDone bool

	// liteLLMValidUntil / openRouterValidUntil bound how long the
	// in-memory table may be served before the resolver re-validates
	// against disk and upstream. Without them a long-lived process (the
	// telemetry daemon) latches the table it loaded at startup and serves
	// those rates until it is restarted, no matter how far the on-disk
	// cache has aged past its TTL.
	liteLLMValidUntil    time.Time
	openRouterValidUntil time.Time

	// liteLLMKeysCache and openRouterKeysCache hold the (model-key list,
	// normalized-key index) pair for each upstream so bestFuzzyMatch does
	// not rebuild the index on every Lookup. A typical hot caller burns
	// thousands of Lookups per Fetch over the same key set; rebuilding the
	// regex-normalised index each time made claude_code's per-fetch cost
	// estimation dominate Fetch wall time.
	liteLLMKeysCache    *fuzzyKeyIndex
	openRouterKeysCache *fuzzyKeyIndex

	// lookupCache memoises full Lookup results keyed by (model, contextLen)
	// so repeated cost estimation for the same model on the same Fetch is
	// a single map probe instead of a fuzzy walk.
	//
	// lookupCacheValidUntil bounds it. The memo short-circuits resolve()
	// entirely, so without an expiry a model priced once would keep its
	// original rate for the life of the process and never give the table
	// TTLs above a chance to run — the hot callers (claude_code, codex)
	// price the same few models on every Fetch.
	lookupCache           map[lookupCacheKey]*Price
	lookupCacheValidUntil time.Time
}

type lookupCacheKey struct {
	model      string
	contextLen int
}

// WithCustomOverrides seeds the resolver with a pre-loaded overrides table.
// Tests use this to bypass the on-disk lookup; callers using DefaultResolver
// or NewResolver pick the table up from disk on first Lookup.
func WithCustomOverrides(table map[string]Price) ResolverOption {
	return func(r *Resolver) {
		r.overrides.once.Do(func() { r.overrides.loaded = table })
	}
}

// ResolverOption customises Resolver behaviour.
type ResolverOption func(*Resolver)

// withClock overrides the clock used for TTL arithmetic. Test-only.
func withClock(fn func() time.Time) ResolverOption {
	return func(r *Resolver) {
		if fn != nil {
			r.now = fn
		}
	}
}

// WithCache overrides the disk cache (used in tests).
func WithCache(c *DiskCache) ResolverOption { return func(r *Resolver) { r.cache = c } }

// WithLiteLLMFetcher overrides the upstream LiteLLM client.
func WithLiteLLMFetcher(f *LiteLLMFetcher) ResolverOption {
	return func(r *Resolver) { r.litellm = f }
}

// WithOpenRouterFetcher overrides the upstream OpenRouter client.
func WithOpenRouterFetcher(f *OpenRouterFetcher) ResolverOption {
	return func(r *Resolver) { r.openrouter = f }
}

// NewResolver constructs a Resolver using the platform user cache dir and
// the default upstream HTTP clients.
func NewResolver(opts ...ResolverOption) (*Resolver, error) {
	r := &Resolver{
		litellm:        NewLiteLLMFetcher(),
		openrouter:     NewOpenRouterFetcher(),
		staleOnFailure: true,
		now:            time.Now,
	}
	for _, opt := range opts {
		opt(r)
	}
	if r.cache == nil {
		c, err := NewDiskCache()
		if err != nil {
			return nil, err
		}
		r.cache = c
	}
	if r.now == nil {
		r.now = time.Now
	}
	// The disk cache's freshness window and the resolver's in-memory
	// window must advance on the same clock, or a test (or a caller with
	// a skewed clock) can see one expire while the other does not.
	if r.cache != nil {
		r.cache.setClock(r.now)
	}
	return r, nil
}

// Lookup resolves rates for `model` at the given `contextLen`. The chain
// is: litellm -> openrouter -> hardcoded fallback. The resolver caches
// upstream payloads on disk (24h default TTL) and reuses them across
// calls.
//
// Lookup never returns nil + nil; callers either get a Price or an error.
// A nil Price + nil error never occurs.
func (r *Resolver) Lookup(ctx context.Context, model string, contextLen int) (*Price, error) {
	if model == "" {
		return nil, errors.New("pricing: empty model id")
	}

	key := lookupCacheKey{model: model, contextLen: contextLen}
	r.mu.Lock()
	if r.lookupCache != nil && !r.clock().Before(r.lookupCacheValidUntil) {
		r.lookupCache = nil
	}
	if cached, ok := r.lookupCache[key]; ok {
		r.mu.Unlock()
		if cached == nil {
			return nil, fmt.Errorf("pricing: no price for model %q", model)
		}
		return cached, nil
	}
	r.mu.Unlock()

	result, err := r.resolve(ctx, model, contextLen)

	r.mu.Lock()
	if r.lookupCache == nil {
		r.lookupCache = make(map[lookupCacheKey]*Price, 16)
		r.lookupCacheValidUntil = r.clock().Add(r.tableTTL())
	}
	r.lookupCache[key] = result
	r.mu.Unlock()

	if result == nil {
		return nil, err
	}
	return result, nil
}

// clock returns the resolver's current time, tolerating a zero-value
// Resolver built by a caller that bypassed NewResolver.
func (r *Resolver) clock() time.Time {
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}

// tableTTL returns the freshness window used for the in-memory tables and
// the lookup memo, tolerating a nil cache.
func (r *Resolver) tableTTL() time.Duration {
	if r.cache == nil {
		return DefaultTTL
	}
	return r.cache.TTL()
}

func (r *Resolver) resolve(ctx context.Context, model string, contextLen int) (*Price, error) {
	if p, ok := lookupCustomOverride(r.overrides.get(), model); ok {
		out := ApplyTier(p, contextLen)
		return &out, nil
	}

	if p, ok := r.tryLiteLLM(ctx, model); ok {
		out := ApplyTier(p, contextLen)
		return &out, nil
	}
	if p, ok := r.tryOpenRouter(ctx, model); ok {
		out := ApplyTier(p, contextLen)
		return &out, nil
	}
	if p, ok := lookupHardcoded(model); ok {
		out := ApplyTier(p, contextLen)
		return &out, nil
	}
	return nil, fmt.Errorf("pricing: no price for model %q", model)
}

func (r *Resolver) tryLiteLLM(ctx context.Context, model string) (Price, bool) {
	table, err := r.loadLiteLLM(ctx)
	if err != nil || len(table) == 0 {
		return Price{}, false
	}
	idx := r.fuzzyIndexFor(table, &r.liteLLMKeysCache)
	hit, ok := bestFuzzyMatchIndexed(model, idx)
	if !ok {
		return Price{}, false
	}
	return table[hit], true
}

func (r *Resolver) tryOpenRouter(ctx context.Context, model string) (Price, bool) {
	table, err := r.loadOpenRouter(ctx)
	if err != nil || len(table) == 0 {
		return Price{}, false
	}
	idx := r.fuzzyIndexFor(table, &r.openRouterKeysCache)
	hit, ok := bestFuzzyMatchIndexed(model, idx)
	if !ok {
		return Price{}, false
	}
	return table[hit], true
}

// fuzzyIndexFor returns the cached normalized-key index for `table`,
// rebuilding it only when the table identity has changed. The slot
// pointer-of-pointer means the same Resolver can independently cache
// LiteLLM and OpenRouter indices.
func (r *Resolver) fuzzyIndexFor(table map[string]Price, slot **fuzzyKeyIndex) *fuzzyKeyIndex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if *slot != nil && (*slot).sourceLen == len(table) {
		return *slot
	}
	idx := buildFuzzyKeyIndex(table)
	*slot = idx
	return idx
}

func (r *Resolver) loadLiteLLM(ctx context.Context) (map[string]Price, error) {
	r.mu.Lock()
	if r.liteLLMLoaded && r.clock().Before(r.liteLLMValidUntil) {
		t := r.liteLLMTable
		r.mu.Unlock()
		return t, nil
	}
	r.mu.Unlock()

	// fresh cache hit? another process may have refreshed the file since
	// we last read it, so this is re-checked every time the in-memory
	// window lapses, not just on the first load.
	if data, mtime, fresh, err := r.cache.Load(litellmCacheName); err == nil && fresh && len(data) > 0 {
		if table, perr := ParseLiteLLM(data); perr == nil {
			r.storeLiteLLM(table, mtime, mtime.Add(r.tableTTL()))
			return table, nil
		}
	}

	table, body, err := r.litellm.Fetch(ctx)
	if err != nil {
		// fall back to a stale cache copy if we have one, and hold it for
		// StaleRetryInterval so a down upstream is not re-dialled on
		// every Lookup.
		if r.staleOnFailure {
			if data, mtime, _, lerr := r.cache.Load(litellmCacheName); lerr == nil && len(data) > 0 {
				if cached, perr := ParseLiteLLM(data); perr == nil {
					r.storeLiteLLM(cached, mtime, r.clock().Add(StaleRetryInterval))
					return cached, nil
				}
			}
		}
		return nil, err
	}
	if len(body) > 0 {
		_ = r.cache.Store(litellmCacheName, body)
	}
	r.storeLiteLLM(table, time.Now().UTC(), r.clock().Add(r.tableTTL()))
	return table, nil
}

func (r *Resolver) loadOpenRouter(ctx context.Context) (map[string]Price, error) {
	r.mu.Lock()
	if r.openRouterDone && r.clock().Before(r.openRouterValidUntil) {
		t := r.openRouter
		r.mu.Unlock()
		return t, nil
	}
	r.mu.Unlock()

	if data, mtime, fresh, err := r.cache.Load(openrouterCacheName); err == nil && fresh && len(data) > 0 {
		if table, perr := ParseOpenRouter(data); perr == nil {
			r.storeOpenRouter(table, mtime, mtime.Add(r.tableTTL()))
			return table, nil
		}
	}

	table, body, err := r.openrouter.Fetch(ctx)
	if err != nil {
		if r.staleOnFailure {
			if data, mtime, _, lerr := r.cache.Load(openrouterCacheName); lerr == nil && len(data) > 0 {
				if cached, perr := ParseOpenRouter(data); perr == nil {
					r.storeOpenRouter(cached, mtime, r.clock().Add(StaleRetryInterval))
					return cached, nil
				}
			}
		}
		return nil, err
	}
	if len(body) > 0 {
		_ = r.cache.Store(openrouterCacheName, body)
	}
	r.storeOpenRouter(table, time.Now().UTC(), r.clock().Add(r.tableTTL()))
	return table, nil
}

func (r *Resolver) storeLiteLLM(t map[string]Price, mtime, validUntil time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.liteLLMTable = t
	r.liteLLMLoaded = true
	r.liteLLMValidUntil = validUntil
	r.liteLLMKeysCache = nil
	r.lookupCache = nil
	if !mtime.IsZero() {
		for k, p := range t {
			p.LastUpdated = mtime
			t[k] = p
		}
	}
}

func (r *Resolver) storeOpenRouter(t map[string]Price, mtime, validUntil time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.openRouter = t
	r.openRouterDone = true
	r.openRouterValidUntil = validUntil
	r.openRouterKeysCache = nil
	r.lookupCache = nil
	if !mtime.IsZero() {
		for k, p := range t {
			p.LastUpdated = mtime
			t[k] = p
		}
	}
}

// EstimateCost computes a USD cost from a resolved Price and a per-token
// usage record. Pass any zero token bucket to skip that line item. Pass a
// contextLen > 0 to apply the appropriate tier override before computing.
//
// If price is nil this returns 0 (so callers can chain Lookup -> EstimateCost
// without a nil check for fall-through fallback paths).
type Usage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	ReasoningTokens  int
}

// Estimate returns the projected cost in USD for a single usage record at
// the given context length.
func Estimate(price *Price, contextLen int, u Usage) float64 {
	if price == nil {
		return 0
	}
	p := ApplyTier(*price, contextLen)
	cost := float64(u.InputTokens) * p.InputCostPerMillion / 1_000_000
	cost += float64(u.OutputTokens) * p.OutputCostPerMillion / 1_000_000
	if u.CacheReadTokens > 0 && p.CacheReadCostPerMillion > 0 {
		cost += float64(u.CacheReadTokens) * p.CacheReadCostPerMillion / 1_000_000
	}
	if u.CacheWriteTokens > 0 && p.CacheWriteCostPerMillion > 0 {
		cost += float64(u.CacheWriteTokens) * p.CacheWriteCostPerMillion / 1_000_000
	}
	if u.ReasoningTokens > 0 {
		rate := p.ReasoningCostPerMillion
		if rate <= 0 {
			rate = p.OutputCostPerMillion
		}
		cost += float64(u.ReasoningTokens) * rate / 1_000_000
	}
	return cost
}

// DefaultResolver returns a process-wide lazy Resolver singleton. The
// first call constructs the resolver; subsequent calls reuse it. On
// construction failure (e.g. no writable cache dir), Lookups still work
// via in-memory tables but no on-disk caching occurs.
func DefaultResolver() *Resolver {
	defaultOnce.Do(func() {
		r, err := NewResolver()
		if err != nil {
			r = &Resolver{
				litellm:    NewLiteLLMFetcher(),
				openrouter: NewOpenRouterFetcher(),
				// no disk cache -- still functional, just no persistence
				cache: NewDiskCacheAt(""),
			}
		}
		defaultResolver = r
	})
	return defaultResolver
}

var (
	defaultResolver *Resolver
	defaultOnce     sync.Once
)
