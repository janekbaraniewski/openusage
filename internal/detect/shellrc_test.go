package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseExportLine(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantOK  bool
		wantVar string
		wantVal string
	}{
		{"plain export", `export OPENAI_API_KEY=sk-abc123`, true, "OPENAI_API_KEY", "sk-abc123"},
		{"single-quoted", `export OPENAI_API_KEY='sk-abc123'`, true, "OPENAI_API_KEY", "sk-abc123"},
		{"double-quoted", `export OPENAI_API_KEY="sk-abc123"`, true, "OPENAI_API_KEY", "sk-abc123"},
		{"posix-form", `OPENAI_API_KEY=sk-abc123`, true, "OPENAI_API_KEY", "sk-abc123"},
		{"with trailing comment", `export OPENAI_API_KEY=sk-abc123 # work`, true, "OPENAI_API_KEY", "sk-abc123"},
		{"fish set -gx", `set -gx OPENAI_API_KEY sk-abc123`, true, "OPENAI_API_KEY", "sk-abc123"},
		{"fish set -x", `set -x OPENAI_API_KEY sk-abc123`, true, "OPENAI_API_KEY", "sk-abc123"},
		{"comment line", `# export OPENAI_API_KEY=sk`, false, "", ""},
		{"blank", ``, false, "", ""},
		{"command sub rejected", `export OPENAI_API_KEY=$(cat /tmp/k)`, false, "", ""},
		{"backtick sub rejected", "export OPENAI_API_KEY=`cat /tmp/k`", false, "", ""},
		{"var sub rejected", `export OPENAI_API_KEY=$ANOTHER`, false, "", ""},
		{"var sub in dquote rejected", `export OPENAI_API_KEY="$ANOTHER"`, false, "", ""},
		{"empty value rejected", `export OPENAI_API_KEY=`, false, "", ""},
		{"unquoted whitespace rejected", `export OPENAI_API_KEY=hello world`, false, "", ""},
		{"fish without -x flag rejected", `set -g OPENAI_API_KEY sk-abc`, false, "", ""},
		{"odd whitespace tabs", "export\tOPENAI_API_KEY=sk-abc", true, "OPENAI_API_KEY", "sk-abc"},
		{"single-quote preserves spaces", `export OPENAI_API_KEY='sk has space'`, true, "OPENAI_API_KEY", "sk has space"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotV, gotVal, ok := parseExportLine(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("parseExportLine(%q) ok = %v, want %v (got name=%q value=%q)", tc.in, ok, tc.wantOK, gotV, gotVal)
			}
			if !ok {
				return
			}
			if gotV != tc.wantVar {
				t.Errorf("name = %q, want %q", gotV, tc.wantVar)
			}
			if gotVal != tc.wantVal {
				t.Errorf("value = %q, want %q", gotVal, tc.wantVal)
			}
		})
	}
}

