package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectKimiCLI_None(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())

	var result Result
	detectKimiCLI(&result)

	for _, a := range result.Accounts {
		if a.ID == "kimi_cli" {
			t.Errorf("expected no kimi_cli account; got %+v", a)
		}
	}
}

func TestDetectKimiCLI_FromSessionsDir(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())

	if err := os.MkdirAll(filepath.Join(home, ".kimi", "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var result Result
	detectKimiCLI(&result)

	var found bool
	for _, a := range result.Accounts {
		if a.ID == "kimi_cli" && a.Provider == "kimi_cli" {
			found = true
			if got := a.Path("sessions_dir", ""); got != filepath.Join(home, ".kimi", "sessions") {
				t.Errorf("sessions_dir = %q, want %q", got, filepath.Join(home, ".kimi", "sessions"))
			}
		}
	}
	if !found {
		t.Errorf("expected kimi_cli account; accounts=%+v", result.Accounts)
	}
}

func TestDetectKimiCLI_KimiCodeSessionsPairConfig(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())
	oldDir := filepath.Join(home, ".kimi")
	codeDir := filepath.Join(home, ".kimi-code")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(codeDir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	codeConfig := filepath.Join(codeDir, "config.json")
	if err := os.WriteFile(codeConfig, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	var result Result
	detectKimiCLI(&result)
	if len(result.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(result.Accounts))
	}
	acct := result.Accounts[0]
	if got := acct.Path("sessions_dir", ""); got != filepath.Join(codeDir, "sessions") {
		t.Errorf("sessions_dir = %q", got)
	}
	if got := acct.Path("config_path", ""); got != codeConfig {
		t.Errorf("config_path = %q, want %q", got, codeConfig)
	}

	if err := os.MkdirAll(filepath.Join(oldDir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	result = Result{}
	detectKimiCLI(&result)
	if len(result.Accounts) != 1 || result.Accounts[0].Path("sessions_dir", "") != filepath.Join(oldDir, "sessions") {
		t.Errorf("original CLI should win when both session directories exist: %+v", result.Accounts)
	}
}

func TestDetectKimiCLI_FromConfigFile(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())

	if err := os.MkdirAll(filepath.Join(home, ".kimi"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := filepath.Join(home, ".kimi", "config.json")
	if err := os.WriteFile(cfg, []byte(`{"model":"kimi-k2"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var result Result
	detectKimiCLI(&result)

	var found bool
	for _, a := range result.Accounts {
		if a.ID == "kimi_cli" {
			found = true
			if got := a.Path("config_path", ""); got != cfg {
				t.Errorf("config_path = %q, want %q", got, cfg)
			}
		}
	}
	if !found {
		t.Errorf("expected kimi_cli account from config.json alone; accounts=%+v", result.Accounts)
	}
}

func TestDetectKimiCLI_DoesNotCollideWithMoonshot(t *testing.T) {
	// The API-key MOONSHOT account uses ID "moonshot-ai". The local
	// Kimi CLI account must use a distinct ID so both coexist.
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())

	if err := os.MkdirAll(filepath.Join(home, ".kimi", "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var result Result
	detectKimiCLI(&result)

	for _, a := range result.Accounts {
		if a.ID == "moonshot-ai" {
			t.Errorf("kimi_cli detector must not produce moonshot-ai account; got %+v", a)
		}
	}
}

func TestDetectKimiCLI_FromBinaryOnPATH(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "kimi")
	t.Setenv("PATH", binDir)
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", binDir)

	var result Result
	detectKimiCLI(&result)

	var found bool
	for _, a := range result.Accounts {
		if a.ID == "kimi_cli" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected kimi_cli account from binary alone; accounts=%+v", result.Accounts)
	}

	var toolFound bool
	for _, tool := range result.Tools {
		if tool.Name == "Kimi CLI" {
			toolFound = true
		}
	}
	if !toolFound {
		t.Errorf("expected Kimi CLI tool entry; tools=%+v", result.Tools)
	}
}
