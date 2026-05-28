package crush

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// writeRegistry writes a projects.json registry under dir and returns
// its path.
func writeRegistry(t *testing.T, dir string, projects []crushProject) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, "projects.json")
	body, err := json.Marshal(crushRegistry{Projects: projects})
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// seedDB creates a fake crush.db file (so fileExists reports true).
func seedDB(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("seeded"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDefaultRegistryPath_HonorsXDGDataHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	want := filepath.Join(tmp, "crush", "projects.json")
	if got := defaultRegistryPath(); got != want {
		t.Errorf("registry path = %q, want %q", got, want)
	}
}

func TestDefaultRegistryPath_FallsBackToLocalShare(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG fallback test is unix-shaped")
	}
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/test/home")
	want := filepath.Join("/test/home", ".local", "share", "crush", "projects.json")
	if got := defaultRegistryPath(); got != want {
		t.Errorf("registry path = %q, want %q", got, want)
	}
}

func TestReadRegistryDBs_ResolvesRelativeAndAbsoluteDataDirs(t *testing.T) {
	tmp := t.TempDir()
	projectA := filepath.Join(tmp, "projects", "alpha")
	projectB := filepath.Join(tmp, "projects", "beta")
	absoluteDataDir := filepath.Join(tmp, "external-data")

	seedDB(t, filepath.Join(projectA, ".crush", "crush.db"))
	seedDB(t, filepath.Join(absoluteDataDir, "crush.db"))

	registry := writeRegistry(t, filepath.Join(tmp, "registry"), []crushProject{
		{Path: projectA, DataDir: ".crush"},
		{Path: projectB, DataDir: absoluteDataDir},
	})

	got := readRegistryDBs(registry)
	if len(got) != 2 {
		t.Fatalf("expected 2 DBs, got %d: %v", len(got), got)
	}
}

func TestReadRegistryDBs_SkipsMissingDBs(t *testing.T) {
	tmp := t.TempDir()
	present := filepath.Join(tmp, "present")
	seedDB(t, filepath.Join(present, ".crush", "crush.db"))

	registry := writeRegistry(t, filepath.Join(tmp, "registry"), []crushProject{
		{Path: present, DataDir: ".crush"},
		{Path: filepath.Join(tmp, "vanished"), DataDir: ".crush"},
	})

	got := readRegistryDBs(registry)
	if len(got) != 1 {
		t.Fatalf("expected 1 DB, got %d: %v", len(got), got)
	}
}

func TestReadRegistryDBs_MissingFileReturnsNil(t *testing.T) {
	if got := readRegistryDBs("/nonexistent/registry.json"); got != nil {
		t.Errorf("missing registry should return nil, got %v", got)
	}
}

func TestReadRegistryDBs_MalformedJSONReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "projects.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := readRegistryDBs(path); got != nil {
		t.Errorf("malformed registry should return nil, got %v", got)
	}
}

func TestResolveDBPaths_HintWins(t *testing.T) {
	tmp := t.TempDir()
	dbA := filepath.Join(tmp, "a", "crush.db")
	dbB := filepath.Join(tmp, "b", "crush.db")
	seedDB(t, dbA)
	seedDB(t, dbB)

	acct := core.AccountConfig{Provider: "crush"}
	acct.SetPath(PathHintDBsKey, dbA+string(os.PathListSeparator)+dbB)

	got := resolveDBPaths(acct)
	if len(got) != 2 {
		t.Fatalf("expected 2 paths from hint, got %d: %v", len(got), got)
	}
}

func TestResolveDBPaths_SingleHintWins(t *testing.T) {
	tmp := t.TempDir()
	db := filepath.Join(tmp, "crush.db")
	seedDB(t, db)

	acct := core.AccountConfig{Provider: "crush"}
	acct.SetPath(PathHintSingleDBKey, db)

	got := resolveDBPaths(acct)
	if len(got) != 1 || got[0] != db {
		t.Errorf("expected [%q], got %v", db, got)
	}
}

func TestResolveDBPaths_FallsThroughToRegistry(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	seedDB(t, filepath.Join(project, ".crush", "crush.db"))

	registry := writeRegistry(t, filepath.Join(tmp, "registry"), []crushProject{
		{Path: project, DataDir: ".crush"},
	})

	acct := core.AccountConfig{Provider: "crush"}
	acct.SetPath(PathHintRegistryKey, registry)

	got := resolveDBPaths(acct)
	if len(got) != 1 {
		t.Fatalf("expected 1 DB from registry, got %d: %v", len(got), got)
	}
}

func TestResolveProjectDB_DefaultsToDotCrush(t *testing.T) {
	got := resolveProjectDB(crushProject{Path: "/abs/project"})
	want := filepath.Join("/abs/project", ".crush", "crush.db")
	if got != want {
		t.Errorf("default data_dir = %q, want %q", got, want)
	}
}

func TestResolveProjectDB_AbsoluteDataDir(t *testing.T) {
	got := resolveProjectDB(crushProject{Path: "/abs/project", DataDir: "/elsewhere"})
	want := filepath.Join("/elsewhere", "crush.db")
	if got != want {
		t.Errorf("absolute data_dir = %q, want %q", got, want)
	}
}
