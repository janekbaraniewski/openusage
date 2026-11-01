package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGHCopilotInstallPath(t *testing.T) {
	tests := []struct {
		name string
		// setup returns the directory that should be reported, or "".
		setup func(t *testing.T, home string) string
	}{
		{
			name:  "nothing installed",
			setup: func(*testing.T, string) string { return "" },
		},
		{
			name: "legacy extension in default data dir",
			setup: func(t *testing.T, home string) string {
				return mkdirAll(t, filepath.Join(home, ".local", "share", "gh", "extensions", "gh-copilot"))
			},
		},
		{
			name: "copilot CLI downloaded by gh",
			setup: func(t *testing.T, home string) string {
				return mkdirAll(t, filepath.Join(home, ".local", "share", "gh", "copilot"))
			},
		},
		{
			name: "XDG_DATA_HOME",
			setup: func(t *testing.T, home string) string {
				xdg := filepath.Join(home, "xdg")
				t.Setenv("XDG_DATA_HOME", xdg)
				return mkdirAll(t, filepath.Join(xdg, "gh", "extensions", "gh-copilot"))
			},
		},
		{
			name: "GH_DATA_DIR wins over XDG_DATA_HOME",
			setup: func(t *testing.T, home string) string {
				t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))
				mkdirAll(t, filepath.Join(home, "xdg", "gh", "copilot"))
				ghData := filepath.Join(home, "ghdata")
				t.Setenv("GH_DATA_DIR", ghData)
				return mkdirAll(t, filepath.Join(ghData, "copilot"))
			},
		},
		{
			name: "a file is not an install",
			setup: func(t *testing.T, home string) string {
				dir := mkdirAll(t, filepath.Join(home, ".local", "share", "gh"))
				if err := os.WriteFile(filepath.Join(dir, "copilot"), nil, 0o644); err != nil {
					t.Fatal(err)
				}
				return ""
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("GH_DATA_DIR", "")
			t.Setenv("XDG_DATA_HOME", "")
			t.Setenv("LOCALAPPDATA", "") // Windows then falls back to ~/.local/share/gh too
			want := tt.setup(t, home)
			if got := ghCopilotInstallPath(); got != want {
				t.Fatalf("ghCopilotInstallPath() = %q, want %q", got, want)
			}
		})
	}
}

func mkdirAll(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return dir
}
