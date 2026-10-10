package detect

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Daemon PATH under launchd/systemd lacks nvm dirs; findBinary must still
// locate an nvm-installed CLI, preferring the newest node version.
func TestFindBinary_NvmInstallWithMinimalPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("nvm layout is POSIX-only")
	}
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("NVM_DIR", "")
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", "") // registers restore
	if err := os.Unsetenv("OPENUSAGE_DETECT_BIN_DIRS"); err != nil {
		t.Fatal(err)
	}
	name := "openusage-fake-codex-379"
	var want string
	for _, v := range []string{"v9.11.2", "v22.3.0", "v18.20.1"} {
		dir := filepath.Join(home, ".nvm", "versions", "node", v, "bin")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		p := writeFakeBinary(t, dir, name)
		if v == "v22.3.0" {
			want = p
		}
	}

	if got := findBinary(name); got != want {
		t.Fatalf("findBinary = %q, want %q", got, want)
	}
}

func TestFindBinary_NpmGlobalPrefixWithMinimalPath(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("NVM_DIR", "")
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", "") // registers restore
	if err := os.Unsetenv("OPENUSAGE_DETECT_BIN_DIRS"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".npm-global", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := writeFakeBinary(t, dir, "openusage-fake-gemini-379")
	if got := findBinary("openusage-fake-gemini-379"); got != want {
		t.Fatalf("findBinary = %q, want %q", got, want)
	}
}
