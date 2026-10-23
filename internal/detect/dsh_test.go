package detect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func findDshAccount(result Result) (core.AccountConfig, bool) {
	for _, a := range result.Accounts {
		if a.ID == "dsh" && a.Provider == "dsh" {
			return a, true
		}
	}
	return core.AccountConfig{}, false
}

func TestDetectDsh_None(t *testing.T) {
	setHome(t, t.TempDir())
	t.Setenv("DSH_HOME", "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())

	var result Result
	detectDsh(&result)

	if acct, ok := findDshAccount(result); ok {
		t.Errorf("expected no dsh account; got %+v", acct)
	}
}

func TestDetectDsh_BinaryAloneIsNotEnough(t *testing.T) {
	setHome(t, t.TempDir())
	t.Setenv("DSH_HOME", "")
	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "dsh")
	t.Setenv("PATH", binDir)
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())

	var result Result
	detectDsh(&result)

	if acct, ok := findDshAccount(result); ok {
		t.Errorf("expected no dsh account without a sessions dir; got %+v", acct)
	}
	if len(result.Tools) != 0 {
		t.Errorf("expected no detected tools; got %+v", result.Tools)
	}
}

func TestDetectDsh_FromSessionsDir(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("DSH_HOME", "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())

	sessionsDir := filepath.Join(home, ".dsh", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var result Result
	detectDsh(&result)

	acct, ok := findDshAccount(result)
	if !ok {
		t.Fatalf("expected dsh account; accounts=%+v", result.Accounts)
	}
	if acct.Auth != "local" {
		t.Errorf("auth = %q, want local", acct.Auth)
	}
	if got := acct.Path("sessions_dir", ""); got != sessionsDir {
		t.Errorf("sessions_dir = %q, want %q", got, sessionsDir)
	}
	if got := acct.Hint("data_dir", ""); got != filepath.Join(home, ".dsh") {
		t.Errorf("data_dir = %q, want %q", got, filepath.Join(home, ".dsh"))
	}
}

func TestDetectDsh_HonoursDshHome(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())

	// The default location exists too, and must lose to DSH_HOME.
	if err := os.MkdirAll(filepath.Join(home, ".dsh", "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	relocated := filepath.Join(t.TempDir(), "dsh-home")
	sessionsDir := filepath.Join(relocated, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("DSH_HOME", relocated)

	var result Result
	detectDsh(&result)

	acct, ok := findDshAccount(result)
	if !ok {
		t.Fatalf("expected dsh account; accounts=%+v", result.Accounts)
	}
	if got := acct.Path("sessions_dir", ""); got != sessionsDir {
		t.Errorf("sessions_dir = %q, want %q", got, sessionsDir)
	}
}

func TestDetectDsh_RecordsBinary(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("DSH_HOME", "")
	binDir := t.TempDir()
	bin := writeFakeBinary(t, binDir, "dsh")
	t.Setenv("PATH", binDir)
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())

	if err := os.MkdirAll(filepath.Join(home, ".dsh", "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var result Result
	detectDsh(&result)

	acct, ok := findDshAccount(result)
	if !ok {
		t.Fatalf("expected dsh account; accounts=%+v", result.Accounts)
	}
	if acct.Binary != bin {
		t.Errorf("binary = %q, want %q", acct.Binary, bin)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "dsh" {
		t.Errorf("tools = %+v, want one dsh entry", result.Tools)
	}
}
