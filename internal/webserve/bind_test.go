package webserve

import (
	"strings"
	"testing"
)

func TestIsLoopbackAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"localhost:8080", true},
		{"[::1]:8080", true},
		{":8080", false},
		{"0.0.0.0:8080", false},
		{"10.0.0.5:8080", false},
		{"hub.example.com:8080", false},
		{"", false},
		{"127.0.0.1", true},
		{"localhost", true},
	}
	for _, tc := range cases {
		t.Run(tc.addr, func(t *testing.T) {
			if got := IsLoopbackAddr(tc.addr); got != tc.want {
				t.Errorf("IsLoopbackAddr(%q) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

func TestValidateExposure(t *testing.T) {
	cases := []struct {
		name        string
		addr        string
		authToken   string
		allowPublic bool
		wantErr     bool
	}{
		{"loopback v4 + no auth", "127.0.0.1:8080", "", false, false},
		{"loopback localhost + no auth", "localhost:8080", "", false, false},
		{"loopback v6 + no auth", "[::1]:8080", "", false, false},
		{"port-only + no auth", ":8080", "", false, true},
		{"wildcard + no auth", "0.0.0.0:8080", "", false, true},
		{"lan ip + no auth", "10.0.0.5:8080", "", false, true},
		{"hostname + no auth", "serve.example.com:8080", "", false, true},
		{"wildcard + auth", ":8080", "secret", false, false},
		{"wildcard + allow-public", ":8080", "", true, false},
		{"loopback + allow-public", "127.0.0.1:8080", "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateExposure(tc.addr, tc.authToken, tc.allowPublic)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for addr=%q", tc.addr)
				}
				if !strings.Contains(err.Error(), "OPENUSAGE_SERVE_TOKEN") {
					t.Errorf("error should mention OPENUSAGE_SERVE_TOKEN, got: %v", err)
				}
				if !strings.Contains(err.Error(), "--allow-public") {
					t.Errorf("error should mention --allow-public, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestNormalizeListenAddr(t *testing.T) {
	if got := normalizeListenAddr(""); got != defaultListenAddr {
		t.Errorf("empty = %q, want %q", got, defaultListenAddr)
	}
	if got := normalizeListenAddr("  :9090  "); got != ":9090" {
		t.Errorf("trimmed = %q, want :9090", got)
	}
}
