// Package export provides the `openusage export` command implementation.
//
// It collects current usage snapshots from either the running telemetry daemon
// (preferred when available) or by running a one-shot direct provider poll, and
// serializes them into a versioned JSON envelope that downstream tooling can
// consume.
//
// Schema version 1 is the initial format. It may change before the project
// reaches a stable release.
package export

import (
	"io"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// SchemaVersion is the version of the export envelope format. Bumped on
// breaking changes; minor additive changes keep the same version.
const SchemaVersion = "1"

// Source identifies which collection path produced the snapshots.
type Source string

const (
	// SourceAuto is the default: try the daemon first, fall back to a
	// direct provider poll if the daemon is unavailable.
	SourceAuto Source = "auto"
	// SourceDirect runs the same auto-detect + provider-fetch flow used
	// by the dashboard, synchronously.
	SourceDirect Source = "direct"
	// SourceDaemon connects to the running telemetry daemon over its
	// unix socket and reads snapshots from the daemon's read model.
	SourceDaemon Source = "daemon"
)

// Format is the on-disk output encoding for an export.
type Format string

const (
	// FormatJSON writes the envelope as indented JSON terminated by a
	// newline. This is the default and the only fully supported format.
	FormatJSON Format = "json"
	// FormatCSV writes a flattened metric-per-row CSV. Intentionally
	// minimal in v1 — JSON is the canonical export.
	FormatCSV Format = "csv"
)

// ExportEnvelope is the top-level JSON payload written by the export command.
//
// Fields are stable for SchemaVersion=1. Token-bearing fields on
// core.AccountConfig already use `json:"-"` so they cannot land here, but the
// encoder also strips snapshot Raw maps defensively since they sometimes carry
// credential hints from provider probes.
type ExportEnvelope struct {
	SchemaVersion    string                `json:"schema_version"`
	GeneratedAt      time.Time             `json:"generated_at"`
	OpenUsageVersion string                `json:"openusage_version"`
	Source           Source                `json:"source"`
	Snapshots        []core.UsageSnapshot  `json:"snapshots"`
}

// Options captures the parameters parsed from CLI flags. The orchestrator
// resolves defaults before invoking the collection paths.
type Options struct {
	// Output is the destination file path. "-" means stdout.
	Output string
	// Format is the on-disk encoding. Defaults to JSON.
	Format Format
	// Source selects the collection path. Defaults to auto.
	Source Source

	// Now overrides the GeneratedAt timestamp. Tests use it to assert
	// deterministic envelopes. Zero value means time.Now().UTC().
	Now time.Time

	// Version overrides the embedded openusage_version. Used by tests so
	// they don't depend on the build-injected version string. Empty means
	// the value from internal/version.
	Version string

	// Stderr is the writer used for non-fatal diagnostics (for example
	// "daemon unavailable, falling back to direct mode"). Tests substitute
	// a buffer here. Nil means os.Stderr.
	Stderr io.Writer
}
