package telemetry

import (
	"sort"
	"strings"
)

// moneyUnits are the metric units that denote an amount of money. Only money
// metrics can feed windowed spend, so only they are worth a balance series.
var moneyUnits = map[string]bool{
	"USD": true, "EUR": true, "GBP": true, "CNY": true, "RMB": true,
	"JPY": true, "CAD": true, "AUD": true, "INR": true, "$": true,
}

// IsMoneyUnit reports whether unit is a currency.
func IsMoneyUnit(unit string) bool {
	return moneyUnits[strings.ToUpper(strings.TrimSpace(unit))]
}

// IsCreditPriorityMetric reports whether metricKey is one of the keys the read
// model may pick as an account's primary credit metric.
func IsCreditPriorityMetric(metricKey string) bool {
	for _, k := range creditMetricPriority {
		if k == metricKey {
			return true
		}
	}
	return false
}

// ShouldRecordBalanceMetric decides whether a metric gets a row in
// balance_observations. Window-based inference alone classifies every
// "lifetime" counter as a cumulative balance, which for local agents meant
// recording about a thousand token/session/request counters per poll that no
// query ever reads. A metric is recorded only when the provider declares it in
// CreditMetrics, when it is a key PrimaryCreditMetric can select, or when its
// unit is a currency.
func ShouldRecordBalanceMetric(metricKey, unit string, declared bool) bool {
	return declared || IsCreditPriorityMetric(metricKey) || IsMoneyUnit(unit)
}

// dropNonMoneyBalanceObservationsSQL deletes the balance rows that
// ShouldRecordBalanceMetric would no longer record: neither a credit-priority
// key nor a currency unit. Idempotent; safe to re-run.
func dropNonMoneyBalanceObservationsSQL() string {
	keys := make([]string, 0, len(creditMetricPriority))
	for _, k := range creditMetricPriority {
		keys = append(keys, "'"+strings.ReplaceAll(k, "'", "''")+"'")
	}
	units := make([]string, 0, len(moneyUnits))
	for u := range moneyUnits {
		units = append(units, "'"+strings.ReplaceAll(u, "'", "''")+"'")
	}
	sort.Strings(units)
	return `DELETE FROM balance_observations
		WHERE metric_key NOT IN (` + strings.Join(keys, ", ") + `)
		  AND UPPER(TRIM(COALESCE(unit, ''))) NOT IN (` + strings.Join(units, ", ") + `)`
}
