package sketchybar

import (
	"fmt"
	"strings"
)

// DocsSnippetOptions are the options the documented "Full managed snippet"
// block is rendered with. They are deliberately generic — a neutral data
// directory and a bare binary name — so the published block matches what a
// reader gets from a default `openusage sketchybar install --write`.
func DocsSnippetOptions() InstallOptions {
	return InstallOptions{
		Preset:  DefaultPreset,
		Binary:  "openusage",
		DataDir: "$HOME/.local/share/openusage/sketchybar",
	}
}

// DocsSnippetBlock renders the fenced Markdown block published in the
// SketchyBar integration guide.
func DocsSnippetBlock() (string, error) {
	snippet, err := BuildSnippet(DocsSnippetOptions())
	if err != nil {
		return "", fmt.Errorf("sketchybar: building docs snippet: %w", err)
	}
	return "```bash\n" + strings.TrimRight(snippet, "\n") + "\n```", nil
}

// SyncDocsSnippet rewrites the managed snippet block inside doc and reports
// whether the content changed. The block is located by the same sentinels the
// installer writes into sketchybarrc, so no extra markers are needed in the
// Markdown — and MDX never sees an HTML comment it cannot parse.
//
// Line endings are normalised to LF first. Git for Windows checks out CRLF by
// default, and comparing a CRLF working copy against LF-generated content
// reports every file as stale no matter its content.
func SyncDocsSnippet(doc string) (string, bool, error) {
	block, err := DocsSnippetBlock()
	if err != nil {
		return "", false, err
	}
	doc = normalizeNewlines(doc)
	start, end, err := docsSnippetBounds(doc)
	if err != nil {
		return "", false, err
	}
	updated := doc[:start] + block + doc[end:]
	return updated, updated != doc, nil
}

func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// docsSnippetBounds returns the byte range of the fenced block that holds the
// managed snippet.
func docsSnippetBounds(doc string) (int, int, error) {
	open := "```bash\n" + SentinelStart
	start := strings.Index(doc, open)
	if start < 0 {
		return 0, 0, fmt.Errorf("sketchybar: docs missing a ```bash block starting with %q", SentinelStart)
	}
	sentinel := strings.Index(doc[start:], SentinelEnd)
	if sentinel < 0 {
		return 0, 0, fmt.Errorf("sketchybar: docs snippet block missing %q", SentinelEnd)
	}
	rest := start + sentinel + len(SentinelEnd)
	closing := strings.Index(doc[rest:], "```")
	if closing < 0 {
		return 0, 0, fmt.Errorf("sketchybar: docs snippet block is not closed")
	}
	return start, rest + closing + len("```"), nil
}
