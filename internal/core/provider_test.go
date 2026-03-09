package core

import "testing"

func TestAccountConfigNormalizeRuntimePaths(t *testing.T) {
	tests := []struct {
		name     string
		account  AccountConfig
		wantKey  string
		wantPath string
	}{
		{
			name: "cursor migrates legacy db fields",
			account: AccountConfig{
				Provider: "cursor",
				Binary:   "/tmp/tracking.db",
				BaseURL:  "/tmp/state.vscdb",
			},
			wantKey:  "tracking_db",
			wantPath: "/tmp/tracking.db",
		},
		{
			name: "claude migrates legacy config fields",
			account: AccountConfig{
				Provider: "claude_code",
				Binary:   "/tmp/stats-cache.json",
				BaseURL:  "/tmp/.claude.json",
			},
			wantKey:  "stats_cache",
			wantPath: "/tmp/stats-cache.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acct := tt.account
			acct.NormalizeRuntimePaths()
			if got := acct.Path(tt.wantKey, ""); got != tt.wantPath {
				t.Fatalf("Path(%q) = %q, want %q", tt.wantKey, got, tt.wantPath)
			}
		})
	}
}
