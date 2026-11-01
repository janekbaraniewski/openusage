package daemon

import (
	"context"
	"maps"
	"os"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

// accountsCacheMaxAge bounds how long a resolved account set is served before
// a background re-resolve picks up newly installed tools or keys. Detection
// walks the filesystem, keychain and env (about a second), so it must never
// run on the request path.
const accountsCacheMaxAge = 5 * time.Minute

// configStamp identifies one version of the settings file. Size is included
// because some filesystems only record mtime at one-second granularity.
type configStamp struct {
	exists  bool
	modTime time.Time
	size    int64
}

func statConfigStamp(path string) configStamp {
	info, err := os.Stat(path)
	if err != nil {
		return configStamp{}
	}
	return configStamp{exists: true, modTime: info.ModTime(), size: info.Size()}
}

// accountsCache holds the daemon's resolved account set so read-model
// requests, statusline/tmux renders and the poll loop do not re-run account
// auto-detection on every call.
//
//   - The settings file is re-read (cheap) whenever its mtime or size changes,
//     so dashboard enable/disable toggles and time-window changes apply on the
//     next request.
//   - The expensive resolve (auto-detection + credential lookup) runs once
//     synchronously when nothing has been resolved yet, and afterwards only in
//     the background: after a config change, or when the result is older than
//     maxAge. Callers keep getting the previous result meanwhile.
type accountsCache struct {
	path    string
	load    func() (config.Config, error)
	resolve func(*config.Config) []core.AccountConfig
	maxAge  time.Duration
	now     func() time.Time

	mu          sync.Mutex
	stamp       configStamp
	cfg         config.Config
	hasCfg      bool
	resolved    []core.AccountConfig
	resolvedAt  time.Time
	hasResolved bool
	resolving   bool
	resolveDone chan struct{}
	pending     sync.WaitGroup
}

func newAccountsCache() *accountsCache {
	return &accountsCache{
		path:    config.ConfigPath(),
		load:    config.Load,
		resolve: resolveUnfilteredAccounts,
		maxAge:  accountsCacheMaxAge,
		now:     time.Now,
	}
}

// resolveUnfilteredAccounts is the full, expensive resolve. Dashboard
// filtering is applied per call by the cache so toggles do not need a
// re-resolve.
func resolveUnfilteredAccounts(cfg *config.Config) []core.AccountConfig {
	if cfg == nil {
		return nil
	}
	if cfg.AutoDetect {
		return ResolveAccounts(cfg)
	}
	return ApplyCredentials(core.MergeAccounts(cfg.Accounts, cfg.AutoDetectedAccounts))
}

// reloadConfigLocked re-reads the settings file when its stamp moved. It
// reports whether a previously loaded config was replaced. c.mu must be held.
func (c *accountsCache) reloadConfigLocked() (changed bool, err error) {
	stamp := statConfigStamp(c.path)
	if c.hasCfg && stamp == c.stamp {
		return false, nil
	}
	cfg, err := c.load()
	if err != nil {
		if !c.hasCfg {
			return false, err
		}
		// Keep serving the last good config; a half-written file is
		// picked up again on the next call.
		return false, nil
	}
	changed = c.hasCfg
	c.cfg = cfg
	c.hasCfg = true
	c.stamp = stamp
	return changed, nil
}

// currentConfig returns the settings, re-read only when the file changed.
// It never triggers an account resolve.
func (c *accountsCache) currentConfig() (config.Config, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	changed, err := c.reloadConfigLocked()
	if err != nil {
		return config.Config{}, err
	}
	if changed && c.hasResolved {
		c.startResolveLocked()
	}
	return c.cfg, nil
}

// get returns the enabled accounts and the config they were filtered with.
// It only blocks on a resolve when none has completed yet.
func (c *accountsCache) get(ctx context.Context) ([]core.AccountConfig, config.Config, error) {
	c.mu.Lock()
	configChanged, err := c.reloadConfigLocked()
	if err != nil {
		c.mu.Unlock()
		return nil, config.Config{}, err
	}

	if !c.hasResolved {
		done := c.startResolveLocked()
		c.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return nil, config.Config{}, ctx.Err()
		}
		c.mu.Lock()
	} else if configChanged || c.now().Sub(c.resolvedAt) >= c.maxAge {
		c.startResolveLocked()
	}

	cfg := c.cfg
	accounts := cloneAccounts(FilterAccountsByDashboard(c.resolved, cfg.Dashboard))
	c.mu.Unlock()
	return accounts, cfg, nil
}

