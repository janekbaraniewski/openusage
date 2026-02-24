package integrations

import (
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/detect"
)

// Match pairs an integration Definition with auto-detection results.
type Match struct {
	Definition Definition
	Tool       *detect.DetectedTool
	Account    *core.AccountConfig
	Status     Status
	Actionable bool // true if tool/account detected AND not installed
}

// MatchDetected matches integration definitions against auto-detection results.
// Uses Definition.MatchProviderIDs to find matching accounts (stable join key).
// Uses Definition.MatchToolNameHint to find the corresponding DetectedTool (display only).
func MatchDetected(defs []Definition, detected detect.Result, dirs Dirs) []Match {
	matches := make([]Match, 0, len(defs))

	for _, def := range defs {
		m := Match{
			Definition: def,
			Status:     def.Detector(dirs),
		}

		// Match accounts by provider ID.
		for i := range detected.Accounts {
			acct := &detected.Accounts[i]
			for _, pid := range def.MatchProviderIDs {
				if acct.Provider == pid {
					m.Account = acct
					break
				}
			}
			if m.Account != nil {
				break
			}
		}

		// Match tools by name hint substring.
		if def.MatchToolNameHint != "" {
			hint := strings.ToLower(def.MatchToolNameHint)
			for i := range detected.Tools {
				tool := &detected.Tools[i]
				if strings.Contains(strings.ToLower(tool.Name), hint) {
					m.Tool = tool
					break
				}
			}
		}

		m.Actionable = (m.Account != nil || m.Tool != nil) && m.Status.State != "ready"

		matches = append(matches, m)
	}

	return matches
}
