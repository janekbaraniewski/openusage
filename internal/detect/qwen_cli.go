package detect

import (
	"log"
	"path/filepath"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func detectQwenCLI(result *Result) {
	bin := findBinary("qwen")
	projectsDir := defaultQwenProjectsDir()
	hasProjects := projectsDir != "" && dirExists(projectsDir)

	if bin == "" && !hasProjects {
		return
	}

	if bin != "" {
		log.Printf("[detect] Found Qwen CLI at %s", bin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "Qwen CLI",
			BinaryPath: bin,
			ConfigDir:  defaultQwenConfigDir(),
			Type:       "cli",
		})
	}

	acct := core.AccountConfig{
		ID:           "qwen_cli",
		Provider:     "qwen_cli",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	if hasProjects {
		acct.SetPath("projects_dir", projectsDir)
		acct.SetHint("projects_dir", projectsDir)
		log.Printf("[detect] Qwen CLI projects dir at %s", projectsDir)
	}
	if dir := defaultQwenConfigDir(); dir != "" {
		acct.SetHint("data_dir", dir)
	}

	addAccount(result, acct)
}

func defaultQwenProjectsDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".qwen", "projects")
}

func defaultQwenConfigDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".qwen")
}