// startResolveLocked launches a background resolve unless one is already
// running, and returns a channel closed when the running resolve finishes.
// c.mu must be held.
func (c *accountsCache) startResolveLocked() <-chan struct{} {
	if c.resolving {
		return c.resolveDone
	}
	c.resolving = true
	done := make(chan struct{})
	c.resolveDone = done
	cfg := c.cfg
	cfg.Accounts = append([]core.AccountConfig(nil), c.cfg.Accounts...)
	cfg.AutoDetectedAccounts = append([]core.AccountConfig(nil), c.cfg.AutoDetectedAccounts...)

	c.pending.Add(1)
	go func() {
		defer c.pending.Done()
		defer close(done)
		resolved := c.resolve(&cfg)

		// The resolve may itself persist newly detected accounts, which
		// bumps the settings file. Re-read it here so that write does not
		// look like a user edit and trigger a second resolve.
		stamp := statConfigStamp(c.path)
		var reloaded config.Config
		var reloadErr error
		c.mu.Lock()
		changed := stamp != c.stamp
		c.mu.Unlock()
		if changed {
			reloaded, reloadErr = c.load()
		}

		c.mu.Lock()
		defer c.mu.Unlock()
		c.resolved = resolved
		c.resolvedAt = c.now()
		c.hasResolved = true
		c.resolving = false
		if changed && reloadErr == nil {
			c.cfg = reloaded
			c.hasCfg = true
			c.stamp = stamp
		}
	}()
	return done
}

// warm resolves accounts in the background so the first request after daemon
// start does not pay for auto-detection.
func (c *accountsCache) warm(ctx context.Context) {
	if c == nil {
		return
	}
	go func() { _, _, _ = c.get(ctx) }()
}

// wait blocks until no resolve is in flight. Test helper.
func (c *accountsCache) wait() { c.pending.Wait() }

// cloneAccounts deep-copies the map fields so callers (providers mutate
// RuntimeHints during Fetch) cannot race with each other or the cache.
func cloneAccounts(in []core.AccountConfig) []core.AccountConfig {
	if in == nil {
		return nil
	}
	out := make([]core.AccountConfig, len(in))
	for i, acct := range in {
		acct.ProviderPaths = maps.Clone(acct.ProviderPaths)
		acct.Paths = maps.Clone(acct.Paths)
		acct.RuntimeHints = maps.Clone(acct.RuntimeHints)
		if acct.BrowserCookie != nil {
			ref := *acct.BrowserCookie
			acct.BrowserCookie = &ref
		}
		out[i] = acct
	}
	return out
}

// accountsCache returns the service's account cache, creating it on first
// use so Service values built directly in tests work too.
func (s *Service) accountsCache() *accountsCache {
	s.accountsOnce.Do(func() {
		if s.accounts == nil {
			s.accounts = newAccountsCache()
		}
	})
	return s.accounts
}

// cachedAccountsAndNorm is the daemon-side account lookup used by the poll loop.
func (s *Service) cachedAccountsAndNorm(ctx context.Context) ([]core.AccountConfig, core.ModelNormalizationConfig, error) {
	accounts, cfg, err := s.accountsCache().get(ctx)
	if err != nil {
		return nil, core.DefaultModelNormalizationConfig(), err
	}
	return accounts, core.NormalizeModelNormalizationConfig(cfg.ModelNormalization), nil
}

// cachedReadModelRequest is the cached replacement for
// building a read-model request from settings. It also returns the accounts the dashboard
// has disabled so callers do not re-read the settings file for them.
func (s *Service) cachedReadModelRequest(ctx context.Context) (ReadModelRequest, map[string]bool, error) {
	accounts, cfg, err := s.accountsCache().get(ctx)
	if err != nil {
		return ReadModelRequest{}, nil, err
	}
	req := BuildReadModelRequest(accounts, cfg.Telemetry.ProviderLinks, core.ParseTimeWindow(cfg.Data.TimeWindow))
	return req, DisabledAccountsFromDashboard(cfg.Dashboard), nil
}

// disabledAccounts returns the dashboard-disabled account IDs from the cached
// settings, avoiding a settings-file parse per read-model compute.
func (s *Service) disabledAccounts() map[string]bool {
	cfg, err := s.accountsCache().currentConfig()
	if err != nil {
		return map[string]bool{}
	}
	return DisabledAccountsFromDashboard(cfg.Dashboard)
}
