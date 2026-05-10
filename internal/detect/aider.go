package detect

import (
	"bufio"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/janekbaraniewski/openusage/internal/core"
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
// We treat env vars as absolute truth: any var set in os.Getenv wins and we
// skip adopting the value from a file. Within files, we honour Aider's
// last-loaded-wins precedence (cwd → git-root → home reversed).
func detectAiderConfig(result *Result) {
	home := homeDir()
	if home == "" {
		return
	}
	cwd, _ := os.Getwd()
	gitRoot := nearestGitRoot(cwd)

	// Aider's documented order is "home, git-root, cwd; later wins". We walk
	// in reverse (cwd first) so the first hit per var also wins; later hits
	// are no-ops because addAccount de-dupes by ID.
	yamlPaths := uniqueExisting([]string{
		filepath.Join(cwd, ".aider.conf.yml"),
		filepath.Join(gitRoot, ".aider.conf.yml"),
		filepath.Join(home, ".aider.conf.yml"),
	})
	envPaths := uniqueExisting([]string{
		filepath.Join(cwd, ".env"),
		filepath.Join(gitRoot, ".env"),
		filepath.Join(home, ".env"),
	})

	for _, p := range yamlPaths {
		adoptAiderYAML(result, p)
	}
	for _, p := range envPaths {
		adoptAiderDotenv(result, p)
	}
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

	dedicated := []struct {
		envVar string
		value  string
	}{
		{"OPENAI_API_KEY", cfg.OpenAIAPIKey},
		{"ANTHROPIC_API_KEY", cfg.AnthropicAPIKey},
	}
	for _, d := range dedicated {
		if d.value == "" {
			continue
		}
		registerAiderKey(result, path, "aider_yaml", d.envVar, d.value)
	}

	// List form: each entry is "<provider>=<key>". The provider name is
	// Aider's own taxonomy (e.g. "gemini", "openrouter") which we map back
	// to our env-var-name table. Provider names without a known mapping are
	// silently skipped — we'd have nowhere to store them.
	for _, entry := range cfg.APIKeyList {
		entry = strings.TrimSpace(entry)
		eq := strings.IndexByte(entry, '=')
		if eq <= 0 {
			continue
		}
		provider := strings.TrimSpace(entry[:eq])
		value := strings.TrimSpace(entry[eq+1:])
		envVar := aiderProviderToEnvVar(provider)
		if envVar == "" || value == "" {
			continue
		}
		registerAiderKey(result, path, "aider_yaml", envVar, value)
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

	knownVars := make(map[string]struct{}, len(envKeyMapping))
	for _, m := range envKeyMapping {
		knownVars[m.EnvVar] = struct{}{}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		name, value, ok := parseExportLine(scanner.Text())
		if !ok {
			continue
		}
		if _, known := knownVars[name]; !known {
			continue
		}
		registerAiderKey(result, path, "aider_dotenv", name, value)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[detect] aider %s scan error: %v", path, err)
	}
}

// registerAiderKey adopts a (var, value) pair as a provider account, honouring
// the "env var wins" rule. Idempotent on account id via addAccount.
func registerAiderKey(result *Result, path, source, envVar, value string) {
	if os.Getenv(envVar) != "" {
		return
	}
	mapping, ok := mappingForEnvVar(envVar)
	if !ok {
		return
	}
	acct := core.AccountConfig{
		ID:        mapping.AccountID,
		Provider:  mapping.Provider,
		Auth:      "api_key",
		APIKeyEnv: envVar,
		Token:     value,
	}
	acct.SetHint("credential_source", source+":"+path)
	before := len(result.Accounts)
	addAccount(result, acct)
	if len(result.Accounts) > before {
		log.Printf("[detect] aider %s → %s/%s (%s=%s)",
			path, mapping.Provider, mapping.AccountID, envVar, maskKey(value))
	}
}

// mappingForEnvVar looks up the (provider, account-id) for an env var. Returns
// false if the env var is not in our known map.
func mappingForEnvVar(envVar string) (envKeyMappingEntry, bool) {
	for _, m := range envKeyMapping {
		if m.EnvVar == envVar {
			return envKeyMappingEntry{EnvVar: m.EnvVar, Provider: m.Provider, AccountID: m.AccountID}, true
		}
	}
	return envKeyMappingEntry{}, false
}

// aiderProviderToEnvVar maps Aider's provider taxonomy (used in `api-key:`
// list entries like `gemini=...`) back to the standard env-var name we use
// elsewhere. Aider names tend to be the LiteLLM short form.
func aiderProviderToEnvVar(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "openai":
		return "OPENAI_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "gemini", "google":
		return "GEMINI_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	case "deepseek":
		return "DEEPSEEK_API_KEY"
	case "groq":
		return "GROQ_API_KEY"
	case "mistral":
		return "MISTRAL_API_KEY"
	case "xai", "grok":
		return "XAI_API_KEY"
	case "moonshot", "moonshotai":
		return "MOONSHOT_API_KEY"
	}
	return ""
}
