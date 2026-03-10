package copilot

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func (p *Provider) readConfig(copilotDir string, snap *core.UsageSnapshot) {
	data, err := os.ReadFile(filepath.Join(copilotDir, "config.json"))
	if err != nil {
		return
	}
	var cfg copilotConfig
	if json.Unmarshal(data, &cfg) != nil {
		return
	}
	if cfg.Model != "" {
		snap.Raw["preferred_model"] = cfg.Model
	}
	if cfg.ReasoningEffort != "" {
		snap.Raw["reasoning_effort"] = cfg.ReasoningEffort
	}
	if cfg.Experimental {
		snap.Raw["experimental"] = "enabled"
	}
}
