package detect

import (
	"log"
	"path/filepath"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func detectCodebuff(result *Result) {
	bin := findBinary("codebuff")
	roots := codebuffDefaultRoots()
	var existingRoots []string
	for _, r := range roots {
		if dirExists(r) {
			existingRoots = append(existingRoots, r)
		}
	}

	if bin == "" && len(existingRoots) == 0 {
		return
	}

	if bin != "" {
		log.Printf("[detect] Found Codebuff at %s", bin)
		configDir := ""
		if len(existingRoots) > 0 {
			configDir = existingRoots[0]
		}
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "Codebuff",
			BinaryPath: bin,
			ConfigDir:  configDir,
			Type:       "cli",
		})
	}

	acct := core.AccountConfig{
		ID:           "codebuff",
		Provider:     "codebuff",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	if len(existingRoots) > 0 {
		acct.SetPath("data_dir", existingRoots[0])
		acct.SetHint("data_dir", existingRoots[0])
		log.Printf("[detect] Codebuff data dir at %s", existingRoots[0])
	}

	addAccount(result, acct)
}

func codebuffDefaultRoots() []string {
	home := homeDir()
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".config", "manicode"),
		filepath.Join(home, ".config", "manicode-dev"),
		filepath.Join(home, ".config", "manicode-staging"),
	}
}
