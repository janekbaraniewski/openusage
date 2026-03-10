package daemon

import (
	"sort"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
	"github.com/samber/lo"
)

type sourceCollectorSpec struct {
	source    shared.TelemetrySource
	options   shared.TelemetryCollectOptions
	accountID string
}

func buildCollectors(accounts []core.AccountConfig) ([]telemetry.Collector, []string) {
	specs, warnings := buildSourceCollectorSpecs(accounts)
	collectors := make([]telemetry.Collector, 0, len(specs))
	for _, spec := range specs {
		collectors = append(collectors, telemetry.NewSourceCollector(spec.source, spec.options, spec.accountID))
	}
	return collectors, warnings
}

func telemetrySourceCount() int {
	count := 0
	for _, provider := range providers.AllProviders() {
		if _, ok := provider.(shared.TelemetrySource); ok {
			count++
		}
	}
	return count
}

func ResolveTelemetrySourceOptions(
	source shared.TelemetrySource,
	requestedAccountID string,
) (shared.TelemetryCollectOptions, string, []string) {
	accountID := strings.TrimSpace(requestedAccountID)
	if source == nil {
		return shared.TelemetryCollectOptions{}, accountID, nil
	}
	accounts, err := loadTelemetrySourceAccounts()
	if err != nil {
		opts := cloneCollectOptions(source.DefaultCollectOptions())
		if accountID != "" {
			if opts.Paths == nil {
				opts.Paths = make(map[string]string)
			}
			opts.Paths["account_id"] = accountID
		}
		return opts, accountID, []string{"telemetry config unavailable; using default source options"}
	}

	return resolveTelemetrySourceOptionsFromAccounts(source, accounts, accountID)
}

func loadTelemetrySourceAccounts() ([]core.AccountConfig, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	accounts := core.MergeAccounts(cfg.Accounts, cfg.AutoDetectedAccounts)
	return ApplyCredentials(accounts), nil
}

func resolveTelemetrySourceOptionsFromAccounts(
	source shared.TelemetrySource,
	accounts []core.AccountConfig,
	requestedAccountID string,
) (shared.TelemetryCollectOptions, string, []string) {
	accountID := strings.TrimSpace(requestedAccountID)
	if source == nil {
		return shared.TelemetryCollectOptions{}, accountID, nil
	}
	defaults := cloneCollectOptions(source.DefaultCollectOptions())

	candidates := telemetryAccountsForSource(source, accounts)
	if accountID != "" {
		for _, acct := range candidates {
			if strings.EqualFold(strings.TrimSpace(acct.ID), accountID) {
				return collectOptionsForAccount(source, acct), strings.TrimSpace(acct.ID), nil
			}
		}
		if defaults.Paths == nil {
			defaults.Paths = make(map[string]string)
		}
		defaults.Paths["account_id"] = accountID
		return defaults, accountID, []string{"telemetry account override not found in config; using source defaults"}
	}

	switch len(candidates) {
	case 0:
		return defaults, "", nil
	case 1:
		acct := candidates[0]
		return collectOptionsForAccount(source, acct), strings.TrimSpace(acct.ID), nil
	default:
		return defaults, "", []string{"multiple telemetry accounts configured for source; account override required for precise hook attribution"}
	}
}

func buildSourceCollectorSpecs(accounts []core.AccountConfig) ([]sourceCollectorSpec, []string) {
	providersBySource := telemetrySourcesBySystem()
	sourceNames := core.SortedStringKeys(providersBySource)

	specs := make([]sourceCollectorSpec, 0, len(sourceNames))
	var warnings []string
	for _, sourceName := range sourceNames {
		source := providersBySource[sourceName]
		candidates := telemetryAccountsForSource(source, accounts)
		if len(candidates) == 0 {
			specs = append(specs, sourceCollectorSpec{
				source:  source,
				options: cloneCollectOptions(source.DefaultCollectOptions()),
			})
			continue
		}

		groups := make(map[string][]core.AccountConfig)
		groupOptions := make(map[string]shared.TelemetryCollectOptions)
		for _, acct := range candidates {
			opts := collectOptionsForAccount(source, acct)
			key := collectOptionsSignature(opts)
			if _, ok := groups[key]; !ok {
				groupOptions[key] = opts
			}
			groups[key] = append(groups[key], acct)
		}
		groupKeys := core.SortedStringKeys(groups)

		for _, key := range groupKeys {
			group := groups[key]
			opts := groupOptions[key]
			if len(group) == 1 {
				specs = append(specs, sourceCollectorSpec{
					source:    source,
					options:   opts,
					accountID: strings.TrimSpace(group[0].ID),
				})
				continue
			}

			accountIDs := core.SortedCompactStrings(lo.Map(group, func(acct core.AccountConfig, _ int) string {
				return acct.ID
			}))
			delete(opts.Paths, "account_id")
			specs = append(specs, sourceCollectorSpec{
				source:  source,
				options: opts,
			})
			warnings = append(warnings, sourceName+": shared telemetry source paths for accounts "+strings.Join(accountIDs, ", ")+": using source-scoped attribution")
		}
	}

	return specs, warnings
}

