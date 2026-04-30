package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")

	in := BrowserSession{
		Domain:        ".opencode.ai",
		CookieName:    "auth",
		Value:         "encrypted-jwt-blob-here",
		SourceBrowser: "chrome",
		CapturedAt:    "2026-04-30T12:34:56Z",
		ExpiresAt:     "2026-05-30T12:34:56Z",
	}
	if err := SaveSessionTo(path, "opencode-console", in); err != nil {
		t.Fatalf("SaveSessionTo error: %v", err)
	}

	got, ok, err := LoadSessionFrom(path, "opencode-console")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("session not found after save")
	}
	if got != in {
		t.Errorf("round-trip = %+v, want %+v", got, in)
	}
}

func TestSaveSession_RejectsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")

	cases := []struct {
		name    string
		acct    string
		session BrowserSession
	}{
		{
			name:    "empty value",
			acct:    "x",
			session: BrowserSession{Domain: ".example.com", CookieName: "auth"},
		},
		{
			name:    "empty domain",
			acct:    "x",
			session: BrowserSession{Value: "v", CookieName: "auth"},
		},
		{
			name:    "empty cookie name",
			acct:    "x",
			session: BrowserSession{Value: "v", Domain: ".example.com"},
		},
		{
			name:    "empty account",
			acct:    "",
			session: BrowserSession{Value: "v", Domain: ".example.com", CookieName: "auth"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := SaveSessionTo(path, tc.acct, tc.session); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestDeleteSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")

	if err := SaveSessionTo(path, "opencode-console", BrowserSession{
		Domain: ".opencode.ai", CookieName: "auth", Value: "v",
	}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSessionFrom(path, "opencode-console"); err != nil {
		t.Fatal(err)
	}
	_, ok, err := LoadSessionFrom(path, "opencode-console")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("session still present after delete")
	}
}

// Sessions and Keys must coexist — saving a session must not blow away
// existing API-key credentials, and vice versa.
func TestSession_CoexistsWithKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")

	if err := SaveCredentialTo(path, "openai", "sk-test"); err != nil {
		t.Fatal(err)
	}
	if err := SaveSessionTo(path, "opencode-console", BrowserSession{
		Domain: ".opencode.ai", CookieName: "auth", Value: "v",
	}); err != nil {
		t.Fatal(err)
	}

	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Keys["openai"] != "sk-test" {
		t.Errorf("api key lost after session save: %q", creds.Keys["openai"])
	}
	if creds.Sessions["opencode-console"].Value != "v" {
		t.Errorf("session lost: %+v", creds.Sessions["opencode-console"])
	}
}

// Loading a credentials file written before this change (only "keys",
// no "sessions") must succeed and produce an empty sessions map — no
// surprises for users upgrading.
func TestLoadCredentials_LegacyFileMissingSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	legacy := `{"keys":{"openai":"sk-test"}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Keys["openai"] != "sk-test" {
		t.Errorf("legacy api key lost: %q", creds.Keys["openai"])
	}
	if creds.Sessions == nil {
		t.Fatal("Sessions map nil after legacy load — should be initialized")
	}
	if len(creds.Sessions) != 0 {
		t.Errorf("Sessions should be empty for legacy file, got %d", len(creds.Sessions))
	}
}

// File serialization must omit the empty sessions map so legacy consumers
// (or hand-edited files) don't see unfamiliar fields.
func TestSaveCredentials_OmitsEmptySessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := SaveCredentialTo(path, "openai", "sk-test"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatal(err)
	}
	if _, ok := generic["sessions"]; ok {
		t.Errorf("empty sessions present in serialization: %s", data)
	}
}

// File permissions must be 0o600 — same as before, the new field doesn't
// change the security posture.
func TestSaveSession_FilePermsAre0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := SaveSessionTo(path, "opencode-console", BrowserSession{
		Domain: ".opencode.ai", CookieName: "auth", Value: "v",
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("perms = %o, want 0o600", got)
	}
}
