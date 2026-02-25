package daemon

import (
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/detect"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func ResolveAccounts(cfg *config.Config) []core.AccountConfig {
	allAccounts := core.MergeAccounts(cfg.Accounts, cfg.AutoDetectedAccounts)

	if cfg.AutoDetect {
		result := detect.AutoDetect()

		manualIDs := make(map[string]bool, len(cfg.Accounts))
		for _, acct := range cfg.Accounts {
			manualIDs[acct.ID] = true
		}
		var autoDetected []core.AccountConfig
		for _, acct := range result.Accounts {
			if !manualIDs[acct.ID] {
				autoDetected = append(autoDetected, acct)
			}
		}

		cfg.AutoDetectedAccounts = autoDetected
		if err := config.SaveAutoDetected(autoDetected); err != nil {
			log.Printf("Warning: could not persist auto-detected accounts: %v", err)
		}

		allAccounts = core.MergeAccounts(cfg.Accounts, cfg.AutoDetectedAccounts)

		if os.Getenv("OPENUSAGE_DEBUG") != "" {
			if len(result.Tools) > 0 || len(result.Accounts) > 0 {
				log.Print(result.Summary())
			}
		}
	}

	return ApplyCredentials(allAccounts)
}

func ApplyCredentials(accounts []core.AccountConfig) []core.AccountConfig {
	credResult := detect.Result{Accounts: accounts}
	detect.ApplyCredentials(&credResult)
	return credResult.Accounts
}

func ResolveSocketPath() string {
	path, _ := ResolveSocketPathWithError()
	return path
}

func ResolveSocketPathWithError() (string, error) {
	if value := strings.TrimSpace(os.Getenv("OPENUSAGE_TELEMETRY_SOCKET")); value != "" {
		return value, nil
	}
	return telemetry.DefaultSocketPath()
}

func FilterAccountsByDashboard(
	accounts []core.AccountConfig,
	dashboardCfg config.DashboardConfig,
) []core.AccountConfig {
	if len(accounts) == 0 {
		return nil
	}

	enabledByAccountID := make(map[string]bool, len(dashboardCfg.Providers))
	for _, pref := range dashboardCfg.Providers {
		accountID := strings.TrimSpace(pref.AccountID)
		if accountID == "" {
			continue
		}
		enabledByAccountID[accountID] = pref.Enabled
	}

	filtered := make([]core.AccountConfig, 0, len(accounts))
	for _, acct := range accounts {
		accountID := strings.TrimSpace(acct.ID)
		if accountID == "" {
			continue
		}
		enabled, ok := enabledByAccountID[accountID]
		if ok && !enabled {
			continue
		}
		filtered = append(filtered, acct)
	}
	return filtered
}

func DisabledAccountsFromDashboard(dashboardCfg config.DashboardConfig) map[string]bool {
	disabled := make(map[string]bool, len(dashboardCfg.Providers))
	for _, pref := range dashboardCfg.Providers {
		accountID := strings.TrimSpace(pref.AccountID)
		if accountID == "" || pref.Enabled {
			continue
		}
		disabled[accountID] = true
	}
	return disabled
}

func DisabledAccountsFromConfig() map[string]bool {
	cfg, err := config.Load()
	if err != nil {
		return map[string]bool{}
	}
	return DisabledAccountsFromDashboard(cfg.Dashboard)
}

func resolveConfigAccounts(
	cfg *config.Config,
	resolver func(*config.Config) []core.AccountConfig,
) []core.AccountConfig {
	if cfg == nil {
		return nil
	}

	if cfg.AutoDetect && resolver != nil {
		accounts := resolver(cfg)
		return FilterAccountsByDashboard(accounts, cfg.Dashboard)
	}

	accounts := core.MergeAccounts(cfg.Accounts, cfg.AutoDetectedAccounts)
	accounts = FilterAccountsByDashboard(accounts, cfg.Dashboard)
	return ApplyCredentials(accounts)
}

func LoadAccountsAndNorm() ([]core.AccountConfig, core.ModelNormalizationConfig, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, core.DefaultModelNormalizationConfig(), err
	}
	accounts := resolveConfigAccounts(&cfg, ResolveAccounts)
	return accounts, core.NormalizeModelNormalizationConfig(cfg.ModelNormalization), nil
}

