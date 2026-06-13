package tui

import (
	"strings"
	"testing"
)

func TestRenderModelTokenBreakdownCacheAnnotation(t *testing.T) {
	models := []modelMixEntry{
		{name: "claude-opus-4-8", input: 100, output: 50, cacheRead: 700, cacheWrite: 100},
	}
	lines := renderModelTokenBreakdown(models, 120, nil)
	if len(lines) == 0 {
		t.Fatal("expected breakdown lines")
	}
	// read=700, denom = in(100)+read(700)+write(100) = 900 -> 78%.
	if !strings.Contains(lines[0], "78% cached") {
		t.Fatalf("header missing cache annotation: %q", lines[0])
	}
}

func TestRenderModelTokenBreakdownNoCacheNoAnnotation(t *testing.T) {
	models := []modelMixEntry{
		{name: "gpt-x", input: 100, output: 50},
	}
	lines := renderModelTokenBreakdown(models, 120, nil)
	if len(lines) == 0 {
		t.Fatal("expected breakdown lines")
	}
	if strings.Contains(lines[0], "cached") {
		t.Fatalf("did not expect cache annotation without cache tokens: %q", lines[0])
	}
}
