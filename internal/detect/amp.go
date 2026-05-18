package detect

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectAmp registers an Amp account when either the Amp client binary is on
// PATH or the per-user data directory exists. Either condition is sufficient
// because the provider reads local files only — a workstation that ran Amp
// once and then uninstalled the binary still has usable data.
func detectAmp(result *Result) {
	bin := findBinary("amp")
	dataDir := ampDataDir()
	hasData := dirExists(dataDir)

	if bin == "" && !hasData {
		return
	}

	tool := DetectedTool{
		Name:       "Amp",
		BinaryPath: bin,
		ConfigDir:  dataDir,
		Type:       "cli",
	}
	result.Tools = append(result.Tools, tool)

	switch {
	case bin != "" && hasData:
		log.Printf("[detect] Found Amp at %s with data dir %s", bin, dataDir)
	case bin != "":
		log.Printf("[detect] Found Amp at %s (no data dir yet)", bin)
	default:
		log.Printf("[detect] Found Amp data dir at %s (no binary on PATH)", dataDir)
	}

	threadsDir := filepath.Join(dataDir, "threads")
	ledgerPath := filepath.Join(dataDir, "ledger.jsonl")

	hasThreads := dirExists(threadsDir)
	hasLedger := fileExists(ledgerPath)

	if !hasData && !hasThreads {
		// Binary present but the user has never run a thread; still
		// register a minimal account so the dashboard shows the tile.
		addAccount(result, core.AccountConfig{
			ID:       "amp",
			Provider: "amp",
			Auth:     "local",
			Binary:   bin,
		})
		return
	}

	acct := core.AccountConfig{
		ID:       "amp",
		Provider: "amp",
		Auth:     "local",
		Binary:   bin,
	}
	acct.SetPath("data_dir", dataDir)
	if hasThreads {
		acct.SetPath("threads_dir", threadsDir)
	}
	if hasLedger {
		acct.SetPath("ledger_path", ledgerPath)
	}
	addAccount(result, acct)
}

// ampDataDir mirrors the resolution logic in the amp provider package. We
// duplicate it here so the detect package doesn't pull the provider in.
func ampDataDir() string {
	if v := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); v != "" {
		return filepath.Join(v, "amp")
	}
	home := homeDir()
	if home == "" {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "amp")
	case "windows":
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			return filepath.Join(appData, "amp")
		}
		return filepath.Join(home, "AppData", "Roaming", "amp")
	default:
		return filepath.Join(home, ".local", "share", "amp")
	}
}
