package amp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// XDG_DATA_HOME env var name, per the XDG Base Directory Specification.
const xdgDataHomeEnv = "XDG_DATA_HOME"

// defaultDataDir returns the platform-appropriate per-user data directory for
// Amp. The reverse-engineered layout places everything under
// `<data>/amp/` with `threads/` and `ledger.jsonl` siblings.
//
// Resolution order:
//  1. `$XDG_DATA_HOME/amp` (when XDG_DATA_HOME is set, on any platform)
//  2. macOS: `$HOME/Library/Application Support/amp`
//  3. Linux/Unix fallback: `$HOME/.local/share/amp`
//  4. Windows: `%APPDATA%/amp` then `$HOME/AppData/Roaming/amp`
//
// Returns "" when the user's home directory cannot be determined.
func defaultDataDir() string {
	if v := strings.TrimSpace(os.Getenv(xdgDataHomeEnv)); v != "" {
		return filepath.Join(v, "amp")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
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

// defaultThreadsDir returns the conventional threads directory under the data
// dir. Returns "" when the data dir cannot be resolved.
func defaultThreadsDir() string {
	dir := defaultDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "threads")
}

// defaultLedgerPath returns the conventional ledger file path under the data
// dir. Returns "" when the data dir cannot be resolved.
func defaultLedgerPath() string {
	dir := defaultDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "ledger.jsonl")
}

// resolveDataDir picks the effective data directory for a fetch / detect
// call. Callers should pass an override (from AccountConfig.Path / Hint or
// AccountConfig.Binary) when present; otherwise the platform default is used.
func resolveDataDir(override string) string {
	if v := strings.TrimSpace(override); v != "" {
		return v
	}
	return defaultDataDir()
}
