package core

import (
	"encoding/json"
	"testing"
)

// AccountConfig.Options is the persisted, provider-scoped bag for scalar
// configuration knobs (design: docs/proposals/provider-options-and-gh-hosts.md).
// Unlike Path, Option must NOT fall through to RuntimeHints.
func TestAccountConfigOption(t *testing.T) {
	acct := AccountConfig{ID: "x"}
	if got := acct.Option("gh_host", ""); got != "" {
		t.Fatalf("Option() unset = %q, want fallback %q", got, "")
	}
	if got := acct.Option("gh_host", "dflt"); got != "dflt" {
		t.Fatalf("Option() unset = %q, want fallback", got)
	}

	acct.SetOption("gh_host", "acme.ghe.com")
	if got := acct.Option("gh_host", ""); got != "acme.ghe.com" {
		t.Fatalf("Option() = %q, want %q", got, "acme.ghe.com")
	}

	// Blank values are unset: fall back instead of returning whitespace.
	acct.Options["gh_host"] = "   "
	if got := acct.Option("gh_host", "dflt"); got != "dflt" {
		t.Fatalf("Option() blank = %q, want fallback", got)
	}

	// RuntimeHints must not leak into Option: transient detection state is
	// not persisted configuration.
	acct.Options = nil
	acct.SetHint("gh_host", "acme.ghe.com")
	if got := acct.Option("gh_host", "dflt"); got != "dflt" {
		t.Fatalf("Option() leaked RuntimeHints value %q", got)
	}
}

func TestAccountConfigSetOptionGuards(t *testing.T) {
	var nilAcct *AccountConfig
	nilAcct.SetOption("k", "v") // must not panic

	acct := AccountConfig{}
	acct.SetOption("", "v")
	acct.SetOption("k", "")
	acct.SetOption("  ", "  ")
	if len(acct.Options) != 0 {
		t.Fatalf("SetOption stored blank key/value: %#v", acct.Options)
	}

	acct.SetOption("  k  ", "  v  ")
	if got := acct.Option("k", ""); got != "v" {
		t.Fatalf("SetOption should trim, got %q", got)
	}
}

func TestAccountConfigOptionsPersistedJSON(t *testing.T) {
	acct := AccountConfig{ID: "copilot-acme", Provider: "copilot", Auth: "cli"}
	acct.SetOption("gh_host", "acme.ghe.com")
	raw, err := json.Marshal(acct)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back AccountConfig
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got := back.Option("gh_host", ""); got != "acme.ghe.com" {
		t.Fatalf("options did not survive JSON round-trip: %s", raw)
	}
}