func telemetrySourcesBySystem() map[string]shared.TelemetrySource {
	out := make(map[string]shared.TelemetrySource)
	for _, provider := range providers.AllProviders() {
		source, ok := provider.(shared.TelemetrySource)
		if !ok {
			continue
		}
		system := strings.ToLower(strings.TrimSpace(source.System()))
		if system == "" {
			continue
		}
		out[system] = source
	}
	return out
}

func telemetryAccountsForSource(source shared.TelemetrySource, accounts []core.AccountConfig) []core.AccountConfig {
	if source == nil || len(accounts) == 0 {
		return nil
	}
	system := strings.ToLower(strings.TrimSpace(source.System()))
	if system == "" {
		return nil
	}

	out := make([]core.AccountConfig, 0, len(accounts))
	for _, acct := range accounts {
		if !strings.EqualFold(strings.TrimSpace(acct.Provider), system) {
			continue
		}
		if strings.TrimSpace(acct.ID) == "" {
			continue
		}
		out = append(out, acct)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func collectOptionsForAccount(source shared.TelemetrySource, acct core.AccountConfig) shared.TelemetryCollectOptions {
	opts := cloneCollectOptions(source.DefaultCollectOptions())
	if opts.Paths == nil {
		opts.Paths = make(map[string]string)
	}
	for key, value := range opts.Paths {
		opts.Paths[key] = strings.TrimSpace(acct.Path(key, value))
	}
	for key, value := range acct.PathMap() {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		opts.Paths[trimmedKey] = trimmedValue
	}
	opts.Paths["account_id"] = strings.TrimSpace(acct.ID)
	return opts
}

func cloneCollectOptions(in shared.TelemetryCollectOptions) shared.TelemetryCollectOptions {
	out := shared.TelemetryCollectOptions{}
	if len(in.Paths) > 0 {
		out.Paths = make(map[string]string, len(in.Paths))
		for key, value := range in.Paths {
			out.Paths[key] = strings.TrimSpace(value)
		}
	}
	if len(in.PathLists) > 0 {
		out.PathLists = make(map[string][]string, len(in.PathLists))
		for key, values := range in.PathLists {
			if len(values) == 0 {
				continue
			}
			cloned := make([]string, 0, len(values))
			for _, value := range values {
				if trimmed := strings.TrimSpace(value); trimmed != "" {
					cloned = append(cloned, trimmed)
				}
			}
			out.PathLists[key] = cloned
		}
	}
	return out
}

func collectOptionsSignature(opts shared.TelemetryCollectOptions) string {
	pathKeys := lo.Filter(core.SortedStringKeys(opts.Paths), func(key string, _ int) bool {
		return key != "account_id" && strings.TrimSpace(opts.Paths[key]) != ""
	})
	listKeys := lo.Filter(core.SortedStringKeys(opts.PathLists), func(key string, _ int) bool {
		return len(opts.PathLists[key]) > 0
	})

	var b strings.Builder
	for _, key := range pathKeys {
		b.WriteString("p:")
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(strings.TrimSpace(opts.Paths[key]))
		b.WriteByte(';')
	}
	for _, key := range listKeys {
		values := core.SortedCompactStrings(opts.PathLists[key])
		b.WriteString("l:")
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(strings.Join(values, ","))
		b.WriteByte(';')
	}
	return b.String()
}
