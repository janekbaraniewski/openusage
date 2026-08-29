package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/tui"
	"github.com/janekbaraniewski/openusage/internal/version"
	"github.com/janekbaraniewski/openusage/internal/webserve"
	"github.com/spf13/cobra"
)

const envServeToken = "OPENUSAGE_SERVE_TOKEN"

func newServeCommand() *cobra.Command {
	var (
		listenAddr  string
		sourceFlag  string
		demo        bool
		openBrowser bool
		noOpen      bool
		allowPublic bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve a local web dashboard for current usage snapshots",
		Long: strings.Join([]string{
			"Start a local HTTP server with a browser dashboard of the same usage snapshots",
			"the terminal UI shows.",
			"",
			"By default the server binds to 127.0.0.1:8080 and opens your browser.",
			"Collection prefers the telemetry daemon and falls back to a direct provider poll",
			"(same as `openusage export --source auto`).",
			"",
			"Security: without OPENUSAGE_SERVE_TOKEN the server refuses to bind a non-loopback",
			"interface unless you pass --allow-public.",
		}, "\n"),
		Example: strings.Join([]string{
			"  openusage serve",
			"  openusage serve --demo",
			"  openusage serve --listen 127.0.0.1:9090 --no-open",
			"  OPENUSAGE_SERVE_TOKEN=s3cret openusage serve --listen :8080",
		}, "\n"),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				log.Printf("warning: config load failed, using defaults: %v", err)
				cfg = config.DefaultConfig()
			}
			opts := webserve.Options{
				ListenAddr:     firstNonEmpty(listenAddr, cfg.Serve.ListenAddr),
				AuthToken:      firstNonEmpty(cfg.Serve.AuthToken, os.Getenv(envServeToken)),
				Source:         sourceFlag,
				TimeWindow:     cfg.Data.TimeWindow,
				Theme:          cfg.Theme,
				UsageMode:      cfg.Dashboard.UsageMode,
				WarnThreshold:  cfg.UI.WarnThreshold,
				CritThreshold:  cfg.UI.CritThreshold,
				RefreshSeconds: cfg.UI.RefreshIntervalSeconds,
				Version:        version.Version,
				Demo:           demo,
				AllowPublic:    allowPublic,
				Config:         &cfg,
			}
			shouldOpen := openBrowser
			if noOpen {
				shouldOpen = false
			} else if !cmd.Flags().Changed("open") {
				fi, statErr := os.Stdout.Stat()
				shouldOpen = statErr == nil && fi.Mode()&os.ModeCharDevice != 0
			}
			return runServe(opts, shouldOpen)
		},
	}

	cmd.Flags().StringVar(&listenAddr, "listen", "", "TCP address to listen on (default 127.0.0.1:8080)")
	cmd.Flags().StringVar(&sourceFlag, "source", "auto", "collection source: auto, direct, or daemon")
	cmd.Flags().BoolVar(&demo, "demo", false, "Serve synthetic snapshots (no daemon or API keys required)")
	cmd.Flags().BoolVar(&openBrowser, "open", false, "Open the dashboard in the default browser")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Do not open a browser")
	cmd.Flags().BoolVar(&allowPublic, "allow-public", false, "Allow binding a non-loopback interface without OPENUSAGE_SERVE_TOKEN")
	return cmd
}

func runServe(opts webserve.Options, openBrowser bool) error {
	if opts.Config != nil {
		if err := tui.LoadThemes(config.ConfigDir()); err != nil && core.DebugEnabled() {
			log.Printf("serve: theme load: %v", err)
		}
		tui.SetThemeByName(opts.Config.Theme)
	}

	srv, err := webserve.NewServer(opts)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	displayURL := serveURL(srv.Addr())
	source := opts.Source
	if opts.Demo {
		source = "demo"
	}
	authLabel := "disabled"
	if srv.AuthEnabled() {
		authLabel = "bearer-token"
	}
	fmt.Printf("OpenUsage web dashboard listening on %s\n", displayURL)
	fmt.Printf("  source=%s  auth=%s\n", source, authLabel)
	fmt.Printf("  press Ctrl+C to stop\n")

	if openBrowser {
		go func() {
			time.Sleep(200 * time.Millisecond)
			if err := openBrowserURL(displayURL); err != nil && core.DebugEnabled() {
				log.Printf("serve: opening browser: %v", err)
			}
		}()
	}

	if err := srv.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func serveURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "http://" + host + ":" + port
}

func openBrowserURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
