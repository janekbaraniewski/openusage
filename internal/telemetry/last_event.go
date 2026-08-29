package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// canonicalEventProvider maps the provider_id recorded on an event onto the
// provider_id the active-selection candidate list is keyed by.
//
// Claude Code is the one agent whose two ingest paths disagree: its hook events
// are recorded against the model vendor ("anthropic"), while the credential
// poller registers the candidate as "claude_code". Left unmapped, real Claude
// Code activity never joins its candidate, so active selection cannot see it and
// a pin on it releases itself the moment Claude is used. Fold the hook spelling
// onto the poller spelling, keyed on agent_name so that direct Anthropic
// API-key usage keeps its own identity.
func canonicalEventProvider(providerID, agentName string) string {
	if providerID == "anthropic" && agentName == "claude_code" {
		return "claude_code"
	}
	return providerID
}

// LastEventTimes returns the most recent canonical event timestamp for each
// provider/account pair. Keys are "provider_id:account_id".
func (s *Store) LastEventTimes(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT provider_id, agent_name, account_id, MAX(occurred_at)
		   FROM usage_events
		  WHERE provider_id IS NOT NULL AND provider_id != ''
		    AND event_type IN ('turn_completed', 'message_usage', 'tool_usage')
		  GROUP BY provider_id, agent_name, account_id`)
	if err != nil {
		return nil, fmt.Errorf("telemetry: querying last event times: %w", err)
	}
	defer rows.Close()

	out := make(map[string]time.Time)
	for rows.Next() {
		var provider string
		var agent sql.NullString
		var account sql.NullString
		var occurred sql.NullString
		if err := rows.Scan(&provider, &agent, &account, &occurred); err != nil {
			return nil, fmt.Errorf("telemetry: scanning last event time: %w", err)
		}
		if !occurred.Valid || strings.TrimSpace(occurred.String) == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, occurred.String)
		if err != nil {
			continue
		}
		acct := account.String
		if strings.TrimSpace(acct) == "" {
			acct = "default"
		}
		key := canonicalEventProvider(provider, strings.TrimSpace(agent.String)) + ":" + acct
		// Grouping by agent_name splits rows the alias then merges, so keep the
		// latest timestamp per resolved key rather than whichever row lands last.
		if prev, ok := out[key]; ok && !ts.UTC().After(prev) {
			continue
		}
		out[key] = ts.UTC()
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("telemetry: iterating last event times: %w", err)
	}
	return out, nil
}
