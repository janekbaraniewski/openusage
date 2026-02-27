package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/appupdate"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

func TestRunStartupUpdateCheckSendsMessageOnUpdate(t *testing.T) {
	var gotMsg *tui.AppUpdateMsg
	checkCalled := false

	runStartupUpdateCheck(
		context.Background(),
		" v1.2.0 ",
		1200*time.Millisecond,
		false,
		func(_ context.Context, opts appupdate.CheckOptions) (appupdate.Result, error) {
			checkCalled = true
			if opts.CurrentVersion != "v1.2.0" {
				t.Fatalf("opts.CurrentVersion = %q, want v1.2.0", opts.CurrentVersion)
			}
			if opts.Timeout != 1200*time.Millisecond {
				t.Fatalf("opts.Timeout = %v, want 1200ms", opts.Timeout)
			}
			return appupdate.Result{
				UpdateAvailable: true,
				CurrentVersion:  "v1.2.0",
				LatestVersion:   "v1.3.0",
				UpgradeHint:     "brew upgrade janekbaraniewski/tap/openusage",
			}, nil
		},
		func(msg tui.AppUpdateMsg) {
			m := msg
			gotMsg = &m
		},
	)

	if !checkCalled {
		t.Fatal("expected check function to be called")
	}
	if gotMsg == nil {
		t.Fatal("expected AppUpdateMsg to be sent")
	}
	if gotMsg.CurrentVersion != "v1.2.0" || gotMsg.LatestVersion != "v1.3.0" {
		t.Fatalf("got message versions = %+v", *gotMsg)
	}
}

func TestRunStartupUpdateCheckNoMessageWhenNoUpdate(t *testing.T) {
	sent := false

	runStartupUpdateCheck(
		context.Background(),
		"v1.2.0",
		time.Second,
		false,
		func(_ context.Context, _ appupdate.CheckOptions) (appupdate.Result, error) {
			return appupdate.Result{UpdateAvailable: false}, nil
		},
		func(_ tui.AppUpdateMsg) {
			sent = true
		},
	)

	if sent {
		t.Fatal("did not expect message when no update is available")
	}
}

func TestRunStartupUpdateCheckLogsErrorOnlyInDebug(t *testing.T) {
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	defer log.SetOutput(prevWriter)
	defer log.SetFlags(prevFlags)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	checkErr := errors.New("boom")

	runStartupUpdateCheck(
		context.Background(),
		"v1.2.0",
		time.Second,
		false,
		func(_ context.Context, _ appupdate.CheckOptions) (appupdate.Result, error) {
			return appupdate.Result{}, checkErr
		},
		func(_ tui.AppUpdateMsg) {},
	)
	if buf.Len() != 0 {
		t.Fatalf("expected no logs when debug=false, got %q", buf.String())
	}

	runStartupUpdateCheck(
		context.Background(),
		"v1.2.0",
		time.Second,
		true,
		func(_ context.Context, _ appupdate.CheckOptions) (appupdate.Result, error) {
			return appupdate.Result{}, checkErr
		},
		func(_ tui.AppUpdateMsg) {},
	)
	if !strings.Contains(buf.String(), "app update check failed: boom") {
		t.Fatalf("expected debug log line, got %q", buf.String())
	}
}
