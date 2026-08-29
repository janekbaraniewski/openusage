package webserve

import (
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

func buildViews(opts Options, meta collectorMeta, snaps []core.UsageSnapshot) ([]AccountView, ThemeTokens) {
	cfg := configOrDefault(opts)
	projector := tui.NewWebProjectorFromConfig(cfg)
	ordered := projector.OrderSnapshots(tui.SnapshotsToMap(snaps))
	if len(ordered) == 0 {
		ordered = snaps
	}

	names := catalogNames(meta)
	views := projector.ProjectSnapshots(ordered, names)
	out := make([]AccountView, len(views))
	for i, v := range views {
		out[i] = accountViewFromTUI(v)
	}
	tokens := tui.WebThemeTokensFromTheme(tui.ActiveTheme())
	return out, themeTokensFromTUI(tokens)
}

func configOrDefault(opts Options) config.Config {
	if opts.Config != nil {
		return *opts.Config
	}
	cfg := config.DefaultConfig()
	if opts.TimeWindow != "" {
		cfg.Data.TimeWindow = opts.TimeWindow
	}
	if opts.Theme != "" {
		cfg.Theme = opts.Theme
	}
	if opts.UsageMode != "" {
		cfg.Dashboard.UsageMode = opts.UsageMode
	}
	if opts.WarnThreshold > 0 {
		cfg.UI.WarnThreshold = opts.WarnThreshold
	}
	if opts.CritThreshold > 0 {
		cfg.UI.CritThreshold = opts.CritThreshold
	}
	if opts.RefreshSeconds > 0 {
		cfg.UI.RefreshIntervalSeconds = opts.RefreshSeconds
	}
	return cfg
}

func catalogNames(meta collectorMeta) map[string]string {
	names := make(map[string]string)
	for _, c := range meta.catalog {
		names[c.ID] = c.Name
	}
	return names
}

func accountViewFromTUI(v tui.WebAccountView) AccountView {
	sections := make([]DetailSection, len(v.DetailSections))
	for i, s := range v.DetailSections {
		sections[i] = DetailSection{Title: s.Title, Icon: s.Icon, Lines: s.Lines}
	}
	resets := make([]ResetPill, len(v.Resets))
	for i, r := range v.Resets {
		resets[i] = ResetPill{Label: r.Label, Duration: r.Duration, Urgent: r.Urgent}
	}
	return AccountView{
		Key:            v.Key,
		ProviderID:     v.ProviderID,
		ProviderName:   v.ProviderName,
		AccountID:      v.AccountID,
		Status:         v.Status,
		StatusBadge:    v.StatusBadge,
		StatusIcon:     v.StatusIcon,
		AccentColor:    v.AccentColor,
		Summary:        v.Summary,
		Detail:         v.Detail,
		TagEmoji:       v.TagEmoji,
		TagLabel:       v.TagLabel,
		GaugePercent:   v.GaugePercent,
		Message:        v.Message,
		Timestamp:      v.Timestamp,
		TileLines:      v.TileLines,
		DetailSections: sections,
		Resets:         resets,
		DailyCost:      v.DailyCost,
	}
}

func themeTokensFromTUI(t tui.WebThemeTokens) ThemeTokens {
	return ThemeTokens{
		Name: t.Name, Icon: t.Icon, Base: t.Base, Mantle: t.Mantle,
		Surface0: t.Surface0, Surface1: t.Surface1, Surface2: t.Surface2,
		Text: t.Text, Subtext: t.Subtext, Dim: t.Dim,
		Accent: t.Accent, Blue: t.Blue, Sapphire: t.Sapphire,
		Green: t.Green, Yellow: t.Yellow, Red: t.Red,
		Peach: t.Peach, Teal: t.Teal, Lavender: t.Lavender, Mauve: t.Mauve,
	}
}
