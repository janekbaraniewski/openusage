package detect

import (
	"bufio"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// detectAiderConfig parses Aider's documented credential locations and adopts
// any keys it finds as standard provider accounts.
//
// Aider's documented behaviour (https://aider.chat/docs/config/api-keys.html):
//   - .aider.conf.yml is searched in $HOME, the closest git repo root, and
//     the current working directory; later files override earlier ones.
//   - .env is searched in the same three locations with the same precedence.
//   - YAML keys: `openai-api-key`, `anthropic-api-key` (dedicated scalars),
//     plus list-form `api-key:` with `<provider>=<value>` strings.
//   - .env files use the standard provider env-var names (OPENAI_API_KEY etc.).
//
// Privacy: this detector ONLY runs if detectAider has registered the Aider
// binary in this run. Without that gate we'd be scanning every `.env` in any
// cwd or git root we happen to be launched from, which is too broad for a
// user who has never installed Aider.
//
// We treat env vars as absolute truth: any var set in os.Getenv wins and we
// skip adopting the value from a file. Within files we honour Aider's
// last-loaded-wins precedence by walking cwd first; addAccount's id-dedupe
// makes earlier scope's value win. We interleave .aider.conf.yml and .env at
// each scope so cwd/.env beats home/.aider.conf.yml (Aider treats them as
// equivalent at the same scope).
func detectAiderConfig(result *Result) {
	if !aiderToolDetected(result) {
		return
	}

	home := homeDir()
	if home == "" {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("[detect] aider: cwd unavailable: %v", err)
		cwd = ""
	}
	gitRoot := nearestGitRoot(cwd)

	var paths []string
	for _, scope := range []string{cwd, gitRoot, home} {
		if scope == "" {
			continue
		}
		paths = append(paths,
			filepath.Join(scope, ".aider.conf.yml"),
			filepath.Join(scope, ".env"),
		)
	}

	for _, p := range uniqueExisting(paths) {
		switch filepath.Base(p) {
		case ".aider.conf.yml":
			adoptAiderYAML(result, p)
		case ".env":
			adoptAiderDotenv(result, p)
		}
	}
}

// aiderToolDetected reports whether detectAider already added the Aider
// binary to result.Tools in this run.
func aiderToolDetected(result *Result) bool {
	for _, t := range result.Tools {
		if t.Name == "Aider" {
			return true
		}
	}
	return false
}

// nearestGitRoot walks up from start until it finds a directory containing
// a `.git` entry (file or dir — git worktrees use a regular file). Returns
// "" if none found or start is empty.
func nearestGitRoot(start string) string {
	if start == "" {
		return ""
	}
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// uniqueExisting returns the input paths in order, dropping duplicates and
// non-existent files. Empty entries are ignored.
func uniqueExisting(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
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

// aiderYAML matches the subset of .aider.conf.yml fields we care about.
// Aider has many other fields; we ignore them.
type aiderYAML struct {
	OpenAIAPIKey    string   `yaml:"openai-api-key"`
	AnthropicAPIKey string   `yaml:"anthropic-api-key"`
	APIKeyList      []string `yaml:"api-key"`
}

func adoptAiderYAML(result *Result, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("[detect] aider %s read error: %v", path, err)
		}
		return
	}
	var cfg aiderYAML
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Printf("[detect] aider %s parse error: %v", path, err)
		return
	}
	source := "aider_yaml:" + path

	for _, d := range []struct {
		envVar string
		value  string
	}{
		{"OPENAI_API_KEY", cfg.OpenAIAPIKey},
		{"ANTHROPIC_API_KEY", cfg.AnthropicAPIKey},
	} {
		if d.value == "" {
			continue
		}
		mapping, ok := envKeyByVar[d.envVar]
		if !ok {
			continue
		}
		adoptAPIKey(result, mapping, d.value, source)
	}

	// List form: each entry is "<provider>=<key>" where <provider> is
	// Aider's own short name. envKeyByAiderShortName indexes those names.
	for _, entry := range cfg.APIKeyList {
		entry = strings.TrimSpace(entry)
		eq := strings.IndexByte(entry, '=')
		if eq <= 0 {
			continue
		}
		shortName := strings.ToLower(strings.TrimSpace(entry[:eq]))
		value := strings.TrimSpace(entry[eq+1:])
		mapping, ok := envKeyByAiderShortName[shortName]
		if !ok {
			log.Printf("[detect] aider %s: unknown provider %q in api-key list, skipping", path, shortName)
			continue
		}
		if value == "" {
			continue
		}
		adoptAPIKey(result, mapping, value, source)
	}
}

func adoptAiderDotenv(result *Result, path string) {
	f, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("[detect] aider %s read error: %v", path, err)
		}
		return
	}
	defer f.Close()
	source := "aider_dotenv:" + path

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		name, value, ok := parseExportLine(scanner.Text())
		if !ok {
			continue
		}
		mapping, known := envKeyByVar[name]
		if !known {
			continue
		}
		adoptAPIKey(result, mapping, value, source)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[detect] aider %s scan error: %v", path, err)
	}
}

