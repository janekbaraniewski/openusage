package detect

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// detectShellRC parses common shell startup files for `export VAR=...` lines
// matching env vars in envKeyMapping. This catches the case where a user sets
// API keys only in their shell rc and launches openusage from a GUI launcher
// (Spotlight/Dock/desktop launcher) which never sources those files — so
// os.Getenv() returns empty even though the key is "set" from the user's POV.
//
// Precedence: detectEnvKeys runs before this. If the env var is already
// populated in the process env, this detector skips that var entirely; the
// addAccount de-dupe means the env-var account also wins by ID. We additionally
// short-circuit per-var to avoid logging "found in shell rc" when the running
// process already has the value.
func detectShellRC(result *Result) {
	home := homeDir()
	if home == "" {
		return
	}

	files := shellRCFiles(home)
	if len(files) == 0 {
		return
	}

	for _, path := range files {
		discoveries, err := parseShellRCFile(path, envKeyByVar)
		if err != nil {
			// Read errors logged at debug-ish level; missing files were
			// filtered out earlier so a real error here is unusual.
			log.Printf("[detect] shell rc %s read error: %v", path, err)
			continue
		}
		for _, d := range discoveries {
			adoptAPIKey(result, d.envKeyMappingEntry, d.Value, "shell_rc:"+path)
		}
	}
}

// shellRCDiscovery is a parsed (var, value, source-file) triple from a single
// shell rc line.
type shellRCDiscovery struct {
	envKeyMappingEntry
	Value string
	Path  string
}

// shellRCFiles returns every shell startup file we know how to parse, in the
// rough order shells load them. The order is informational only — addAccount
// already de-dupes.
func shellRCFiles(home string) []string {
	candidates := []string{
		filepath.Join(home, ".zshenv"),
		filepath.Join(home, ".zprofile"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".zlogin"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".profile"),
		filepath.Join(home, ".config", "fish", "config.fish"),
	}

	// Modular configs: ~/.zshrc.d/*.zsh, ~/.bashrc.d/*.sh, ~/.config/fish/conf.d/*.fish.
	for _, glob := range []string{
		filepath.Join(home, ".zshrc.d", "*.zsh"),
		filepath.Join(home, ".bashrc.d", "*.sh"),
		filepath.Join(home, ".config", "fish", "conf.d", "*.fish"),
	} {
		matches, err := filepath.Glob(glob)
		if err == nil {
			candidates = append(candidates, matches...)
		}
	}

	// Filter to existing regular files; strip duplicates while preserving order.
	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, p := range candidates {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		if !fileExists(p) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// parseShellRCFile parses a single rc file and returns all known-env-var
// assignments it found.
func parseShellRCFile(path string, knownVars map[string]envKeyMappingEntry) ([]shellRCDiscovery, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []shellRCDiscovery
	scanner := bufio.NewScanner(f)
	// Allow long lines (some users have one-line "export FOO=very_long_value").
	const maxLine = 1 << 20 // 1 MiB
	scanner.Buffer(make([]byte, 64*1024), maxLine)

	for scanner.Scan() {
		line := scanner.Text()
		v, value, ok := parseExportLine(line)
		if !ok {
			continue
		}
		entry, known := knownVars[v]
		if !known {
			continue
		}
		out = append(out, shellRCDiscovery{
			envKeyMappingEntry: entry,
			Value:              value,
			Path:               path,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// parseExportLine extracts (NAME, VALUE) from a shell rc line. Recognises:
//
//	export NAME=VALUE
//	NAME=VALUE
//	set -gx NAME VALUE     (fish)
//	set -x  NAME VALUE     (fish, also acceptable)
//
// Returns ok=false for any line we can't safely parse without executing a
// shell — including values that reference other variables ($VAR, ${VAR},
// $(...), `...`) or use unquoted whitespace.
func parseExportLine(raw string) (name, value string, ok bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}

	// Strip leading "export " (bash/zsh) — keep in mind "exportFOO" is not it.
	if strings.HasPrefix(line, "export ") || strings.HasPrefix(line, "export\t") {
		line = strings.TrimSpace(line[len("export"):])
	}

	// Fish: `set -gx NAME VALUE` or `set -x NAME VALUE`.
	if strings.HasPrefix(line, "set ") || strings.HasPrefix(line, "set\t") {
		fields := splitFishSet(line)
		if len(fields) < 4 {
			return "", "", false
		}
		// fields[0]=="set", fields[1] is flags like "-gx" or "-x".
		flags := fields[1]
		if !strings.Contains(flags, "x") {
			return "", "", false
		}
		name = fields[2]
		// Re-join the remainder so multi-word values stay intact, then the
		// usual quote/substitution rules apply.
		value = strings.Join(fields[3:], " ")
	} else {
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			return "", "", false
		}
		name = line[:eq]
		value = line[eq+1:]
	}

	if !isValidEnvName(name) {
		return "", "", false
	}

	value, ok = sanitiseShellValue(value)
	if !ok {
		return "", "", false
	}
	if value == "" {
		return "", "", false
	}
	return name, value, true
}

// splitFishSet tokenises a `set ...` line on whitespace, ignoring quoted
// segments. It's a tiny shell-style splitter that keeps quoted regions
// together; substitution rejection happens later in sanitiseShellValue.
func splitFishSet(line string) []string {
	var out []string
	var cur strings.Builder
	var inSingle, inDouble bool
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			cur.WriteByte(c)
		case c == '"' && !inSingle:
			inDouble = !inDouble
			cur.WriteByte(c)
		case (c == ' ' || c == '\t') && !inSingle && !inDouble:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// isValidEnvName rejects anything that isn't a plausible POSIX env var name.
func isValidEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlpha := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
		isDigit := c >= '0' && c <= '9'
		if i == 0 && !isAlpha {
			return false
		}
		if !isAlpha && !isDigit {
			return false
		}
	}
	return true
}

// sanitiseShellValue handles quoting and rejects values we can't parse
// without invoking a shell. Returns the literal string the shell would
// expand to (assuming no substitutions), or ok=false if the value contains
// substitutions, command substitutions, or unquoted whitespace.
func sanitiseShellValue(raw string) (string, bool) {
	v := strings.TrimSpace(raw)

	// Strip a trailing inline comment when not inside quotes. We do this
	// before quote stripping so `export FOO=bar  # note` works.
	if !strings.HasPrefix(v, "'") && !strings.HasPrefix(v, "\"") {
		if hash := strings.Index(v, " #"); hash >= 0 {
			v = strings.TrimSpace(v[:hash])
		}
		if hash := strings.Index(v, "\t#"); hash >= 0 {
			v = strings.TrimSpace(v[:hash])
		}
	}

	switch {
	case strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") && len(v) >= 2:
		// Single quotes: literal, no expansion. Safe.
		return v[1 : len(v)-1], true
	case strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"") && len(v) >= 2:
		// Double quotes: variable expansion would happen in a real shell.
		// Reject if any expansion characters present.
		inner := v[1 : len(v)-1]
		if strings.ContainsAny(inner, "$`") {
			return "", false
		}
		return inner, true
	default:
		// Bare value. Reject anything that would be expanded or split.
		if strings.ContainsAny(v, " \t$`\"'") {
			return "", false
		}
		// Reject obvious garbage like trailing semicolons.
		if strings.HasSuffix(v, ";") {
			v = strings.TrimRight(v, ";")
		}
		if v == "" {
			return "", false
		}
		return v, true
	}
}