func TestDetectShellRC_FindsKeyInZshrc(t *testing.T) {
	home := t.TempDir()
	zshrc := filepath.Join(home, ".zshrc")
	body := `# my zshrc
export PATH="/usr/local/bin:$PATH"
export OPENAI_API_KEY=sk-from-zshrc-1234567890
`
	if err := os.WriteFile(zshrc, []byte(body), 0o600); err != nil {
		t.Fatalf("write zshrc: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("OPENAI_API_KEY", "")

	var result Result
	detectShellRC(&result)

	var found bool
	for _, a := range result.Accounts {
		if a.Provider != "openai" {
			continue
		}
		found = true
		if a.Token != "sk-from-zshrc-1234567890" {
			t.Errorf("Token = %q, want sk-from-zshrc-1234567890", a.Token)
		}
		if got := a.Hint("credential_source", ""); !strings.HasPrefix(got, "shell_rc:") {
			t.Errorf("credential_source = %q, want shell_rc: prefix", got)
		}
		if !strings.HasSuffix(a.Hint("credential_source", ""), ".zshrc") {
			t.Errorf("credential_source should point at .zshrc, got %q", a.Hint("credential_source", ""))
		}
	}
	if !found {
		t.Fatalf("expected openai account from zshrc, got %+v", result.Accounts)
	}
}

func TestDetectShellRC_FindsKeyInZshrcD(t *testing.T) {
	home := t.TempDir()
	dropin := filepath.Join(home, ".zshrc.d", "09-claude.zsh")
	if err := os.MkdirAll(filepath.Dir(dropin), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dropin, []byte(`export ANTHROPIC_API_KEY="sk-ant-from-dropin-12345"`+"\n"), 0o600); err != nil {
		t.Fatalf("write dropin: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("ANTHROPIC_API_KEY", "")

	var result Result
	detectShellRC(&result)

	var found bool
	for _, a := range result.Accounts {
		if a.Provider == "anthropic" && a.Token == "sk-ant-from-dropin-12345" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected anthropic account from .zshrc.d/, got %+v", result.Accounts)
	}
}

func TestDetectShellRC_FindsFishKey(t *testing.T) {
	home := t.TempDir()
	fish := filepath.Join(home, ".config", "fish", "config.fish")
	if err := os.MkdirAll(filepath.Dir(fish), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fish, []byte(`set -gx GROQ_API_KEY gsk-fish-1234567890`+"\n"), 0o600); err != nil {
		t.Fatalf("write fish: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("GROQ_API_KEY", "")

	var result Result
	detectShellRC(&result)

	var found bool
	for _, a := range result.Accounts {
		if a.Provider == "groq" && a.Token == "gsk-fish-1234567890" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected groq account from fish config, got %+v", result.Accounts)
	}
}

func TestDetectShellRC_EnvVarBeatsFile(t *testing.T) {
	home := t.TempDir()
	zshrc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(zshrc, []byte(`export OPENAI_API_KEY=sk-from-file`+"\n"), 0o600); err != nil {
		t.Fatalf("write zshrc: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("OPENAI_API_KEY", "sk-from-env")

	var result Result
	detectShellRC(&result)

	for _, a := range result.Accounts {
		if a.Provider == "openai" {
			t.Fatalf("env var was set; detectShellRC should have skipped, got %+v", a)
		}
	}
}

func TestDetectShellRC_IgnoresUnknownVars(t *testing.T) {
	home := t.TempDir()
	zshrc := filepath.Join(home, ".zshrc")
	body := `export FOO=bar
export NOT_AN_AI_KEY=garbage
export OPENAI_API_KEY=sk-real-1234567890
`
	if err := os.WriteFile(zshrc, []byte(body), 0o600); err != nil {
		t.Fatalf("write zshrc: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("OPENAI_API_KEY", "")

	var result Result
	detectShellRC(&result)

	if len(result.Accounts) != 1 {
		t.Fatalf("expected 1 account from %d known-var lines, got %d: %+v", 1, len(result.Accounts), result.Accounts)
	}
}

func TestDetectShellRC_HomeUnsetIsSafe(t *testing.T) {
	t.Setenv("HOME", "")

	var result Result
	detectShellRC(&result) // must not panic
	if len(result.Accounts) != 0 {
		t.Errorf("expected 0 accounts with empty HOME, got %+v", result.Accounts)
	}
}

func TestDetectShellRC_DoesNotDoubleCount(t *testing.T) {
	// Same key in two files: only one account should result.
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(`export OPENAI_API_KEY=sk-zshrc-12345`+"\n"), 0o600); err != nil {
		t.Fatalf("write zshrc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".profile"), []byte(`export OPENAI_API_KEY=sk-profile-67890`+"\n"), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("OPENAI_API_KEY", "")

	var result Result
	detectShellRC(&result)

	count := 0
	for _, a := range result.Accounts {
		if a.Provider == "openai" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 openai account when key is in two files, got %d: %+v", count, result.Accounts)
	}
}
