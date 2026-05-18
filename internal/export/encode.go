package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// encode writes the envelope to w in the requested format.
//
// For JSON we use a canonical 2-space indent and append a trailing newline so
// the output works nicely with line-oriented tools. Snapshots arrive
// pre-sorted; we strip the Raw map defensively (the schema commits to omitting
// it, and provider probes sometimes stash credential hints there).
func encode(w io.Writer, env ExportEnvelope, format Format) error {
	cleaned := stripRawAndTokens(env)
	switch format {
	case "", FormatJSON:
		return encodeJSON(w, cleaned)
	case FormatCSV:
		return encodeCSV(w, cleaned)
	default:
		return fmt.Errorf("export: unsupported format %q", format)
	}
}

func encodeJSON(w io.Writer, env ExportEnvelope) error {
	buf, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("export: encoding envelope: %w", err)
	}
	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("export: writing envelope: %w", err)
	}
	if _, err := w.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("export: writing trailing newline: %w", err)
	}
	return nil
}

// encodeCSV writes a flat metric-per-row CSV. It's intentionally minimal in v1
// — JSON is the canonical export format.
//
// Columns:
//   schema_version, generated_at, openusage_version, source,
//   provider_id, account_id, snapshot_timestamp, status, message,
//   metric, used, limit, remaining, unit, window
func encodeCSV(w io.Writer, env ExportEnvelope) error {
	buf := bytes.NewBuffer(nil)
	cw := csv.NewWriter(buf)
	header := []string{
		"schema_version", "generated_at", "openusage_version", "source",
		"provider_id", "account_id", "snapshot_timestamp", "status", "message",
		"metric", "used", "limit", "remaining", "unit", "window",
	}
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("export: writing csv header: %w", err)
	}

	envFields := []string{
		env.SchemaVersion,
		env.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		env.OpenUsageVersion,
		string(env.Source),
	}

	for _, snap := range env.Snapshots {
		baseSnap := []string{
			snap.ProviderID,
			snap.AccountID,
			snap.Timestamp.UTC().Format("2006-01-02T15:04:05Z07:00"),
			string(snap.Status),
			snap.Message,
		}
		// Emit one row per metric for stable shape. If the snapshot has no
		// metrics (auth error, empty provider) write a single row with
		// blank metric columns so the snapshot is still represented.
		keys := make([]string, 0, len(snap.Metrics))
		for k := range snap.Metrics {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		if len(keys) == 0 {
			row := append(append([]string{}, envFields...), baseSnap...)
			row = append(row, "", "", "", "", "", "")
			if err := cw.Write(row); err != nil {
				return fmt.Errorf("export: writing csv row: %w", err)
			}
			continue
		}

		for _, key := range keys {
			m := snap.Metrics[key]
			row := append(append([]string{}, envFields...), baseSnap...)
			row = append(row,
				key,
				floatPtrString(m.Used),
				floatPtrString(m.Limit),
				floatPtrString(m.Remaining),
				m.Unit,
				m.Window,
			)
			if err := cw.Write(row); err != nil {
				return fmt.Errorf("export: writing csv row: %w", err)
			}
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("export: flushing csv: %w", err)
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("export: writing csv output: %w", err)
	}
	return nil
}

func floatPtrString(p *float64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatFloat(*p, 'f', -1, 64)
}

// stripRawAndTokens returns a deep-cloned envelope whose snapshots have the
// `Raw` map cleared. AccountConfig.Token is already excluded by `json:"-"`
// but Raw is a string map and not excluded by the schema — provider probes
// occasionally drop credential hints there. Defensive cleanup keeps the
// export file safe to share.
func stripRawAndTokens(env ExportEnvelope) ExportEnvelope {
	out := env
	if len(env.Snapshots) == 0 {
		return out
	}
	out.Snapshots = make([]core.UsageSnapshot, len(env.Snapshots))
	for i, snap := range env.Snapshots {
		clone := snap.DeepClone()
		clone.Raw = nil
		out.Snapshots[i] = clone
	}
	return out
}
