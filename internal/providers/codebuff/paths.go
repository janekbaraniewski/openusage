package codebuff

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PathHintDataDirKey overrides the resolved channel root list with a single
// explicit path.
const PathHintDataDirKey = "data_dir"

// CodebuffDataDirEnv is honored as an additional channel root when set.
const CodebuffDataDirEnv = "CODEBUFF_DATA_DIR"

func defaultDataDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".config", "manicode"),
		filepath.Join(home, ".config", "manicode-dev"),
		filepath.Join(home, ".config", "manicode-staging"),
	}
}

func resolveDataDirs(acct core.AccountConfig) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if _, dup := seen[p]; dup {
			return
		}
		if !dirExists(p) {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	if override := strings.TrimSpace(acct.Path(PathHintDataDirKey, "")); override != "" {
		add(override)
	}
	for _, d := range defaultDataDirs() {
		add(d)
	}
	if env := strings.TrimSpace(os.Getenv(CodebuffDataDirEnv)); env != "" {
		add(env)
	}
	return out
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
