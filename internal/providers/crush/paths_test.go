package crush

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultSearchRoots_ExcludesProtectedPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	roots := defaultSearchRoots()
	for _, r := range roots {
		switch r {
		case home:
			t.Errorf("$HOME (%q) is in default search roots; walking it triggers macOS TCC prompts", r)
		case filepath.Join(home, "Documents"):
			t.Errorf("~/Documents is in default search roots; iCloud Desktop & Documents Sync makes it TCC-protected on macOS")
		}
	}
}

func TestIsSkippableDirName_SkipsProtectedDirs(t *testing.T) {
	protected := []string{
		"Library", "Pictures", "Movies", "Music", "Desktop",
		"Public", "Applications", ".Trash",
		"My Photos.photoslibrary",
		"Photos Library.photoslibrary",
	}
	for _, name := range protected {
		if !isSkippableDirName(name) {
			t.Errorf("%q must be skipped to avoid macOS TCC prompts", name)
		}
	}
}

func TestIsSkippableDirName_AllowsProjectDirs(t *testing.T) {
	projectish := []string{"code", "src", "projects", "Workspace", "openusage", "my-app"}
	for _, name := range projectish {
		if isSkippableDirName(name) {
			t.Errorf("%q should not be skipped — it could hold a project clone", name)
		}
	}
}

func TestDiscoverDBs_SkipsProtectedSiblingDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test models macOS-shaped layouts")
	}
	tmp := t.TempDir()
	// A real project clone the walker should find.
	good := filepath.Join(tmp, "code", "proj", ".crush")
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(good, "crush.db"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A landmine that, if descended into, would trigger TCC on a real system.
	landmine := filepath.Join(tmp, "Pictures", "Photos Library.photoslibrary", ".crush")
	if err := os.MkdirAll(landmine, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(landmine, "crush.db"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := discoverDBs([]string{tmp}, 6)
	if len(got) != 1 {
		t.Fatalf("expected exactly one DB, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], filepath.Join("code", "proj", ".crush", "crush.db")) {
		t.Errorf("walker found wrong DB: %q", got[0])
	}
	for _, p := range got {
		if strings.Contains(p, "Pictures") || strings.Contains(p, ".photoslibrary") {
			t.Errorf("walker descended into a protected dir: %q", p)
		}
	}
}
