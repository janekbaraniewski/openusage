package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentServiceEnvSnapshot_IncludesKnownConfiguredVars(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-openai")
	t.Setenv("ZEN_API_KEY", "sk-zen")
	t.Setenv("UNRELATED_ENV", "ignore-me")

	env := currentServiceEnvSnapshot()

	if env["OPENAI_API_KEY"] != "sk-openai" {
		t.Fatalf("OPENAI_API_KEY = %q, want sk-openai", env["OPENAI_API_KEY"])
	}
	if env["ZEN_API_KEY"] != "sk-zen" {
		t.Fatalf("ZEN_API_KEY = %q, want sk-zen", env["ZEN_API_KEY"])
	}
	if _, ok := env["UNRELATED_ENV"]; ok {
		t.Fatal("unexpected unrelated env var in snapshot")
	}
}

func TestWriteServiceEnvFile_WritesQuotedSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.env")
	err := writeServiceEnvFile(path, map[string]string{
		"OPENAI_API_KEY": "sk-openai",
		"OLLAMA_HOST":    "http://127.0.0.1:11434",
	})
	if err != nil {
		t.Fatalf("writeServiceEnvFile error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `OPENAI_API_KEY="sk-openai"`) {
		t.Fatalf("env file missing quoted OPENAI_API_KEY:\n%s", text)
	}
	if !strings.Contains(text, `OLLAMA_HOST="http://127.0.0.1:11434"`) {
		t.Fatalf("env file missing quoted OLLAMA_HOST:\n%s", text)
	}
}
