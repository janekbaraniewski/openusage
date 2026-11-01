package openrouter

import (
	"hash/maphash"
	"strconv"
	"sync"
)

const (
	// maxCachedGenerationDetails bounds the per-generation detail cache. It is
	// comfortably above maxGenerationsToFetch so a full window of rows for a
	// couple of accounts fits without churning.
	maxCachedGenerationDetails = 2048
	// maxCachedGenerationWindows bounds how many accounts (base URL + key) keep
	// their last-seen generation window in memory.
	maxCachedGenerationWindows = 16
)

// generationDetail holds the fields a /generation?id= lookup contributes.
// Completed generations are immutable, so a detail is cached forever (subject
// to the size bound) once fetched.
type generationDetail struct {
	ProviderName      string
	UpstreamID        string
	ProviderResponses []generationProviderResponse
}

// generationWindow is the newest-first list of generation rows returned by the
// previous poll for one account. The next poll pages only until it reaches
// rows[0].ID and reuses the remainder from here.
type generationWindow struct {
	rows []generationEntry
}

// generationCache is safe for concurrent use. Both maps are bounded with FIFO
// eviction, which is adequate because entries are written once and never
// updated in place.
type generationCache struct {
	mu sync.Mutex

	details     map[string]generationDetail
	detailOrder []string

	windows     map[string]generationWindow
	windowOrder []string
}

func newGenerationCache() *generationCache {
	return &generationCache{
		details: make(map[string]generationDetail),
		windows: make(map[string]generationWindow),
	}
}

func (c *generationCache) detail(id string) (generationDetail, bool) {
	if c == nil || id == "" {
		return generationDetail{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.details[id]
	return d, ok
}

func (c *generationCache) storeDetail(id string, d generationDetail) {
	if c == nil || id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.details[id]; !exists {
		c.detailOrder = append(c.detailOrder, id)
	}
	c.details[id] = d
	for len(c.detailOrder) > maxCachedGenerationDetails {
		oldest := c.detailOrder[0]
		c.detailOrder = c.detailOrder[1:]
		delete(c.details, oldest)
	}
}

// window returns a copy of the cached rows for key so callers can mutate it.
func (c *generationCache) window(key string) []generationEntry {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	w, ok := c.windows[key]
	if !ok {
		return nil
	}
	return append([]generationEntry(nil), w.rows...)
}

func (c *generationCache) storeWindow(key string, rows []generationEntry) {
	if c == nil {
		return
	}
	stored := append([]generationEntry(nil), rows...)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.windows[key]; !exists {
		c.windowOrder = append(c.windowOrder, key)
	}
	c.windows[key] = generationWindow{rows: stored}
	for len(c.windowOrder) > maxCachedGenerationWindows {
		oldest := c.windowOrder[0]
		c.windowOrder = c.windowOrder[1:]
		delete(c.windows, oldest)
	}
}

func (c *generationCache) dropWindow(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.windows[key]; !exists {
		return
	}
	delete(c.windows, key)
	for i, k := range c.windowOrder {
		if k == key {
			c.windowOrder = append(c.windowOrder[:i], c.windowOrder[i+1:]...)
			break
		}
	}
}

// windowKeySeed is random per process, so window keys cannot be precomputed
// or compared across runs.
var windowKeySeed = maphash.MakeSeed()

// generationWindowKey identifies an account without keeping the raw API key
// in memory as a map key. It is an in-memory cache key, not a stored secret
// digest, so a seeded non-cryptographic hash is enough; collisions across the
// handful of cached accounts are negligible at 64 bits.
func generationWindowKey(baseURL, apiKey string) string {
	return strconv.FormatUint(maphash.String(windowKeySeed, baseURL+"\x00"+apiKey), 16)
}

func (d generationDetail) apply(row *generationEntry) {
	if len(d.ProviderResponses) > 0 {
		row.ProviderResponses = d.ProviderResponses
	}
	if d.ProviderName != "" {
		row.ProviderName = d.ProviderName
	}
	if d.UpstreamID != "" {
		row.UpstreamID = d.UpstreamID
	}
}
