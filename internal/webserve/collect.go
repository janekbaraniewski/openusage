package webserve

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/export"
	"github.com/janekbaraniewski/openusage/internal/version"
)

type collector struct {
	mu       sync.Mutex
	cached   Envelope
	cachedAt time.Time
	ttl      time.Duration
	source   export.Source
	demo     bool
	meta     collectorMeta
	opts     Options
	now      func() time.Time
	collect  CollectFunc
}

type collectorMeta struct {
	version        string
	timeWindow     string
	theme          string
	refreshSeconds int
	catalog        []CatalogEntry
}

func newCollector(opts Options) *collector {
	refresh := opts.RefreshSeconds
	if refresh <= 0 {
		refresh = 30
	}
	src := export.Source(strings.ToLower(strings.TrimSpace(opts.Source)))
	if src == "" {
		src = export.SourceAuto
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	c := &collector{
		ttl:    time.Duration(refresh) * time.Second,
		source: src,
		demo:   opts.Demo,
		now:    now,
		opts:   opts,
		meta: collectorMeta{
			version:        strings.TrimSpace(opts.Version),
			timeWindow:     strings.TrimSpace(opts.TimeWindow),
			theme:          strings.TrimSpace(opts.Theme),
			refreshSeconds: refresh,
			catalog:        providerCatalog(),
		},
		collect: opts.Collect,
	}
	if c.meta.version == "" {
		c.meta.version = strings.TrimSpace(version.Version)
	}
	if c.meta.timeWindow == "" {
		c.meta.timeWindow = "30d"
	}
	if c.meta.theme == "" {
		c.meta.theme = "Gruvbox"
	}
	return c
}

func (c *collector) envelope() (Envelope, error) {
	if c.collect != nil {
		env, err := c.collect()
		if err != nil {
			return Envelope{}, err
		}
		return c.decorate(env), nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if !c.cachedAt.IsZero() && c.ttl > 0 && now.Sub(c.cachedAt) < c.ttl {
		return c.cached, nil
	}

	env, err := c.fetch()
	if err != nil {
		return Envelope{}, err
	}
	env = c.decorate(env)
	c.cached = env
	c.cachedAt = now
	return env, nil
}

func (c *collector) fetch() (Envelope, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	snaps, source, err := c.fetchSnapshots(ctx)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		Source:    source,
		Snapshots: snaps,
	}, nil
}

func (c *collector) decorate(env Envelope) Envelope {
	out := env
	out.SchemaVersion = schemaVersion
	out.GeneratedAt = c.now()
	out.OpenUsageVersion = c.meta.version
	out.TimeWindow = c.meta.timeWindow
	out.Theme = c.meta.theme
	out.RefreshIntervalSeconds = c.meta.refreshSeconds
	out.Catalog = c.meta.catalog
	if c.opts.UsageMode != "" {
		out.UsageMode = c.opts.UsageMode
	} else if c.opts.Config != nil && c.opts.Config.Dashboard.UsageMode != "" {
		out.UsageMode = c.opts.Config.Dashboard.UsageMode
	}
	if strings.TrimSpace(out.Source) == "" {
		if c.demo {
			out.Source = "demo"
		} else {
			out.Source = string(c.source)
		}
	}
	out.Snapshots = stripRaw(out.Snapshots)
	views, tokens := buildViews(c.opts, c.meta, out.Snapshots)
	out.Views = views
	out.ThemeTokens = tokens
	return out
}
