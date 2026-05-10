package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/detect"
)

// MaskKey behaviour is covered in internal/detect/mask_test.go. The test
// below just smoke-tests that maskKey is reachable through the report path.

func TestPrintDetectReport_RendersAccountsAndMissing(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	a1 := core.AccountConfig{
		ID:        "openai",
		Provider:  "openai",
		Auth:      "api_key",
		APIKeyEnv: "OPENAI_API_KEY",
		Token:     "sk-test1234567890abcdef",
	}
	a1.SetHint("credential_source", "shell_rc:/home/u/.zshrc")

	a2 := core.AccountConfig{
		ID:        "anthropic",
		Provider:  "anthropic",
		Auth:      "api_key",
		APIKeyEnv: "ANTHROPIC_API_KEY",
	}

	result := detect.Result{
		Tools: []detect.DetectedTool{
			{Name: "Cursor IDE", Type: "ide", BinaryPath: "/usr/local/bin/cursor"},
		},
		Accounts: []core.AccountConfig{a1, a2},
	}

	var buf bytes.Buffer
	if err := printDetectReport(&buf, result, false); err != nil {
		t.Fatalf("printDetectReport: %v", err)
	}
	out := buf.String()

	mustContain := []string{
		"Tools detected:",
		"Cursor IDE",
		"/usr/local/bin/cursor",
		"Accounts detected:",
		"openai",
		"sk-t...cdef",
		"shell_rc:/home/u/.zshrc",
		"$ANTHROPIC_API_KEY (unset)",
		"No credentials found for:",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}

	// Tokens must NOT appear in clear text.
	if strings.Contains(out, "sk-test1234567890abcdef") {
		t.Errorf("clear-text token leaked into report:\n%s", out)
	}
}

func TestPrintDetectReport_EmptyResult(t *testing.T) {
	var buf bytes.Buffer
	if err := printDetectReport(&buf, detect.Result{}, false); err != nil {
		t.Fatalf("printDetectReport: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Tools detected:") || !strings.Contains(out, "(none)") {
		t.Errorf("expected (none) for tools, got:\n%s", out)
	}
	if !strings.Contains(out, "Accounts detected:") || !strings.Contains(out, "(none)") {
		t.Errorf("expected (none) for accounts, got:\n%s", out)
	}
	// With no accounts, every registered provider should be in the missing list.
	if !strings.Contains(out, "No credentials found for:") {
		t.Errorf("expected missing-providers section, got:\n%s", out)
	}
	if !strings.Contains(out, "openai") || !strings.Contains(out, "anthropic") {
		t.Errorf("expected missing list to include known providers, got:\n%s", out)
	}
}

func TestMaskKeyEndToEnd(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"sk-test1234567890abcdef", "sk-t...cdef"},
		{"short", "****"},
		{"", "****"},
		{"   sk-test1234567890   ", "sk-t...7890"},
	}
	for _, tc := range cases {
		if got := detect.MaskKey(tc.in); got != tc.want {
			t.Errorf("detect.MaskKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
