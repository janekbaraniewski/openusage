//go:build !windows

package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// The npm-installed `codex` is a node script that spawns the native binary as
// a child. Stand in for both with shell scripts: the wrapper starts the
// "server" in the background and waits (#446).
const fakeAppServer = `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"id":1,'*) echo '{"id":1,"result":{}}' ;;
    *'"id":2,'*) echo '{"id":2,"result":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":2,"windowDurationMins":10080,"resetsAt":1893456000}}}}' ;;
  esac
done
`

// sh points a background job's stdin at /dev/null, so pass it through fd 3.
const fakeWrapper = `#!/bin/sh
exec 3<&0
"$(dirname "$0")/codex-native" "$@" <&3 3<&- &
wait
`

// A wrapper whose child never answers and ignores stdin EOF: only killing the
// whole process group can unblock the reader on timeout.
const fakeHangingServer = `#!/bin/sh
trap '' TERM
while :; do sleep 1; done
`

func writeFakeCodex(t *testing.T, server string) (dir, wrapper string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex-native"), []byte(server), 0o755); err != nil {
		t.Fatal(err)
	}
	wrapper = filepath.Join(dir, "codex")
	if err := os.WriteFile(wrapper, []byte(fakeWrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, wrapper
}

func TestFetchCodexRateLimitsRPCReturnsWhenCodexIsAWrapper(t *testing.T) {
	dir, wrapper := writeFakeCodex(t, fakeAppServer)
	acct := core.AccountConfig{ID: "codex-cli", Provider: "codex", Binary: wrapper}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := fetchCodexRateLimitsRPCProcess(ctx, acct, dir)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("fetchCodexRateLimitsRPCProcess did not return: cmd.Wait is blocked on an orphaned app-server's pipes")
	}
}

func TestFetchCodexRateLimitsRPCTimesOutWhenWrappedServerHangs(t *testing.T) {
	dir, wrapper := writeFakeCodex(t, fakeHangingServer)
	acct := core.AccountConfig{ID: "codex-cli", Provider: "codex", Binary: wrapper}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := fetchCodexRateLimitsRPCProcess(ctx, acct, dir)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout error from a server that never answers")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("fetchCodexRateLimitsRPCProcess did not return after its deadline: the wrapped child survived the kill")
	}
}
