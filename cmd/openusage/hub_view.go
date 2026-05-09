package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/tui"
	"github.com/spf13/cobra"
)

// envHubToken is shared with the hub server + exporter: if --token is not
// provided, this env var is used as a Bearer token fallback.
const envHubToken = "OPENUSAGE_HUB_TOKEN"

func newHubViewCommand() *cobra.Command {
	var interval time.Duration
	var token string

	cmd := &cobra.Command{
		Use:   "hub-view <url>",
		Short: "View a remote hub's aggregated usage data in the TUI",
		Long: strings.Join([]string{
			"Connect to a remote OpenUsage hub and display its aggregated snapshot data in a read-only TUI.",
			"No local providers or daemon required.",
			"",
			"If the target hub has auth_token set, pass --token or export OPENUSAGE_HUB_TOKEN to authenticate.",
		}, "\n"),
		Example: strings.Join([]string{
			"  openusage hub-view https://openusage.gameapp.club",
			"  openusage hub-view http://192.168.1.10:9190 --interval 10s",
			"  OPENUSAGE_HUB_TOKEN=s3cret openusage hub-view http://hub:9190",
		}, "\n"),
		Args: cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			cfg, err := config.Load()
			if err != nil {
				log.Printf("warning: config load failed, using defaults: %v", err)
				cfg = config.DefaultConfig()
			}
			hubURL := strings.TrimRight(strings.TrimSpace(args[0]), "/")
			if interval > 0 {
				cfg.UI.RefreshIntervalSeconds = int(interval.Seconds())
			}
			resolved := strings.TrimSpace(token)
			if resolved == "" {
				resolved = strings.TrimSpace(os.Getenv(envHubToken))
			}
			runHubView(cfg, hubURL, resolved)
		},
	}

	cmd.Flags().DurationVar(&interval, "interval", 0, "polling interval for fetching snapshots (0 uses config or 30s)")
	cmd.Flags().StringVar(&token, "token", "", "Bearer token for hubs requiring auth (falls back to OPENUSAGE_HUB_TOKEN env var)")
	return cmd
}

func runHubView(cfg config.Config, hubURL, token string) {
	verbose := os.Getenv("OPENUSAGE_DEBUG") != ""

	if err := tui.LoadThemes(config.ConfigDir()); err != nil && verbose {
		log.Printf("theme load: %v", err)
	}
	tui.SetThemeByName(cfg.Theme)

	pollInterval := time.Duration(cfg.UI.RefreshIntervalSeconds) * time.Second
	if pollInterval <= 0 {
		pollInterval = 30 * time.Second
	}

	timeWindow := core.ParseTimeWindow(cfg.Data.TimeWindow)

	model := tui.NewModel(
		cfg.UI.WarnThreshold,
		cfg.UI.CritThreshold,
		cfg.Experimental.Analytics,
		cfg.Dashboard,
		nil,
		timeWindow,
	)

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithFPS(30))
	dispatcher := &snapshotDispatcher{}
	dispatcher.bind(program)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		// Optimistically mark the hub as "running" on launch so the splash
		// screen doesn't block the TUI — the first fetch will update the
		// status to Error if the hub is unreachable.
		program.Send(tui.DaemonStatusMsg{Status: tui.DaemonRunning})

		snapshotsURL := hubURL + "/v1/snapshots"
		client := &http.Client{Timeout: 10 * time.Second}

		// lastStatus avoids flooding the TUI with redundant status messages
		// when nothing has changed between polls.
		var lastStatus tui.DaemonStatus
		var lastMessage string

		sendStatus := func(status tui.DaemonStatus, message string) {
			if status == lastStatus && message == lastMessage {
				return
			}
			lastStatus = status
			lastMessage = message
			program.Send(tui.DaemonStatusMsg{Status: status, Message: message})
		}

		fetch := func() {
			snaps, err := fetchHubSnapshots(client, snapshotsURL, token)
			if err != nil {
				if verbose {
					log.Printf("hub-view: fetch %s: %v", snapshotsURL, err)
				}
				sendStatus(tui.DaemonError, fmt.Sprintf("hub fetch failed: %v", err))
				return
			}
			sendStatus(tui.DaemonRunning, fmt.Sprintf("hub %s · %d machine snapshots", hubURL, len(snaps)))
			if len(snaps) == 0 {
				return
			}
			dispatcher.dispatch(daemon.SnapshotFrame{Snapshots: snaps, TimeWindow: timeWindow})
		}

		fetch() // immediate first fetch
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fetch()
			}
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
		program.Quit()
	}()

	if _, err := program.Run(); err != nil {
		log.SetOutput(os.Stderr)
		log.Fatalf("TUI error: %v", err)
	}
}

func fetchHubSnapshots(client *http.Client, url, token string) (map[string]core.UsageSnapshot, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("HTTP 401: hub requires Bearer token (--token or OPENUSAGE_HUB_TOKEN)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var snaps map[string]core.UsageSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snaps); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return snaps, nil
}
