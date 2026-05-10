package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withAiderHome rewires HOME to a fresh temp dir, drops a fake `aider` binary
// onto PATH (so detectAiderConfig's "Aider installed?" gate passes), and
// clears the env vars our aider detector might compete with so tests don't
// leak. Returns the home dir path.
func withAiderHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "aider"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake aider bin: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir)
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", binDir)
	for _, m := range envKeyMapping {
		t.Setenv(m.EnvVar, "")
	}
	return home
}

// detectAiderConfigForTest runs detectAider so result.Tools is populated,
// then runs detectAiderConfig. Production AutoDetect does this in order; the
// privacy gate in detectAiderConfig requires it.
func detectAiderConfigForTest(result *Result) {
	detectAider(result)
	detectAiderConfig(result)
}

// chdirTo changes cwd for the duration of the test and restores it after.
// detectAiderConfig pulls cwd via os.Getwd, so we need to control it.
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prev)
	})
}

func TestDetectAiderConfig_DedicatedYAMLKeys(t *testing.T) {
	home := withAiderHome(t)
	chdirTo(t, home)

	body := `# my aider config
openai-api-key: sk-aider-yaml-12345
anthropic-api-key: sk-ant-aider-yaml-67890
model: gpt-4o
`
	if err := os.WriteFile(filepath.Join(home, ".aider.conf.yml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var result Result
	detectAiderConfigForTest(&result)

	want := map[string]string{
		"openai":    "sk-aider-yaml-12345",
		"anthropic": "sk-ant-aider-yaml-67890",
	}
	got := map[string]string{}
	for _, a := range result.Accounts {
		got[a.Provider] = a.Token
		if !strings.HasPrefix(a.Hint("credential_source", ""), "aider_yaml:") {
			t.Errorf("%s credential_source = %q, want aider_yaml: prefix", a.Provider, a.Hint("credential_source", ""))
		}
	}
	for provider, want := range want {
		if got[provider] != want {
			t.Errorf("provider %s Token = %q, want %q", provider, got[provider], want)
		}
	}
}

func TestDetectAiderConfig_ListFormKeys(t *testing.T) {
	home := withAiderHome(t)
	chdirTo(t, home)

	body := `api-key:
  - gemini=gem-aider-yaml-12345
  - openrouter=or-aider-yaml-67890
  - deepseek=ds-aider-yaml-abcde
`
	if err := os.WriteFile(filepath.Join(home, ".aider.conf.yml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var result Result
	detectAiderConfigForTest(&result)

	want := map[string]string{
		"gemini_api": "gem-aider-yaml-12345",
		"openrouter": "or-aider-yaml-67890",
		"deepseek":   "ds-aider-yaml-abcde",
	}
	got := map[string]string{}
	for _, a := range result.Accounts {
		got[a.Provider] = a.Token
	}
	for provider, want := range want {
		if got[provider] != want {
			t.Errorf("provider %s Token = %q, want %q (full: %+v)", provider, got[provider], want, got)
		}
	}
}

func TestDetectAiderConfig_DotenvKeys(t *testing.T) {
	home := withAiderHome(t)
	chdirTo(t, home)

	body := `# project secrets
OPENAI_API_KEY=sk-from-dotenv-12345
GROQ_API_KEY="gsk-quoted-67890"
`
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var result Result
	detectAiderConfigForTest(&result)

	got := map[string]string{}
	for _, a := range result.Accounts {
		got[a.Provider] = a.Token
		if !strings.HasPrefix(a.Hint("credential_source", ""), "aider_dotenv:") {
			t.Errorf("%s credential_source = %q, want aider_dotenv: prefix", a.Provider, a.Hint("credential_source", ""))
		}
	}
	if got["openai"] != "sk-from-dotenv-12345" {
		t.Errorf("openai Token = %q", got["openai"])
	}
	if got["groq"] != "gsk-quoted-67890" {
		t.Errorf("groq Token = %q", got["groq"])
	}
}

func TestDetectAiderConfig_EnvVarBeatsFile(t *testing.T) {
	home := withAiderHome(t)
	chdirTo(t, home)

	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("OPENAI_API_KEY=sk-from-file\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("OPENAI_API_KEY", "sk-from-env-12345")

	var result Result
	detectAiderConfigForTest(&result)

	for _, a := range result.Accounts {
		if a.Provider == "openai" {
			t.Fatalf("env should win; got file-derived account: %+v", a)
		}
	}
}

func TestDetectAiderConfig_CwdConfigBeatsHome(t *testing.T) {
	home := withAiderHome(t)
	project := t.TempDir()
	chdirTo(t, project)

	// home config has one key, project config has another for the same provider.
	if err := os.WriteFile(filepath.Join(home, ".aider.conf.yml"),
		[]byte("openai-api-key: sk-from-home-12345\n"), 0o600); err != nil {
		t.Fatalf("write home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, ".aider.conf.yml"),
		[]byte("openai-api-key: sk-from-project-67890\n"), 0o600); err != nil {
		t.Fatalf("write project: %v", err)
	}

	var result Result
	detectAiderConfigForTest(&result)

	var openai string
	for _, a := range result.Accounts {
		if a.Provider == "openai" {
			openai = a.Token
		}
	}
	if openai != "sk-from-project-67890" {
		t.Errorf("openai Token = %q, want sk-from-project-67890 (cwd should beat home)", openai)
	}
}

func TestDetectAiderConfig_GitRootConfig(t *testing.T) {
	home := withAiderHome(t)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	subdir := filepath.Join(repo, "src", "deep")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	chdirTo(t, subdir)

	if err := os.WriteFile(filepath.Join(repo, ".aider.conf.yml"),
		[]byte("openai-api-key: sk-from-git-root-12345\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = home

	var result Result
	detectAiderConfigForTest(&result)

	var openai string
	for _, a := range result.Accounts {
		if a.Provider == "openai" {
			openai = a.Token
		}
	}
	if openai != "sk-from-git-root-12345" {
		t.Errorf("openai Token = %q, want sk-from-git-root-12345 (git-root should be searched)", openai)
	}
}

func TestDetectAiderConfig_DotenvBeatsHomeYAML(t *testing.T) {
	// Aider treats .aider.conf.yml and .env as equivalent at the same scope,
	// with deeper scopes overriding shallower ones. cwd/.env must beat
	// home/.aider.conf.yml — earlier code processed all YAML before any
	// .env, which broke this.
	home := withAiderHome(t)
	project := t.TempDir()
	chdirTo(t, project)

	if err := os.WriteFile(filepath.Join(home, ".aider.conf.yml"),
		[]byte("openai-api-key: sk-from-home-yaml-12345\n"), 0o600); err != nil {
		t.Fatalf("write home yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, ".env"),
		[]byte("OPENAI_API_KEY=sk-from-project-env-67890\n"), 0o600); err != nil {
		t.Fatalf("write project env: %v", err)
	}

	var result Result
	detectAiderConfigForTest(&result)

	var openai string
	for _, a := range result.Accounts {
		if a.Provider == "openai" {
			openai = a.Token
		}
	}
	if openai != "sk-from-project-env-67890" {
		t.Errorf("openai Token = %q, want sk-from-project-env-67890 (cwd/.env must beat home/.aider.conf.yml)", openai)
	}
}

func TestDetectAiderConfig_NotInstalledIsNoOp(t *testing.T) {
	// Privacy gate: if Aider isn't installed, .env files in cwd/git-root must
	// NOT be scanned even if they exist with our known env-var names.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", "")
	for _, m := range envKeyMapping {
		t.Setenv(m.EnvVar, "")
	}
	if err := os.WriteFile(filepath.Join(home, ".env"),
		[]byte("OPENAI_API_KEY=sk-private-do-not-adopt\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	chdirTo(t, home)

	var result Result
	detectAiderConfig(&result) // direct call — no detectAider runs first

	for _, a := range result.Accounts {
		if a.Provider == "openai" {
			t.Errorf("Aider not installed; .env should not have been adopted, got %+v", a)
		}
	}
}

func TestDetectAiderConfig_NoConfigIsSafe(t *testing.T) {
	withAiderHome(t)
	chdirTo(t, t.TempDir())

	var result Result
	detectAiderConfigForTest(&result) // must not panic
	if len(result.Accounts) != 0 {
		t.Errorf("expected 0 accounts with no config, got %+v", result.Accounts)
	}
}

func TestDetectAiderConfig_MalformedYAMLIsSafe(t *testing.T) {
	home := withAiderHome(t)
	chdirTo(t, home)

	if err := os.WriteFile(filepath.Join(home, ".aider.conf.yml"), []byte("not: valid: yaml: ::"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var result Result
	detectAiderConfigForTest(&result) // must not panic
}
