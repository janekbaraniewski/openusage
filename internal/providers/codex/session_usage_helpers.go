package codex

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func recordToolCall(toolCalls map[string]int, callTool map[string]string, callID, tool string) {
	tool = normalizeToolName(tool)
	toolCalls[tool]++
	if strings.TrimSpace(callID) != "" {
		callTool[callID] = tool
	}
}

func normalizeToolName(tool string) string {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return "unknown"
	}
	return tool
}

func setToolCallOutcome(callID, output string, outcomes map[string]int) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	outcomes[callID] = inferToolCallOutcome(output)
}

func inferToolCallOutcome(output string) int {
	lower := strings.ToLower(strings.TrimSpace(output))
	if lower == "" {
		return 1
	}
	if strings.Contains(lower, `"exit_code":0`) || strings.Contains(lower, "process exited with code 0") {
		return 1
	}
	if strings.Contains(lower, "cancelled") || strings.Contains(lower, "canceled") || strings.Contains(lower, "aborted") {
		return 3
	}
	if idx := strings.Index(lower, "process exited with code "); idx >= 0 {
		rest := lower[idx+len("process exited with code "):]
		n := 0
		for _, r := range rest {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		if n == 0 {
			return 1
		}
		return 2
	}
	if idx := strings.Index(lower, "exit code "); idx >= 0 {
		rest := lower[idx+len("exit code "):]
		n := 0
		foundDigit := false
		for _, r := range rest {
			if r < '0' || r > '9' {
				if foundDigit {
					break
				}
				continue
			}
			foundDigit = true
			n = n*10 + int(r-'0')
		}
		if !foundDigit || n == 0 {
			return 1
		}
		return 2
	}
	if strings.Contains(lower, `"exit_code":`) && !strings.Contains(lower, `"exit_code":0`) {
		return 2
	}
	if strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
		return 2
	}
	return 1
}

func recordCommandLanguage(cmd string, langs map[string]int) {
	if language := detectCommandLanguage(cmd); language != "" {
		langs[language]++
	}
}

func detectCommandLanguage(cmd string) string {
	trimmed := strings.TrimSpace(strings.ToLower(cmd))
	if trimmed == "" {
		return ""
	}
	switch {
	case strings.Contains(trimmed, " go ") || strings.HasPrefix(trimmed, "go ") || strings.Contains(trimmed, "gofmt ") || strings.Contains(trimmed, "golangci-lint"):
		return "go"
	case strings.Contains(trimmed, " terraform ") || strings.HasPrefix(trimmed, "terraform "):
		return "terraform"
	case strings.Contains(trimmed, " python ") || strings.HasPrefix(trimmed, "python ") || strings.HasPrefix(trimmed, "python3 "):
		return "python"
	case strings.Contains(trimmed, " npm ") || strings.HasPrefix(trimmed, "npm ") || strings.Contains(trimmed, " yarn ") || strings.HasPrefix(trimmed, "pnpm ") || strings.Contains(trimmed, " node "):
		return "ts"
	case strings.Contains(trimmed, " cargo ") || strings.HasPrefix(trimmed, "cargo ") || strings.Contains(trimmed, " rustc "):
		return "rust"
	case strings.Contains(trimmed, " java ") || strings.HasPrefix(trimmed, "java ") || strings.Contains(trimmed, " gradle ") || strings.Contains(trimmed, " mvn "):
		return "java"
	case strings.Contains(trimmed, ".log"):
		return "log"
	case strings.Contains(trimmed, ".txt"):
		return "txt"
	default:
		return "shell"
	}
}

func commandContainsGitCommit(cmd string) bool {
	normalized := " " + strings.ToLower(cmd) + " "
	return strings.Contains(normalized, " git commit ")
}

func accumulatePatchStats(input string, stats *patchStats, langs map[string]int) {
	if stats == nil {
		return
	}
	lines := strings.Split(input, "\n")
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "*** Update File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
			if path != "" {
				stats.Files[path] = struct{}{}
				if language := languageFromPath(path); language != "" {
					langs[language]++
				}
			}
		case strings.HasPrefix(line, "*** Add File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
			if path != "" {
				stats.Files[path] = struct{}{}
				if language := languageFromPath(path); language != "" {
					langs[language]++
				}
			}
		case strings.HasPrefix(line, "*** Delete File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))
			if path != "" {
				stats.Files[path] = struct{}{}
				stats.Deleted[path] = struct{}{}
				if language := languageFromPath(path); language != "" {
					langs[language]++
				}
			}
		case strings.HasPrefix(line, "*** Move to: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Move to: "))
			if path != "" {
				stats.Files[path] = struct{}{}
				if language := languageFromPath(path); language != "" {
					langs[language]++
				}
			}
		case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "***"):
			continue
		case strings.HasPrefix(line, "+"):
			stats.Added++
		case strings.HasPrefix(line, "-"):
			stats.Removed++
		}
	}
}

func languageFromPath(path string) string {
	lower := strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.HasSuffix(lower, ".go"):
		return "go"
	case strings.HasSuffix(lower, ".tf"):
		return "terraform"
	case strings.HasSuffix(lower, ".ts"), strings.HasSuffix(lower, ".tsx"), strings.HasSuffix(lower, ".js"), strings.HasSuffix(lower, ".jsx"):
		return "ts"
	case strings.HasSuffix(lower, ".py"):
		return "python"
	case strings.HasSuffix(lower, ".rs"):
		return "rust"
	case strings.HasSuffix(lower, ".java"):
		return "java"
	case strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
		return "yaml"
	case strings.HasSuffix(lower, ".json"):
		return "json"
	case strings.HasSuffix(lower, ".md"):
		return "md"
	case strings.HasSuffix(lower, ".tpl"):
		return "tpl"
	case strings.HasSuffix(lower, ".txt"):
		return "txt"
	case strings.HasSuffix(lower, ".log"):
		return "log"
	case strings.HasSuffix(lower, ".sh"), strings.HasSuffix(lower, ".zsh"), strings.HasSuffix(lower, ".bash"):
		return "shell"
	default:
		return ""
	}
}

func normalizeModelName(name string) string {
	return shared.NormalizeLooseModelName(name)
}

func classifyClient(source, originator string) string {
	src := strings.ToLower(strings.TrimSpace(source))
	org := strings.ToLower(strings.TrimSpace(originator))

	switch {
	case src == "openusage" || src == "codex":
		return "CLI"
	case strings.Contains(org, "desktop"):
		return "Desktop App"
	case strings.Contains(org, "exec") || src == "exec":
		return "Exec"
	case strings.Contains(org, "cli") || src == "cli":
		return "CLI"
	case src == "vscode" || src == "ide":
		return "IDE"
	case src == "":
		return "Other"
	default:
		return strings.ToUpper(src)
	}
}

func normalizeClientName(name string) string {
	return shared.NormalizeLooseClientName(name)
}

func sanitizeMetricName(name string) string {
	return shared.SanitizeMetricName(name)
}

func dayFromTimestamp(timestamp string) string {
	if timestamp == "" {
		return ""
	}

	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, timestamp); err == nil {
			return parsed.Format("2006-01-02")
		}
	}

	if len(timestamp) >= 10 {
		candidate := timestamp[:10]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func dayFromSessionPath(path, sessionsDir string) string {
	rel, err := filepath.Rel(sessionsDir, path)
	if err != nil {
		return ""
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 {
		return ""
	}

	candidate := fmt.Sprintf("%s-%s-%s", parts[0], parts[1], parts[2])
	if _, err := time.Parse("2006-01-02", candidate); err != nil {
		return ""
	}
	return candidate
}
