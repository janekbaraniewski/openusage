package sketchybar

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var updateDocs = flag.Bool("update-docs", false, "rewrite the managed snippet block in the SketchyBar guide")

// docsGuidePath is the guide whose "Full managed snippet" block is generated
// from BuildSnippet rather than maintained by hand.
const docsGuidePath = "../../docs/site/docs/guides/sketchybar-integration.md"

// TestDocsSnippetUpToDate keeps the published snippet honest. The block drifted
// once already: the guide advertised `--subscribe ai_switcher mouse.entered`
// after the switcher moved to mouse.clicked, which left anyone following the
// documented config with a switcher that responded to nothing.
//
// Regenerate with: make docs-sketchybar
// skipIfWindows guards the checks that compare generated snippet content.
// BuildSnippet routes DataDir through expandPath, which ends in filepath.Clean
// and therefore rewrites "/" to "\\" on windows -- the published block would
// read OPENUSAGE_SKETCHYBAR_DIR='$HOME\\.local\\share\\...'. SketchyBar is
// macOS-only, so the guide is POSIX by definition and the freshness check is
// meaningful only where the separators match. It still runs on ubuntu and macOS.
func skipIfWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sketchybar is macOS-only; generated paths use windows separators here")
	}
}

func TestDocsSnippetUpToDate(t *testing.T) {
	skipIfWindows(t)

	path := filepath.FromSlash(docsGuidePath)
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read guide: %v", err)
	}

	updated, changed, err := SyncDocsSnippet(string(current))
	if err != nil {
		t.Fatalf("sync docs snippet: %v", err)
	}
	if !changed {
		return
	}
	if *updateDocs {
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			t.Fatalf("write guide: %v", err)
		}
		t.Logf("rewrote %s", path)
		return
	}
	t.Fatalf("%s is stale; regenerate with `make docs-sketchybar`\nwant block:\n%s", docsGuidePath, mustDocsBlock(t))
}

func mustDocsBlock(t *testing.T) string {
	t.Helper()
	block, err := DocsSnippetBlock()
	if err != nil {
		t.Fatalf("DocsSnippetBlock: %v", err)
	}
	return block
}

// TestSyncDocsSnippetNormalizesCRLF pins the fix for a windows-only CI break.
// Git for Windows checks out CRLF by default, so a guide that was byte-correct
// still compared unequal against LF-generated content and every run reported
// the docs as stale.
func TestSyncDocsSnippetNormalizesCRLF(t *testing.T) {
	skipIfWindows(t)

	raw, err := os.ReadFile(filepath.FromSlash(docsGuidePath))
	if err != nil {
		t.Fatalf("read guide: %v", err)
	}
	// The checkout itself may already be CRLF (this test also runs on windows),
	// so normalise before building the fixture. Replacing "\n" in CRLF content
	// would yield "\r\r\n" and test nothing real.
	lf := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if _, changed, err := SyncDocsSnippet(lf); err != nil || changed {
		t.Fatalf("LF guide should already be in sync: changed=%v err=%v", changed, err)
	}

	crlf := strings.ReplaceAll(lf, "\n", "\r\n")
	if crlf == lf {
		t.Fatal("CRLF fixture is identical to the LF source; the test proves nothing")
	}
	_, changed, err := SyncDocsSnippet(crlf)
	if err != nil {
		t.Fatalf("sync CRLF guide: %v", err)
	}
	if changed {
		t.Fatal("a CRLF checkout of an up-to-date guide was reported as stale")
	}
}

// TestDocsSnippetBlockIsFenced guards the generator itself: a malformed block
// would silently corrupt the guide when -update-docs runs.
func TestDocsSnippetBlockIsFenced(t *testing.T) {
	block, err := DocsSnippetBlock()
	if err != nil {
		t.Fatalf("DocsSnippetBlock: %v", err)
	}
	if !strings.HasPrefix(block, "```bash\n") {
		t.Fatalf("block does not open a bash fence:\n%s", block)
	}
	if !strings.HasSuffix(block, "\n```") {
		t.Fatalf("block does not close its fence:\n%s", block)
	}
	for _, want := range []string{SentinelStart, SentinelEnd, "ai-usage.sh", "provider-select.sh"} {
		if !strings.Contains(block, want) {
			t.Fatalf("block missing %q:\n%s", want, block)
		}
	}
	// Exactly two fences: the block must never nest or leave one unbalanced.
	if got := strings.Count(block, "```"); got != 2 {
		t.Fatalf("fence count = %d, want 2:\n%s", got, block)
	}
}

// TestSyncDocsSnippetRejectsMissingBlock ensures a guide that lost its block
// fails loudly instead of being silently left stale.
func TestSyncDocsSnippetRejectsMissingBlock(t *testing.T) {
	if _, _, err := SyncDocsSnippet("# Guide\n\nNo snippet here.\n"); err == nil {
		t.Fatal("expected an error for a guide with no managed block")
	}
}