func BuildReadModelRequest(
	accounts []core.AccountConfig,
	providerLinks map[string]string,
	timeWindow string,
) ReadModelRequest {
	seen := make(map[string]bool, len(accounts))
	outAccounts := make([]ReadModelAccount, 0, len(accounts))
	for _, acct := range accounts {
		accountID := strings.TrimSpace(acct.ID)
		providerID := strings.TrimSpace(acct.Provider)
		if accountID == "" || providerID == "" || seen[accountID] {
			continue
		}
		seen[accountID] = true
		outAccounts = append(outAccounts, ReadModelAccount{
			AccountID:  accountID,
			ProviderID: providerID,
		})
	}
	links := make(map[string]string, len(providerLinks))
	for source, target := range providerLinks {
		source = strings.ToLower(strings.TrimSpace(source))
		target = strings.ToLower(strings.TrimSpace(target))
		if source != "" && target != "" {
			links[source] = target
		}
	}
	return ReadModelRequest{Accounts: outAccounts, ProviderLinks: links, TimeWindow: timeWindow}
}

func BuildReadModelRequestFromConfig() (ReadModelRequest, error) {
	cfg, err := config.Load()
	if err != nil {
		return ReadModelRequest{}, err
	}
	accounts := resolveConfigAccounts(&cfg, ResolveAccounts)
	return BuildReadModelRequest(accounts, cfg.Telemetry.ProviderLinks, cfg.Data.TimeWindow), nil
}

func ReadModelRequestKey(req ReadModelRequest) string {
	accounts := make([]ReadModelAccount, 0, len(req.Accounts))
	seenAccounts := make(map[string]bool, len(req.Accounts))
	for _, account := range req.Accounts {
		accountID := strings.TrimSpace(account.AccountID)
		providerID := strings.TrimSpace(account.ProviderID)
		if accountID == "" || providerID == "" {
			continue
		}
		key := accountID + "|" + providerID
		if seenAccounts[key] {
			continue
		}
		seenAccounts[key] = true
		accounts = append(accounts, ReadModelAccount{
			AccountID:  accountID,
			ProviderID: providerID,
		})
	}
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].AccountID != accounts[j].AccountID {
			return accounts[i].AccountID < accounts[j].AccountID
		}
		return accounts[i].ProviderID < accounts[j].ProviderID
	})

	linkKeys := make([]string, 0, len(req.ProviderLinks))
	for source, target := range req.ProviderLinks {
		source = strings.ToLower(strings.TrimSpace(source))
		target = strings.ToLower(strings.TrimSpace(target))
		if source == "" || target == "" {
			continue
		}
		linkKeys = append(linkKeys, source+"="+target)
	}
	sort.Strings(linkKeys)

	var b strings.Builder
	b.Grow(128 + len(accounts)*32 + len(linkKeys)*24)
	b.WriteString("accounts:")
	for _, account := range accounts {
		b.WriteString(account.AccountID)
		b.WriteByte(':')
		b.WriteString(account.ProviderID)
		b.WriteByte(';')
	}
	b.WriteString("|links:")
	for _, key := range linkKeys {
		b.WriteString(key)
		b.WriteByte(';')
	}
	return b.String()
}

func ReadModelTemplatesFromRequest(
	req ReadModelRequest,
	disabledAccounts map[string]bool,
) map[string]core.UsageSnapshot {
	if disabledAccounts == nil {
		disabledAccounts = map[string]bool{}
	}
	accounts := make([]ReadModelAccount, 0, len(req.Accounts))
	seen := make(map[string]bool, len(req.Accounts))
	for _, account := range req.Accounts {
		accountID := strings.TrimSpace(account.AccountID)
		providerID := strings.TrimSpace(account.ProviderID)
		if disabledAccounts[accountID] {
			continue
		}
		if accountID == "" || providerID == "" || seen[accountID] {
			continue
		}
		seen[accountID] = true
		accounts = append(accounts, ReadModelAccount{AccountID: accountID, ProviderID: providerID})
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].AccountID < accounts[j].AccountID })

	out := make(map[string]core.UsageSnapshot, len(accounts))
	now := time.Now().UTC()
	for _, account := range accounts {
		out[account.AccountID] = core.UsageSnapshot{
			ProviderID:  account.ProviderID,
			AccountID:   account.AccountID,
			Timestamp:   now,
			Status:      core.StatusUnknown,
			Metrics:     map[string]core.Metric{},
			Resets:      map[string]time.Time{},
			Attributes:  map[string]string{},
			Diagnostics: map[string]string{},
			Raw:         map[string]string{},
			DailySeries: map[string][]core.TimePoint{},
		}
	}
	return out
}

func SnapshotsHaveUsableData(snaps map[string]core.UsageSnapshot) bool {
	for _, snap := range snaps {
		if snap.Status != core.StatusUnknown {
			return true
		}
		if len(snap.Metrics) > 0 || len(snap.Resets) > 0 || len(snap.DailySeries) > 0 || len(snap.ModelUsage) > 0 {
			return true
		}
	}
	return false
}
