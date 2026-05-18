package pricing

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// DefaultTTL is the cache freshness window used when OPENUSAGE_PRICING_TTL
// is unset or unparseable.
const DefaultTTL = 24 * time.Hour

// DiskCache stores upstream pricing payloads under
// $UserCacheDir/openusage/pricing/<name>.json, keyed by source name.
// Writes are atomic (write to tmp + rename) so a concurrent reader never
// sees a partially-written file.
type DiskCache struct {
	dir string
	ttl time.Duration
	mu  sync.Mutex
}

// NewDiskCache returns a DiskCache rooted at the platform user cache dir
// ($XDG_CACHE_HOME or ~/Library/Caches on macOS). The directory is created
// lazily on first write.
func NewDiskCache() (*DiskCache, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("pricing: resolving user cache dir: %w", err)
	}
	dir := filepath.Join(base, "openusage", "pricing")
	return &DiskCache{dir: dir, ttl: ResolveTTL()}, nil
}

// NewDiskCacheAt returns a cache rooted at an explicit directory. Useful in
// tests and for callers that want to share a fixture path.
func NewDiskCacheAt(dir string) *DiskCache {
	return &DiskCache{dir: dir, ttl: ResolveTTL()}
}

// Dir returns the directory the cache writes to.
func (c *DiskCache) Dir() string { return c.dir }

// TTL returns the configured freshness window.
func (c *DiskCache) TTL() time.Duration { return c.ttl }

// SetTTL overrides the freshness window. Provided mainly for tests.
func (c *DiskCache) SetTTL(ttl time.Duration) { c.ttl = ttl }

// Path returns the on-disk path for a given cache slot.
func (c *DiskCache) Path(name string) string {
	return filepath.Join(c.dir, name+".json")
}

// Load returns the cached payload for `name` along with its mtime. The bool
// reports whether the cache entry exists AND is fresh (mtime+ttl > now).
// A stale entry still returns its bytes (with ok=false) so callers can
// fall back to it on upstream failure.
func (c *DiskCache) Load(name string) ([]byte, time.Time, bool, error) {
	path := c.Path(name)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, time.Time{}, false, nil
		}
		return nil, time.Time{}, false, fmt.Errorf("pricing: stat cache %s: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("pricing: read cache %s: %w", path, err)
	}
	fresh := time.Since(info.ModTime()) < c.ttl
	return data, info.ModTime(), fresh, nil
}

// Store atomically writes data to the named cache slot. Concurrent callers
// targeting the same slot are serialised via c.mu so that the
// write-to-tmp + rename window is well-defined.
func (c *DiskCache) Store(name string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("pricing: creating cache dir: %w", err)
	}
	final := c.Path(name)
	tmp, err := os.CreateTemp(c.dir, "."+name+"-*.tmp")
	if err != nil {
		return fmt.Errorf("pricing: creating temp cache file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("pricing: writing temp cache: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("pricing: syncing temp cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("pricing: closing temp cache: %w", err)
	}
	if err := os.Rename(tmpPath, final); err != nil {
		cleanup()
		return fmt.Errorf("pricing: renaming temp cache: %w", err)
	}
	return nil
}

// ResolveTTL returns the cache freshness window honouring the
// OPENUSAGE_PRICING_TTL env var (Go duration syntax, e.g. "12h", "30m").
// On parse failure or unset, DefaultTTL is returned.
func ResolveTTL() time.Duration {
	v := os.Getenv("OPENUSAGE_PRICING_TTL")
	if v == "" {
		return DefaultTTL
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	// allow plain seconds for convenience
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return DefaultTTL
}
