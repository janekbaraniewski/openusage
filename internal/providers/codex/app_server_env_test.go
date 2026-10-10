package codex

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestCodexRPCTimeoutLeavesBudgetForFallback(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name     string
		deadline time.Duration // 0 means no deadline
		want     time.Duration
	}{
		{"no deadline", 0, codexRPCMaxTimeout},
		{"daemon poll budget", 8 * time.Second, 4 * time.Second},
		{"generous deadline capped", time.Minute, codexRPCMaxTimeout},
		{"expired", -time.Second, time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.deadline != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, now.Add(tc.deadline))
				defer cancel()
			}
			if got := codexRPCTimeout(ctx, now); got != tc.want {
				t.Fatalf("codexRPCTimeout = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCodexAppServerEnv(t *testing.T) {
	sep := string(os.PathListSeparator)
	binDir := filepath.Join(t.TempDir(), "nvm", "bin")
	binary := filepath.Join(binDir, "codex")

	t.Run("prepends binary dir and sets CODEX_HOME", func(t *testing.T) {
		env := codexAppServerEnv([]string{"PATH=/usr/bin" + sep + "/bin", "CODEX_HOME=/stale", "HOME=/h"}, binary, "/cfg")
		if !slices.Contains(env, "PATH="+binDir+sep+"/usr/bin"+sep+"/bin") {
			t.Fatalf("PATH not prepended: %v", env)
		}
		if !slices.Contains(env, "CODEX_HOME=/cfg") || slices.Contains(env, "CODEX_HOME=/stale") {
			t.Fatalf("CODEX_HOME not replaced: %v", env)
		}
		if !slices.Contains(env, "HOME=/h") {
			t.Fatalf("unrelated vars dropped: %v", env)
		}
	})

	t.Run("does not duplicate dir already on PATH", func(t *testing.T) {
		env := codexAppServerEnv([]string{"PATH=/usr/bin" + sep + binDir}, binary, "")
		if !slices.Equal(env, []string{"PATH=/usr/bin" + sep + binDir}) {
			t.Fatalf("env = %v", env)
		}
	})

	t.Run("adds PATH when missing", func(t *testing.T) {
		env := codexAppServerEnv(nil, binary, "")
		if !slices.Equal(env, []string{"PATH=" + binDir}) {
			t.Fatalf("env = %v", env)
		}
	})

	t.Run("bare binary name leaves PATH alone", func(t *testing.T) {
		env := codexAppServerEnv([]string{"PATH=/usr/bin"}, "codex", "")
		if !slices.Equal(env, []string{"PATH=/usr/bin"}) {
			t.Fatalf("env = %v", env)
		}
	})
}

// An npm/nvm codex install is a `#!/usr/bin/env node` script next to the node
// binary. Under launchd/systemd that directory is not on PATH, so the
// interpreter must be resolved from the binary's own directory.
func TestRPCProcessResolvesInterpreterNextToBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell scripts as the fake codex binary")
	}
	dir := t.TempDir()
	interp := "openusage-fake-node"
	if err := os.WriteFile(filepath.Join(dir, interp), []byte("#!/bin/sh\nexec /bin/sh \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := strings.Join([]string{
		"#!/usr/bin/env " + interp,
		`read first`,
		`printf '%s\n' '{"id":1,"result":{}}'`,
		`read notification`,
		`read request`,
		`printf '%s\n' '{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":0,"windowDurationMins":300}}}}'`,
	}, "\n") + "\n"
	binary := filepath.Join(dir, "codex")
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin"+string(os.PathListSeparator)+"/bin")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := fetchCodexRateLimitsRPCProcess(ctx, core.AccountConfig{Binary: binary}, dir)
	if err != nil || result.RateLimitsV2 == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
