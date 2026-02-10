package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/agentusage/internal/config"
	"github.com/janekbaraniewski/agentusage/internal/core"
	"github.com/janekbaraniewski/agentusage/internal/detect"
	"github.com/janekbaraniewski/agentusage/internal/providers"
	"github.com/janekbaraniewski/agentusage/internal/settings"
	"github.com/janekbaraniewski/agentusage/internal/tui"
)

func main() {
	if os.Getenv("AGENTUSAGE_DEBUG") == "" {
		log.SetOutput(io.Discard)
	} else {
		log.SetOutput(os.Stderr)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Config path: %s\n", config.ConfigPath())
		os.Exit(1)
	}

	userSettings, err := settings.Load()
	if err != nil {
		log.Printf("Warning: could not load settings: %v", err)
		userSettings = settings.DefaultSettings()
	}

	tui.SetThemeByName(userSettings.Theme)

	if cfg.AutoDetect {
		result := detect.AutoDetect()

		existingIDs := make(map[string]bool, len(cfg.Accounts))
		for _, acct := range cfg.Accounts {
			existingIDs[acct.ID] = true
		}
		for _, acct := range result.Accounts {
			if !existingIDs[acct.ID] {
				cfg.Accounts = append(cfg.Accounts, acct)
			}
		}

		if os.Getenv("AGENTUSAGE_DEBUG") != "" {
			if len(result.Tools) > 0 || len(result.Accounts) > 0 {
				fmt.Fprint(os.Stderr, result.Summary())
				fmt.Fprintln(os.Stderr)
			}
		}
	}

	if len(cfg.Accounts) == 0 {
		fmt.Println("⚡ AgentUsage — No accounts configured or detected.")
		fmt.Println()
		fmt.Printf("Config path: %s\n\n", config.ConfigPath())
		fmt.Println("Auto-detection checks for:")
		fmt.Println("  • Cursor IDE       (local DBs + API)")
		fmt.Println("  • Claude Code CLI  (stats-cache.json)")
		fmt.Println("  • OpenAI Codex CLI (session rate limits + tokens)")
		fmt.Println("  • GitHub Copilot   (gh CLI)")
		fmt.Println("  • Gemini CLI       (gemini binary)")
		fmt.Println("  • Aider CLI        (aider binary)")
		fmt.Println("  • Environment variables:")
		fmt.Println("    OPENAI_API_KEY, ANTHROPIC_API_KEY, OPENROUTER_API_KEY,")
		fmt.Println("    GROQ_API_KEY, MISTRAL_API_KEY, DEEPSEEK_API_KEY,")
		fmt.Println("    XAI_API_KEY, GEMINI_API_KEY, GOOGLE_API_KEY")
		fmt.Println()
		fmt.Println("Set any of the above env vars, install a tool, or create a config:")
		fmt.Printf("  mkdir -p %s\n", config.ConfigDir())
		fmt.Printf("  cat > %s <<'EOF'\n", config.ConfigPath())
		fmt.Print(`auto_detect = true

[ui]
refresh_interval_seconds = 30
warn_threshold = 0.20
crit_threshold = 0.05

[[accounts]]
id = "openai-personal"
provider = "openai"
api_key_env = "OPENAI_API_KEY"
probe_model = "gpt-4.1-mini"
`)
		fmt.Println("EOF")
		os.Exit(0)
	}

	interval := time.Duration(cfg.UI.RefreshIntervalSeconds) * time.Second
	engine := core.NewEngine(interval)

	for _, p := range providers.AllProviders() {
		engine.RegisterProvider(p)
	}
	engine.SetAccounts(cfg.Accounts)

	model := tui.NewModel(cfg.UI.WarnThreshold, cfg.UI.CritThreshold, userSettings.Experimental.Analytics)
	model.SetSettingsPath(settings.Path())
	p := tea.NewProgram(model, tea.WithAltScreen())

	engine.OnUpdate(func(snaps map[string]core.QuotaSnapshot) {
		p.Send(tui.SnapshotsMsg(snaps))
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go engine.Run(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
		p.Quit()
	}()

	if _, err := p.Run(); err != nil {
		log.SetOutput(os.Stderr)
		log.Fatalf("TUI error: %v", err)
	}
}
