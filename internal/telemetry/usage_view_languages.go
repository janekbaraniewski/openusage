package telemetry

import "strings"

func inferLanguageFromFilePath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	base := p
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		base = p[idx+1:]
	}
	if idx := strings.LastIndex(base, "\\"); idx >= 0 {
		base = base[idx+1:]
	}
	switch strings.ToLower(base) {
	case "dockerfile":
		return "docker"
	case "makefile":
		return "make"
	}
	idx := strings.LastIndex(p, ".")
	if idx < 0 {
		if lang := extToLanguage("." + strings.ToLower(p)); lang != "" {
			return lang
		}
		return ""
	}
	return extToLanguage(strings.ToLower(p[idx:]))
}

func extToLanguage(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".tf", ".tfvars", ".hcl":
		return "terraform"
	case ".sh", ".bash", ".zsh", ".fish":
		return "shell"
	case ".md", ".mdx":
		return "markdown"
	case ".json":
		return "json"
	case ".yml", ".yaml":
		return "yaml"
	case ".sql":
		return "sql"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".cs":
		return "csharp"
	case ".vue":
		return "vue"
	case ".svelte":
		return "svelte"
	case ".toml":
		return "toml"
	case ".xml":
		return "xml"
	case ".css", ".scss", ".less":
		return "css"
	case ".html", ".htm":
		return "html"
	case ".dart":
		return "dart"
	case ".zig":
		return "zig"
	case ".lua":
		return "lua"
	case ".r":
		return "r"
	case ".proto":
		return "protobuf"
	case ".ex", ".exs":
		return "elixir"
	case ".graphql", ".gql":
		return "graphql"
	}
	return ""
}
