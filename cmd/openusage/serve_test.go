package main

import "testing"

func TestServeURL(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:8080": "http://127.0.0.1:8080",
		"localhost:9090": "http://localhost:9090",
		":8080":          "http://127.0.0.1:8080",
		"0.0.0.0:8080":   "http://127.0.0.1:8080",
		"[::1]:8080":     "http://[::1]:8080",
	}
	for in, want := range cases {
		if got := serveURL(in); got != want {
			t.Errorf("serveURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", " keep "); got != "keep" {
		t.Errorf("got %q", got)
	}
	if got := firstNonEmpty(); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestNewServeCommandFlags(t *testing.T) {
	cmd := newServeCommand()
	for _, name := range []string{"listen", "source", "demo", "open", "no-open", "allow-public"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
}
