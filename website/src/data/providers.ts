export interface Provider {
  name: string;
  slug: string;
  icon: string;
  detection: string;
  category: "agent" | "api" | "local";
}

export const providers: Provider[] = [
  // Coding agents & IDEs
  { name: "Claude Code", slug: "claude-code", icon: "claude", detection: "claude binary", category: "agent" },
  { name: "Cursor", slug: "cursor", icon: "cursor", detection: "cursor binary", category: "agent" },
  { name: "GitHub Copilot", slug: "copilot", icon: "githubcopilot", detection: "gh CLI", category: "agent" },
  { name: "Codex CLI", slug: "codex-cli", icon: "openai", detection: "codex binary", category: "agent" },
  { name: "Gemini CLI", slug: "gemini-cli", icon: "googlegemini", detection: "gemini binary", category: "agent" },
  { name: "OpenCode", slug: "opencode", icon: "openai", detection: "OPENCODE_API_KEY", category: "agent" },
  { name: "Ollama", slug: "ollama", icon: "ollama", detection: "OLLAMA_HOST / binary", category: "local" },
  // API platforms
  { name: "OpenAI", slug: "openai", icon: "openai", detection: "OPENAI_API_KEY", category: "api" },
  { name: "Anthropic", slug: "anthropic", icon: "anthropic", detection: "ANTHROPIC_API_KEY", category: "api" },
  { name: "OpenRouter", slug: "openrouter", icon: "openrouter", detection: "OPENROUTER_API_KEY", category: "api" },
  { name: "Groq", slug: "groq", icon: "groq", detection: "GROQ_API_KEY", category: "api" },
  { name: "Mistral AI", slug: "mistral", icon: "mistralai", detection: "MISTRAL_API_KEY", category: "api" },
  { name: "DeepSeek", slug: "deepseek", icon: "deepseek", detection: "DEEPSEEK_API_KEY", category: "api" },
  { name: "xAI", slug: "xai", icon: "xai", detection: "XAI_API_KEY", category: "api" },
  { name: "Gemini API", slug: "gemini-api", icon: "googlegemini", detection: "GEMINI_API_KEY", category: "api" },
  { name: "Alibaba Cloud", slug: "alibaba", icon: "alibabacloud", detection: "ALIBABA_CLOUD_API_KEY", category: "api" },
];

export const providerScreenshots = [
  { name: "Claude Code", file: "claudecode.png", desc: "Daily activity, per-model tokens, billing blocks, burn rate" },
  { name: "Cursor", file: "cursor.png", desc: "Plan spend & limits, per-model aggregation, Composer sessions" },
  { name: "OpenRouter", file: "openrouter.png", desc: "Credits, activity, per-model breakdown across API endpoints" },
  { name: "GitHub Copilot", file: "copilot.png", desc: "Chat & completions quota, org billing, session tracking" },
  { name: "Gemini CLI", file: "gemini.png", desc: "OAuth status, conversation count, per-model tokens" },
  { name: "Codex CLI", file: "codex.png", desc: "Session tokens, per-model breakdown, credits, rate limits" },
];
