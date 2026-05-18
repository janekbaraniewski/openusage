package main

import (
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/config"
)

// TestResolveHubRuntime covers the defaulting and auth-token resolution paths
// of resolveHubRuntime. The shape follows detect_test.go: table-driven with
// per-case t.Setenv to control OPENUSAGE_HUB_TOKEN without leaking between
// cases.
func TestResolveHubRuntime(t *testing.T) {
	cases := []struct {
		name           string
		cfg            config.HubConfig
		envToken       string // value to set for OPENUSAGE_HUB_TOKEN ("" → unset)
		wantAddr       string
		wantStaleSecs  int
		wantAuthToken  string
		wantAuthEnable bool
	}{
		{
			name:           "all defaults",
			cfg:            config.HubConfig{},
			wantAddr:       ":9190",
			wantStaleSecs:  300,
			wantAuthToken:  "",
			wantAuthEnable: false,
		},
		{
			name:           "explicit listen_addr is preserved",
			cfg:            config.HubConfig{ListenAddr: "127.0.0.1:8080"},
			wantAddr:       "127.0.0.1:8080",
			wantStaleSecs:  300,
			wantAuthEnable: false,
		},
		{
			name:           "listen_addr is trimmed",
			cfg:            config.HubConfig{ListenAddr: "  :9090  "},
			wantAddr:       ":9090",
			wantStaleSecs:  300,
			wantAuthEnable: false,
		},
		{
			name:           "stale_timeout 0 falls back to 300s",
			cfg:            config.HubConfig{StaleTimeoutSeconds: 0},
			wantAddr:       ":9190",
			wantStaleSecs:  300,
			wantAuthEnable: false,
		},
		{
			name:           "negative stale_timeout falls back to 300s",
			cfg:            config.HubConfig{StaleTimeoutSeconds: -10},
			wantAddr:       ":9190",
			wantStaleSecs:  300,
			wantAuthEnable: false,
		},
		{
			name:           "explicit stale_timeout is preserved",
			cfg:            config.HubConfig{StaleTimeoutSeconds: 60},
			wantAddr:       ":9190",
			wantStaleSecs:  60,
			wantAuthEnable: false,
		},
		{
			name:           "config auth_token wins",
			cfg:            config.HubConfig{AuthToken: "from-config"},
			envToken:       "from-env",
			wantAddr:       ":9190",
			wantStaleSecs:  300,
			wantAuthToken:  "from-config",
			wantAuthEnable: true,
		},
		{
			name:           "env var used when config empty",
			cfg:            config.HubConfig{},
			envToken:       "from-env",
			wantAddr:       ":9190",
			wantStaleSecs:  300,
			wantAuthToken:  "from-env",
			wantAuthEnable: true,
		},
		{
			name:           "auth_token whitespace trimmed",
			cfg:            config.HubConfig{AuthToken: "  spaced-token  "},
			wantAuthToken:  "spaced-token",
			wantAddr:       ":9190",
			wantStaleSecs:  300,
			wantAuthEnable: true,
		},
		{
			name:           "empty config and empty env => no auth",
			cfg:            config.HubConfig{},
			envToken:       "",
			wantAddr:       ":9190",
			wantStaleSecs:  300,
			wantAuthToken:  "",
			wantAuthEnable: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// t.Setenv with "" unsets safely for the duration of the test.
			t.Setenv("OPENUSAGE_HUB_TOKEN", tc.envToken)

			rt := resolveHubRuntime(config.Config{Hub: tc.cfg})

			if rt.addr != tc.wantAddr {
				t.Errorf("addr = %q, want %q", rt.addr, tc.wantAddr)
			}
			gotStaleSecs := int(rt.staleFor.Seconds())
			if gotStaleSecs != tc.wantStaleSecs {
				t.Errorf("staleFor = %ds, want %ds", gotStaleSecs, tc.wantStaleSecs)
			}
			if rt.authToken != tc.wantAuthToken {
				t.Errorf("authToken = %q, want %q", rt.authToken, tc.wantAuthToken)
			}
			if got := rt.server.AuthEnabled(); got != tc.wantAuthEnable {
				t.Errorf("server.AuthEnabled() = %v, want %v", got, tc.wantAuthEnable)
			}
			if rt.store == nil {
				t.Error("store must not be nil")
			}
			if rt.server == nil {
				t.Error("server must not be nil")
			}
		})
	}
}

// TestValidateHubExposure covers the unsafe-default guard added for the
// --allow-public flag. The matrix is (addr × authToken × allowPublic) →
// either nil error (safe) or an error mentioning OPENUSAGE_HUB_TOKEN.
func TestValidateHubExposure(t *testing.T) {
	cases := []struct {
		name        string
		addr        string
		authToken   string
		allowPublic bool
		wantErr     bool
	}{
		// Loopback bindings are always safe.
		{"loopback v4 + no auth + no opt-in", "127.0.0.1:9190", "", false, false},
		{"loopback localhost + no auth", "localhost:9190", "", false, false},
		{"loopback v6 + no auth", "[::1]:9190", "", false, false},

		// All-interfaces / explicit non-loopback bindings without auth → refused.
		{"port-only :9190 + no auth + no opt-in", ":9190", "", false, true},
		{"wildcard 0.0.0.0 + no auth + no opt-in", "0.0.0.0:9190", "", false, true},
		{"specific LAN IP + no auth + no opt-in", "10.0.0.5:9190", "", false, true},
		{"unresolvable hostname + no auth", "hub.example.com:9190", "", false, true},

		// Auth token present → always allowed (no need to opt-in).
		{"wildcard + auth", ":9190", "secret", false, false},
		{"specific IP + auth", "10.0.0.5:9190", "secret", false, false},

		// --allow-public set → always allowed.
		{"wildcard + no auth + opt-in", ":9190", "", true, false},
		{"loopback + no auth + opt-in", "127.0.0.1:9190", "", true, false},

		// Both auth and opt-in set → allowed.
		{"wildcard + auth + opt-in", ":9190", "secret", true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateHubExposure(tc.addr, tc.authToken, tc.allowPublic)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for addr=%q auth=%q allowPublic=%v, got nil",
						tc.addr, tc.authToken, tc.allowPublic)
				}
				// Error should mention the env var so operators know how to fix it.
				if !strings.Contains(err.Error(), "OPENUSAGE_HUB_TOKEN") {
					t.Errorf("error should mention OPENUSAGE_HUB_TOKEN, got: %v", err)
				}
				if !strings.Contains(err.Error(), "--allow-public") {
					t.Errorf("error should mention --allow-public, got: %v", err)
				}
			} else if err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}
