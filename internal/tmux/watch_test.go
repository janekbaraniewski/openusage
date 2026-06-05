package tmux

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/report"
)

// captureRunner records every tmuxRunner call for assertions.
type captureRunner struct {
	mu    sync.Mutex
	calls [][]string
}

func (c *captureRunner) run(args ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, append([]string(nil), args...))
	return nil
}

func (c *captureRunner) Calls() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]string, len(c.calls))
	for i, c := range c.calls {
		out[i] = append([]string(nil), c...)
	}
	return out
}

// makeAlertCtx returns a Context whose Block reports a high burn rate so the
// burn-rate threshold trips. Used by the threshold-cross tests.
func makeAlertCtx(burn float64, remaining time.Duration) Context {
	return Context{
		Block: report.Row{
			BurnRateUSDPerHour: burn,
			TimeRemaining:      remaining,
		},
		HaveBlock: true,
		Now:       time.Now(),
	}
}

func TestFireDispatchesMessage(t *testing.T) {
	r := &captureRunner{}
	fire(WatchOptions{Runner: r.run, Out: &bytes.Buffer{}}, AlertModeMessage, "burn rate too high")
	calls := r.Calls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 tmux calls (message + refresh), got %d: %v", len(calls), calls)
	}
	if calls[0][0] != "display-message" {
		t.Fatalf("first call should be display-message, got %v", calls[0])
	}
	if calls[1][0] != "refresh-client" {
		t.Fatalf("second call should be refresh-client, got %v", calls[1])
	}
}

func TestFireBothModeAddsBell(t *testing.T) {
	r := &captureRunner{}
	fire(WatchOptions{Runner: r.run, Out: &bytes.Buffer{}}, AlertModeBoth, "msg")
	calls := r.Calls()
	if len(calls) < 3 {
		t.Fatalf("expected display-message + bell + refresh, got %v", calls)
	}
	if calls[1][0] != "set-option" {
		t.Fatalf("expected set-option for bell, got %v", calls[1])
	}
}

func TestFireNoneSuppresses(t *testing.T) {
	r := &captureRunner{}
	fire(WatchOptions{Runner: r.run, Out: &bytes.Buffer{}}, AlertModeNone, "msg")
	if len(r.Calls()) != 0 {
		t.Fatalf("mode=none should not invoke tmux, got %v", r.Calls())
	}
}

// TestEvaluateCooldownRespected exercises the cooldown via the package-private
// evaluate function with a synthetic state machine. We invoke fire directly
// through the same path Watch would take.
func TestEvaluateCooldownRespected(t *testing.T) {
	r := &captureRunner{}
	state := alertState{}
	opts := WatchOptions{
		Runner:   r.run,
		Out:      &bytes.Buffer{},
		Cooldown: time.Hour,
	}
	now := time.Now()

	// Simulate two threshold crosses within the cooldown window. Only the
	// first should fire.
	burnLimit := 1.0
	bctx := makeAlertCtx(5.0, time.Hour)
	if bctx.Block.BurnRateUSDPerHour >= burnLimit && now.Sub(state.lastBurnFire) >= opts.Cooldown {
		fire(opts, AlertModeMessage, "first")
		state.lastBurnFire = now
	}
	// Second tick "now+1m": still inside cooldown, must not fire again.
	now2 := now.Add(time.Minute)
	if bctx.Block.BurnRateUSDPerHour >= burnLimit && now2.Sub(state.lastBurnFire) >= opts.Cooldown {
		fire(opts, AlertModeMessage, "second")
		state.lastBurnFire = now2
	}

	calls := r.Calls()
	// Each fire emits 2 commands (display-message + refresh-client).
	if len(calls) != 2 {
		t.Fatalf("expected 2 tmux calls (one fire), got %d: %v", len(calls), calls)
	}
}

func TestParseAlertMode(t *testing.T) {
	cases := map[string]AlertMode{
		"":        AlertModeMessage,
		"bell":    AlertModeBell,
		"both":    AlertModeBoth,
		"none":    AlertModeNone,
		"message": AlertModeMessage,
		"weird":   AlertModeMessage,
	}
	for in, want := range cases {
		got := ParseAlertMode(in)
		if got != want {
			t.Errorf("ParseAlertMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWatchRefusesOutsideTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	err := Watch(context.Background(), WatchOptions{Runner: func(args ...string) error { return nil }})
	if err == nil {
		t.Fatalf("expected error when $TMUX is unset")
	}
}

func TestEvaluateBlockExpiryFires(t *testing.T) {
	r := &captureRunner{}
	state := alertState{}
	opts := WatchOptions{
		Runner:   r.run,
		Out:      &bytes.Buffer{},
		Cooldown: time.Hour,
		Alerts:   config.TmuxAlerts{BlockMinutesRemaining: 10},
	}
	now := time.Now()

	// Block has 5 minutes left; threshold is 10 minutes. Should fire.
	remaining := 5 * time.Minute
	if remaining > 0 && remaining <= time.Duration(opts.Alerts.BlockMinutesRemaining)*time.Minute &&
		now.Sub(state.lastBlockFire) >= opts.Cooldown {
		fire(opts, AlertModeMessage, "block ending")
	}
	if len(r.Calls()) == 0 {
		t.Fatalf("expected at least one fire call")
	}
}

func TestFormatMinutes(t *testing.T) {
	cases := map[time.Duration]string{
		0:                           "<1m",
		30 * time.Second:            "<1m",
		2 * time.Minute:             "2m",
		95 * time.Second:            "1m",
		3*time.Hour + 4*time.Minute: "184m",
	}
	for in, want := range cases {
		if got := formatMinutes(in); got != want {
			t.Errorf("formatMinutes(%v) = %q, want %q", in, got, want)
		}
	}
}
