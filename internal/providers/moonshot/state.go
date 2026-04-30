package moonshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// peakState tracks the highest balance value ever observed per account, per
// balance dimension. Moonshot's API exposes only the *remaining* balance;
// without the deposit total, gauges can't render. We derive the deposit
// approximation by remembering the maximum balance we've ever seen.
//
// Self-corrects: on the next top-up the peak is bumped to the new high. Worst
// case (openusage installed mid-cycle, no top-up since) is a stable
// "essentially full" gauge until the next top-up — which is honest about what
// we actually know.
type peakState struct {
	PeakAvailable float64   `json:"peak_available_balance"`
	PeakCash      float64   `json:"peak_cash_balance"`
	PeakVoucher   float64   `json:"peak_voucher_balance"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// stateFile maps accountID → peakState. Loaded/saved as a single JSON blob to
// keep IO simple. The file lives next to the telemetry SQLite so it inherits
// the same state-dir conventions.
type stateFile struct {
	Version  int                  `json:"version"`
	Accounts map[string]peakState `json:"accounts"`
}

const stateFileVersion = 1

var stateMu sync.Mutex

// stateFilePath returns the canonical location for the provider's peak state.
// Override via OPENUSAGE_MOONSHOT_STATE_PATH for tests.
func stateFilePath() (string, error) {
	if override := os.Getenv("OPENUSAGE_MOONSHOT_STATE_PATH"); override != "" {
		return override, nil
	}
	dir, err := stateBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "provider_state", "moonshot.json"), nil
}

func stateBaseDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("LOCALAPPDATA")
		if appData == "" {
			appData = os.Getenv("APPDATA")
		}
		if appData == "" {
			return "", fmt.Errorf("no LOCALAPPDATA/APPDATA in environment")
		}
		return filepath.Join(appData, "openusage"), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "state", "openusage"), nil
	}
}

func loadState() (stateFile, error) {
	path, err := stateFilePath()
	if err != nil {
		return stateFile{Version: stateFileVersion, Accounts: map[string]peakState{}}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return stateFile{Version: stateFileVersion, Accounts: map[string]peakState{}}, nil
		}
		return stateFile{Version: stateFileVersion, Accounts: map[string]peakState{}}, err
	}
	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		// Corrupt file — start fresh rather than blow up. The loss is one
		// historical peak, which self-heals on the next top-up.
		return stateFile{Version: stateFileVersion, Accounts: map[string]peakState{}}, nil
	}
	if sf.Accounts == nil {
		sf.Accounts = map[string]peakState{}
	}
	if sf.Version == 0 {
		sf.Version = stateFileVersion
	}
	return sf, nil
}

func saveState(sf stateFile) error {
	path, err := stateFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// updatePeaks loads the persisted state for accountID, bumps any peak whose
// observed value exceeds the stored value, persists if anything changed, and
// returns the resulting peaks. On any IO error it falls back to "peaks =
// observed" so gauges still render — preserving correctness over persistence.
func updatePeaks(accountID string, observed peakState) peakState {
	stateMu.Lock()
	defer stateMu.Unlock()

	sf, _ := loadState()
	current := sf.Accounts[accountID]

	merged := peakState{
		PeakAvailable: maxFloat(current.PeakAvailable, observed.PeakAvailable),
		PeakCash:      maxFloat(current.PeakCash, observed.PeakCash),
		PeakVoucher:   maxFloat(current.PeakVoucher, observed.PeakVoucher),
	}
	changed := merged.PeakAvailable != current.PeakAvailable ||
		merged.PeakCash != current.PeakCash ||
		merged.PeakVoucher != current.PeakVoucher
	if changed {
		merged.UpdatedAt = time.Now().UTC()
		sf.Accounts[accountID] = merged
		_ = saveState(sf)
	} else {
		// Preserve the old timestamp when nothing changed, so the file isn't
		// rewritten on every poll just to bump UpdatedAt.
		merged.UpdatedAt = current.UpdatedAt
	}
	return merged
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
