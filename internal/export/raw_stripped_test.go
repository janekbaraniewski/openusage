package export

import (
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// Raw may contain credential hints left by provider probes. Export must never
// carry it. If this test fails, fix the encoder rather than weakening it.
func TestEncodeStripsRawMap(t *testing.T) {
	snap := core.NewUsageSnapshot("claude_code", "default")
	snap.Raw["block_start"] = "2026-08-15T14:00:00Z"
	snap.Raw["totally_a_secret"] = "sk-live-should-never-appear"

	var buf strings.Builder
	if err := encode(&buf, ExportEnvelope{
		SchemaVersion: SchemaVersion,
		Snapshots:     []core.UsageSnapshot{snap},
	}, FormatJSON); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "sk-live-should-never-appear") {
		t.Fatalf("Raw map leaked into export output:\n%s", out)
	}
	if strings.Contains(out, "totally_a_secret") {
		t.Fatalf("Raw keys leaked into export output:\n%s", out)
	}
}
