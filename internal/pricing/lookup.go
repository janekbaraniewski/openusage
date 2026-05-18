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

// Resolver chains the upstream pricing sources together. A single Resolver
// owns the disk cache and the in-memory tables, refreshing on demand.
//
// Resolver is safe for concurrent use.
type Resolver struct {
	cache          *DiskCache
	litellm        *LiteLLMFetcher
	openrouter     *OpenRouterFetcher
	staleOnFailure bool

	mu             sync.Mutex
	liteLLMTable   map[string]Price
	openRouter     map[string]Price
	liteLLMLoaded  bool
	openRouterDone bool
}

// ResolverOption customises Resolver behaviour.
type ResolverOption func(*Resolver)

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
	keys := make([]string, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	hit, ok := bestFuzzyMatch(model, keys)
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
	keys := make([]string, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	hit, ok := bestFuzzyMatch(model, keys)
	if !ok {
		return Price{}, false
	}
	return table[hit], true
}

func (r *Resolver) loadLiteLLM(ctx context.Context) (map[string]Price, error) {
	r.mu.Lock()
	if r.liteLLMLoaded {
		t := r.liteLLMTable
		r.mu.Unlock()
		return t, nil
	}
	r.mu.Unlock()

	// fresh cache hit?
	if data, mtime, fresh, err := r.cache.Load(litellmCacheName); err == nil && fresh && len(data) > 0 {
		if table, perr := ParseLiteLLM(data); perr == nil {
			r.storeLiteLLM(table, mtime)
			return table, nil
		}
	}

	table, body, err := r.litellm.Fetch(ctx)
	if err != nil {
		// fall back to a stale cache copy if we have one
		if r.staleOnFailure {
			if data, mtime, _, lerr := r.cache.Load(litellmCacheName); lerr == nil && len(data) > 0 {
				if cached, perr := ParseLiteLLM(data); perr == nil {
					r.storeLiteLLM(cached, mtime)
					return cached, nil
				}
			}
		}
		return nil, err
	}
	if len(body) > 0 {
		_ = r.cache.Store(litellmCacheName, body)
	}
	r.storeLiteLLM(table, time.Now().UTC())
	return table, nil
}

func (r *Resolver) loadOpenRouter(ctx context.Context) (map[string]Price, error) {
	r.mu.Lock()
	if r.openRouterDone {
		t := r.openRouter
		r.mu.Unlock()
		return t, nil
	}
	r.mu.Unlock()

	if data, mtime, fresh, err := r.cache.Load(openrouterCacheName); err == nil && fresh && len(data) > 0 {
		if table, perr := ParseOpenRouter(data); perr == nil {
			r.storeOpenRouter(table, mtime)
			return table, nil
		}
	}

	table, body, err := r.openrouter.Fetch(ctx)
	if err != nil {
		if r.staleOnFailure {
			if data, mtime, _, lerr := r.cache.Load(openrouterCacheName); lerr == nil && len(data) > 0 {
				if cached, perr := ParseOpenRouter(data); perr == nil {
					r.storeOpenRouter(cached, mtime)
					return cached, nil
				}
			}
		}
		return nil, err
	}
	if len(body) > 0 {
		_ = r.cache.Store(openrouterCacheName, body)
	}
	r.storeOpenRouter(table, time.Now().UTC())
	return table, nil
}

func (r *Resolver) storeLiteLLM(t map[string]Price, mtime time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.liteLLMTable = t
	r.liteLLMLoaded = true
	if !mtime.IsZero() {
		for k, p := range t {
			p.LastUpdated = mtime
			t[k] = p
		}
	}
}

func (r *Resolver) storeOpenRouter(t map[string]Price, mtime time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.openRouter = t
	r.openRouterDone = true
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
