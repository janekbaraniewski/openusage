package cursor

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractContainerBox(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/home/user/.agent-containers/physics/.cursor/chats/abc/transcript.jsonl", "physics"},
		{"/home/user/.cursor-containers/nurulz/workspace", "nurulz"},
		{"/home/user/.agy-containers/chaos/.gemini", "chaos"},
		{"/Users/me/project", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractContainerBox(tt.path)
		if got != tt.want {
			t.Errorf("extractContainerBox(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestExtractAccountSlug(t *testing.T) {
	tests := []struct {
		email string
		want  string
	}{
		{"physicsxd2izi@gmail.com", "physicsxd2izi"},
		{"nurul.islam@example.com", "nurulislam"},
		{"mohammed19-dev@company.org", "mohammed19-dev"},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractAccountSlug(tt.email)
		if got != tt.want {
			t.Errorf("extractAccountSlug(%q) = %q, want %q", tt.email, got, tt.want)
		}
	}
}

func TestCaptureStatusLine_AutoRouting(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "cursor-status.json")

	line, err := CaptureStatusLine([]byte(sampleCursorStatusLineJSON), mainPath)
	if err != nil {
		t.Fatalf("CaptureStatusLine() error = %v", err)
	}
	if !strings.Contains(line, "Cursor") {
		t.Errorf("rendered line = %q, want Cursor prefix", line)
	}

	// Verify main file was written
	if !fileExists(mainPath) {
		t.Fatalf("main status file %q not created", mainPath)
	}

	// Verify container box file was routed
	physicsPath := filepath.Join(dir, "cursor-physics-status.json")
	if !fileExists(physicsPath) {
		t.Errorf("container routed status file %q not created", physicsPath)
	}

	// Verify slug file was routed
	slugPath := filepath.Join(dir, "cursor-physicsxd2izi-status.json")
	if !fileExists(slugPath) {
		t.Errorf("slug routed status file %q not created", slugPath)
	}
}

func TestRenderStatusLine_FormatsCorrectly(t *testing.T) {
	raw := []byte(`{
		"model": {"display_name": "Grok 4.6", "param_summary": "High"},
		"context_window": {"used_percentage": 42.0}
	}`)
	rendered := RenderStatusLine(raw)
	if want := "Cursor · Grok 4.6 (High) · context 42%"; rendered != want {
		t.Errorf("RenderStatusLine() = %q, want %q", rendered, want)
	}
}

func TestRenderStatusLine_MalformedFallsBack(t *testing.T) {
	rendered := RenderStatusLine([]byte(`{malformed`))
	if rendered != "Cursor" {
		t.Errorf("RenderStatusLine() = %q, want fallback 'Cursor'", rendered)
	}
}
