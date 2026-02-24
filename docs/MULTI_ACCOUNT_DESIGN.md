# Multi-Account Support: Research & Design Document

## Executive Summary

This document presents a comprehensive design for adding multi-account support to OpenUsage. The goal is to enable users to track AI usage across multiple accounts (work, personal, different organizations) within a single OpenUsage instance.

## 1. Current State Analysis

### 1.1 Account Identity Model

Currently, OpenUsage uses a simple account identification model:

```go
type AccountConfig struct {
    ID         string            // Unique account identifier
    Provider   string            // Provider ID (e.g., "openai", "claude_code")
    Auth       string            // "api_key", "oauth", "cli", "local", "token"
    APIKeyEnv  string            // Env var name for API key
    Token      string            // Runtime-only (never persisted)
    ExtraData  map[string]string // Runtime-only extra data
}
```

**Current Limitations:**
- Accounts are keyed by `ID` alone (not `ID + Provider` combination)
- No built-in support for multiple accounts of the same provider
- No account display names or metadata
- Limited credential storage (only API keys in credentials.json)

### 1.2 Provider Authentication Patterns

| Provider | Auth Type | Storage Location | Multi-Account Support |
|----------|-----------|------------------|----------------------|
| **API Key Providers** (OpenAI, Anthropic, etc.) | API Key | Environment variables | ❌ Limited - one env var per provider |
| **Cursor** | Token | SQLite DB (`~/.cursor/state.vscdb`) | ❌ Single account |
| **Codex** | Token | JSON file (`~/.codex/auth.json`) | ❌ Single account |
| **Gemini CLI** | OAuth | JSON files (`~/.gemini/`) | ❌ Single active account |
| **GitHub Copilot** | CLI | `gh` CLI authentication | ⚠️ Via `gh auth switch` |
| **Claude Code** | Local + OAuth | `~/.claude.json`, `~/.claude/` | ❌ Single account |

### 1.3 Configuration Storage

Current config structure (`~/.config/openusage/settings.json`):

```json
{
  "accounts": [
    {
      "id": "openai",
      "provider": "openai",
      "auth": "api_key",
      "api_key_env": "OPENAI_API_KEY"
    }
  ],
  "auto_detected_accounts": [...]
}
```

Credentials stored separately in `~/.config/openusage/credentials.json`:

```json
{
  "keys": {
    "openai": "sk-...",
    "anthropic": "sk-ant-..."
  }
}
```

## 2. Multi-Account Architecture Design

### 2.1 Account Identity Redesign

**Proposed Change:** Account uniqueness should be `ID + Provider` composite key.

```go
// AccountIdentity uniquely identifies an account
type AccountIdentity struct {
    ID       string // User-defined identifier (e.g., "openai-work", "openai-personal")
    Provider string // Provider ID (e.g., "openai", "claude_code")
}

func (a AccountIdentity) String() string {
    return fmt.Sprintf("%s/%s", a.Provider, a.ID)
}
```

**Rationale:**
- Allows multiple accounts per provider (e.g., `openai/work`, `openai/personal`)
- Maintains backward compatibility (existing single accounts keep working)
- Enables provider-specific account grouping in UI

### 2.2 Enhanced Account Configuration

```go
type AccountConfig struct {
    // Identity
    ID         string `json:"id"`        // User-defined account identifier
    Provider   string `json:"provider"`  // Provider ID
    
    // Display
    DisplayName string `json:"display_name,omitempty"` // Human-readable name (e.g., "Work Account")
    Email       string `json:"email,omitempty"`        // Account email for identification
    Organization string `json:"organization,omitempty"` // Org/team name
    
    // Authentication
    Auth       string            `json:"auth,omitempty"`        // Method: "api_key", "oauth", "cli", "local"
    APIKeyEnv  string            `json:"api_key_env,omitempty"` // Env var name (for api_key auth)
    ConfigDir  string            `json:"config_dir,omitempty"`  // Custom config directory path
    Profile    string            `json:"profile,omitempty"`     // CLI profile name (e.g., for 'gh auth switch')
    
    // Provider-specific settings
    ProbeModel string            `json:"probe_model,omitempty"`
    Binary     string            `json:"binary,omitempty"`
    BaseURL    string            `json:"base_url,omitempty"`
    
    // Runtime (never persisted)
    Token      string            `json:"-"`
    ExtraData  map[string]string `json:"-"`
}
```

### 2.3 Credential Storage Enhancement

Current `credentials.json` only supports API keys. We need to expand it:

```go
type Credentials struct {
    Version int                        `json:"version"` // For future migrations
    APIKeys map[string]string          `json:"api_keys"` // account ID → API key
    OAuth   map[string]OAuthCredential `json:"oauth"`    // account ID → OAuth tokens
    Tokens  map[string]string          `json:"tokens"`   // account ID → bearer tokens
}

type OAuthCredential struct {
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token"`
    ExpiresAt    time.Time `json:"expires_at"`
    Provider     string    `json:"provider"` // Provider that issued these credentials
}
```

**Security Considerations:**
- File permissions: `0600` (already implemented)
- Consider OS keychain integration for production use
- Tokens should never be logged or displayed

## 3. Provider-Specific Multi-Account Strategies

### 3.1 API Key Providers (OpenAI, Anthropic, etc.)

**Difficulty: EASY** ✅

**Strategy: Multiple Environment Variables**

```bash
# Work account
export OPENAI_API_KEY_WORK="sk-work..."

# Personal account  
export OPENAI_API_KEY_PERSONAL="sk-personal..."
```

```json
{
  "accounts": [
    {
      "id": "work",
      "provider": "openai",
      "auth": "api_key",
      "api_key_env": "OPENAI_API_KEY_WORK",
      "display_name": "OpenAI - Work"
    },
    {
      "id": "personal", 
      "provider": "openai",
      "auth": "api_key",
      "api_key_env": "OPENAI_API_KEY_PERSONAL",
      "display_name": "OpenAI - Personal"
    }
  ]
}
```

**Implementation:**
1. Modify `detectEnvKeys()` to detect multiple env var patterns
2. Support suffix pattern: `{PROVIDER}_API_KEY_{SUFFIX}`
3. Auto-generate account IDs from suffix

### 3.2 Claude Code - The Challenge

**Difficulty: HARD** 🔴

**Current Architecture:**
- Auth stored in `~/.claude.json` (OAuth data, email, org UUID)
- Stats stored in `~/.claude/stats-cache.json`
- No native multi-account support in Claude Code CLI

**Analysis: Can We Implement Auth Like Claude/Opencode?**

**Short Answer: Not directly.** Claude Code is a CLI tool that manages its own authentication. OpenUsage is a usage tracker that reads Claude Code's data, not a Claude client.

**However, there are viable workarounds:**

#### Option A: Config Directory Isolation (RECOMMENDED)

**Concept:** Users maintain separate Claude Code installations with different config directories.

**How it works:**
```bash
# Work account
export CLAUDE_CONFIG_DIR="$HOME/.claude-work"
claude auth login  # Authenticates work account

# Personal account
export CLAUDE_CONFIG_DIR="$HOME/.claude-personal"
claude auth login  # Authenticates personal account
```

**OpenUsage Configuration:**
```json
{
  "accounts": [
    {
      "id": "work",
      "provider": "claude_code",
      "auth": "local",
      "config_dir": "$HOME/.claude-work",
      "display_name": "Claude Code - Work"
    },
    {
      "id": "personal",
      "provider": "claude_code", 
      "auth": "local",
      "config_dir": "$HOME/.claude-personal",
      "display_name": "Claude Code - Personal"
    }
  ]
}
```

**Pros:**
- Works today with no changes to Claude Code
- Clean separation of accounts
- Can use both accounts simultaneously (different terminal sessions)

**Cons:**
- Requires user to manage multiple config directories
- No automatic account switching
- Each config directory needs separate Claude Code installation/cache

**Implementation in OpenUsage:**

Modify `claude_code` provider to accept custom config directory:

```go
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
    // Use custom config dir if specified
    configDir := filepath.Join(homeDir(), ".claude")
    if acct.ConfigDir != "" {
        configDir = os.ExpandEnv(acct.ConfigDir)
    }
    
    statsFile := filepath.Join(configDir, "stats-cache.json")
    accountFile := filepath.Join(homeDir(), ".claude.json")
    
    // If custom config dir, look for account file there too
    if acct.ConfigDir != "" {
        accountFile = filepath.Join(configDir, "account.json")
    }
    
    // ... rest of fetch logic using these paths
}
```

#### Option B: Session Management Wrapper

**Concept:** OpenUsage manages multiple `.claude.json` files and swaps them.

**How it works:**
1. Store multiple Claude auth files: `~/.openusage/claude-sessions/work.json`, `personal.json`
2. User "activates" an account before using Claude
3. OpenUsage swaps the active `~/.claude.json` symlink

**Implementation:**
```go
// ActivateClaudeAccount switches to a different Claude account
func ActivateClaudeAccount(accountID string) error {
    sessionFile := filepath.Join(ConfigDir(), "claude-sessions", accountID + ".json")
    claudeJson := filepath.Join(homeDir(), ".claude.json")
    
    // Backup current
    if _, err := os.Stat(claudeJson); err == nil {
        backup := filepath.Join(ConfigDir(), "claude-sessions", "_current.json")
        os.Rename(claudeJson, backup)
    }
    
    // Activate new
    return os.Symlink(sessionFile, claudeJson)
}
```

**Pros:**
- Single Claude Code installation
- Fast account switching

**Cons:**
- Can't use multiple accounts simultaneously
- Risk of data loss if switch happens mid-session
- More complex to implement

**Verdict:** Option A (Config Directory Isolation) is cleaner and safer.

### 3.3 Cursor

**Difficulty: MEDIUM** 🟡

**Strategy:** Config Directory Override (similar to Claude Code)

Cursor stores auth in `~/.cursor/state.vscdb` (SQLite). It may support custom config directories.

```json
{
  "accounts": [
    {
      "id": "work",
      "provider": "cursor",
      "config_dir": "$HOME/.cursor-work",
      "display_name": "Cursor - Work"
    }
  ]
}
```

### 3.4 GitHub Copilot

**Difficulty: EASY** ✅

**Strategy:** Use `gh` CLI's Native Multi-Account Support

The `gh` CLI already supports multiple accounts:

```bash
# Add work account
gh auth login --hostname github.com --web
gh auth switch --hostname github.com --user work-user

# Add personal account  
gh auth login --hostname github.com --web
gh auth switch --hostname github.com --user personal-user
```

**OpenUsage Configuration:**
```json
{
  "accounts": [
    {
      "id": "work",
      "provider": "copilot",
      "auth": "cli",
      "profile": "work-user",  // gh username
      "display_name": "GitHub Copilot - Work"
    },
    {
      "id": "personal",
      "provider": "copilot", 
      "auth": "cli",
      "profile": "personal-user",
      "display_name": "GitHub Copilot - Personal"
    }
  ]
}
```

**Implementation:**
```go
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
    binary := acct.Binary
    if binary == "" {
        binary = "gh"
    }
    
    // Switch to the correct account before fetching
    if acct.Profile != "" {
        cmd := exec.CommandContext(ctx, binary, "auth", "switch", "--user", acct.Profile)
        if err := cmd.Run(); err != nil {
            return core.QuotaSnapshot{}, fmt.Errorf("switching gh account: %w", err)
        }
    }
    
    // Now fetch rate limits
    cmd := exec.CommandContext(ctx, binary, "api", "/rate_limit")
    // ... rest of implementation
}
```

### 3.5 Gemini CLI

**Difficulty: MEDIUM** 🟡

**Current Limitation:** Gemini CLI stores OAuth in `~/.gemini/oauth_creds.json` with only one active account.

**Strategy:** Multiple Config Directories

```bash
# Work account
export GEMINI_CONFIG_DIR="$HOME/.gemini-work"
gemini auth login

# Personal account
export GEMINI_CONFIG_DIR="$HOME/.gemini-personal"
gemini auth login
```

**OpenUsage Configuration:**
```json
{
  "accounts": [
    {
      "id": "work",
      "provider": "gemini_cli",
      "auth": "oauth",
      "config_dir": "$HOME/.gemini-work",
      "display_name": "Gemini CLI - Work"
    }
  ]
}
```

## 4. Authentication Management System

### 4.1 Auth Flow Interface

```go
// AccountAuthenticator handles authentication for a specific provider
type AccountAuthenticator interface {
    // AuthType returns the authentication method type
    AuthType() string
    
    // SupportsMultiAccount returns true if this provider supports multiple accounts
    SupportsMultiAccount() bool
    
    // DetectAccounts detects all available accounts for this provider
    DetectAccounts(ctx context.Context) ([]AccountConfig, error)
    
    // Authenticate performs interactive authentication for a new account
    Authenticate(ctx context.Context, accountID string) (AccountConfig, error)
    
    // Validate checks if the account credentials are valid
    Validate(ctx context.Context, acct AccountConfig) error
    
    // Refresh refreshes expired credentials (for OAuth)
    Refresh(ctx context.Context, acct AccountConfig) (AccountConfig, error)
}
```

### 4.2 Account Manager

```go
type AccountManager struct {
    mu        sync.RWMutex
    accounts  map[string]AccountConfig // key: "provider/id"
    creds     Credentials
    configPath string
}

func (am *AccountManager) AddAccount(acct AccountConfig) error {
    key := acct.Identity().String()
    
    am.mu.Lock()
    defer am.mu.Unlock()
    
    // Validate uniqueness
    if _, exists := am.accounts[key]; exists {
        return fmt.Errorf("account %s already exists", key)
    }
    
    am.accounts[key] = acct
    return am.save()
}

func (am *AccountManager) RemoveAccount(provider, id string) error {
    key := fmt.Sprintf("%s/%s", provider, id)
    
    am.mu.Lock()
    defer am.mu.Unlock()
    
    delete(am.accounts, key)
    delete(am.creds.APIKeys, id)
    delete(am.creds.OAuth, id)
    delete(am.creds.Tokens, id)
    
    return am.save()
}

func (am *AccountManager) GetAccount(provider, id string) (AccountConfig, bool) {
    am.mu.RLock()
    defer am.mu.RUnlock()
    
    key := fmt.Sprintf("%s/%s", provider, id)
    acct, ok := am.accounts[key]
    return acct, ok
}

func (am *AccountManager) GetAccountsByProvider(provider string) []AccountConfig {
    am.mu.RLock()
    defer am.mu.RUnlock()
    
    var result []AccountConfig
    for _, acct := range am.accounts {
        if acct.Provider == provider {
            result = append(result, acct)
        }
    }
    return result
}

func (am *AccountManager) ResolveCredentials(acct AccountConfig) (string, error) {
    // Try runtime token first
    if acct.Token != "" {
        return acct.Token, nil
    }
    
    // Try credentials file
    am.mu.RLock()
    defer am.mu.RUnlock()
    
    switch acct.Auth {
    case "api_key":
        if key, ok := am.creds.APIKeys[acct.ID]; ok {
            return key, nil
        }
        // Fall back to environment variable
        if acct.APIKeyEnv != "" {
            return os.Getenv(acct.APIKeyEnv), nil
        }
        
    case "oauth":
        if oauth, ok := am.creds.OAuth[acct.ID]; ok {
            if time.Now().After(oauth.ExpiresAt) {
                return "", fmt.Errorf("oauth token expired")
            }
            return oauth.AccessToken, nil
        }
        
    case "token":
        if token, ok := am.creds.Tokens[acct.ID]; ok {
            return token, nil
        }
    }
    
    return "", fmt.Errorf("no credentials found for account %s", acct.ID)
}
```

### 4.3 Interactive Authentication Commands

New CLI commands for account management:

```bash
# List all accounts
openusage accounts list

# Add a new account (interactive)
openusage accounts add openai --id work --display-name "Work Account"
# Prompts for API key, stores securely

# Add with environment variable
openusage accounts add openai --id personal --env-var OPENAI_API_KEY_PERSONAL

# Add Claude Code account with custom config dir
openusage accounts add claude_code --id work --config-dir ~/.claude-work

# Remove an account
openusage accounts remove openai/work

# Verify account credentials
openusage accounts verify openai/work

# Set default account for a provider
openusage accounts set-default openai work
```

## 5. UI/UX Design

### 5.1 Account Display in Dashboard

**Current:** Provider-centric display
```
┌─ OpenAI ─────────────┐
│ Usage: 85%           │
│ Credits: $120/$150   │
└──────────────────────┘
```

**Proposed:** Account-centric display with grouping
```
┌─ OpenAI ─────────────────────────┐
│                                  │
│ ┌─ Work ─────────────┐          │
│ │ Usage: 85%         │          │
│ │ Credits: $120/$150 │          │
│ └────────────────────┘          │
│                                  │
│ ┌─ Personal ─────────┐          │
│ │ Usage: 12%         │          │
│ │ Credits: $44/$50   │          │
│ └────────────────────┘          │
│                                  │
└──────────────────────────────────┘
```

### 5.2 Account List View (New)

A dedicated view for managing accounts:

```
┌─ Accounts ───────────────────────────────┐
│                                          │
│ API Key Providers:                       │
│   [+] OpenAI                             │
│       ├── Work (work@company.com)    [✓] │
│       └── Personal (me@email.com)    [✓] │
│   [+] Anthropic                          │
│       └── Default                    [✓] │
│                                          │
│ CLI Providers:                           │
│   [+] Claude Code                        │
│       ├── Work (~/.claude-work)      [✓] │
│       └── Personal (~/.claude-home)  [!] │
│                                          │
│ [Add Account] [Remove] [Verify]          │
└──────────────────────────────────────────┘
```

### 5.3 Account Selector in Detail View

When viewing provider details, show account selector:

```
┌─ OpenAI ───────────────────────┐
│                                │
│ Account: [Work ▼]              │
│         [Personal]             │
│                                │
│ Usage:                           │
│ ████████████████░░░░ 85%       │
│                                │
│ Credits: $120.50 / $150.00     │
│                                │
└────────────────────────────────┘
```

## 6. Migration Strategy

### 6.1 Backward Compatibility

**Goal:** Existing single-account users should continue working without changes.

**Implementation:**
1. Keep legacy account IDs working (e.g., "openai" → "openai/default")
2. Auto-convert single accounts to new format on first run
3. Support both old and new config formats during transition

**Migration Code:**
```go
func MigrateLegacyAccounts(cfg *Config) {
    for i, acct := range cfg.Accounts {
        // If account has no display name, use provider name
        if acct.DisplayName == "" {
            cfg.Accounts[i].DisplayName = providerName(acct.Provider)
        }
        
        // If account ID is just the provider name, append "/default"
        if acct.ID == acct.Provider {
            cfg.Accounts[i].ID = "default"
        }
    }
}
```

### 6.2 Config Versioning

Add version field to config for future migrations:

```json
{
  "version": 2,
  "accounts": [...]
}
```

## 7. Security Considerations

### 7.1 Credential Storage

- Continue using `0600` permissions on credentials.json
- Consider OS keychain integration (macOS Keychain, Windows Credential Manager, Linux Secret Service)
- Never log or display full credentials
- Support credential encryption at rest (optional)

### 7.2 Environment Variables

- Warn users that env vars may be visible in process lists
- Recommend using credentials.json for production/multi-account setups
- Support `.env` file loading for local development

### 7.3 Token Refresh

- OAuth tokens should auto-refresh when expired
- Store refresh tokens securely
- Handle refresh failures gracefully (mark account as needing re-auth)

## 8. Implementation Roadmap

### Phase 1: Foundation (Week 1-2)
1. ✅ Update AccountConfig struct with new fields
2. ✅ Update Credentials struct to support multiple credential types
3. ✅ Implement AccountManager with CRUD operations
4. ✅ Add config versioning and migration
5. ✅ Update Engine to use new account identity model

### Phase 2: API Key Providers (Week 3)
1. ✅ Update detection logic to support multiple env var patterns
2. ✅ Implement `accounts` CLI commands
3. ✅ Add account storage/retrieval to credentials.json
4. ✅ Test with OpenAI, Anthropic, etc.

### Phase 3: CLI Providers - Config Directory Support (Week 4)
1. ✅ Implement ConfigDir support in Claude Code provider
2. ✅ Implement ConfigDir support in Cursor provider
3. ✅ Implement ConfigDir support in Gemini CLI provider
4. ✅ Add documentation for setting up multiple config directories

### Phase 4: GitHub Copilot Multi-Account (Week 5)
1. ✅ Implement profile switching in Copilot provider
2. ✅ Test with multiple `gh` accounts
3. ✅ Add documentation

### Phase 5: UI Updates (Week 6)
1. ✅ Update dashboard to show multiple accounts per provider
2. ✅ Add account list view
3. ✅ Add account selector in detail view
4. ✅ Update themes/styling for account grouping

### Phase 6: Testing & Documentation (Week 7)
1. ✅ Comprehensive testing of all provider combinations
2. ✅ Security audit of credential storage
3. ✅ User documentation
4. ✅ Migration guide for existing users

## 9. Open Questions

1. **Should we implement OAuth flows directly?** 
   - Pros: Better UX, no need for external tools
   - Cons: Complex, security responsibility, maintenance burden
   - **Recommendation:** Defer to Phase 2, focus on config directory approach first

2. **How to handle account name collisions?**
   - If user has "work" accounts for both OpenAI and Anthropic
   - **Solution:** Display as "OpenAI/work" and "Anthropic/work" in global context

3. **Should accounts be shareable across teams?**
   - Should we support account configs without credentials (user provides at runtime)?
   - **Recommendation:** No, keep it personal. Team sharing adds complexity.

4. **What about rate limiting across multiple accounts?**
   - Should we throttle requests when user has many accounts?
   - **Recommendation:** Implement per-provider rate limiting, parallel fetching with limits

## 10. Conclusion

Multi-account support is feasible and valuable. The key insight is that **we don't need to implement auth flows ourselves** - we can leverage:

1. **Environment variables** for API key providers
2. **Config directory isolation** for CLI tools (Claude Code, Cursor, Gemini)
3. **Native multi-account support** where available (GitHub Copilot via `gh`)

The main work involves:
1. Refactoring account identity to support `provider/id` composite keys
2. Updating all providers to accept custom config directories
3. Building account management UI/CLI
4. Maintaining backward compatibility

**Recommended Next Steps:**
1. Implement Phase 1 (Foundation) - update core data structures
2. Create proof-of-concept with Claude Code multi-config
3. Get user feedback on the config directory approach
4. Iterate on UI design
5. Proceed with full implementation

---

## Appendix A: Example Configuration

### Before (Single Account)
```json
{
  "accounts": [
    {
      "id": "openai",
      "provider": "openai",
      "auth": "api_key",
      "api_key_env": "OPENAI_API_KEY"
    },
    {
      "id": "claude-code",
      "provider": "claude_code",
      "auth": "local"
    }
  ]
}
```

### After (Multi-Account)
```json
{
  "version": 2,
  "accounts": [
    {
      "id": "work",
      "provider": "openai",
      "auth": "api_key",
      "api_key_env": "OPENAI_API_KEY_WORK",
      "display_name": "OpenAI - Work",
      "email": "work@company.com"
    },
    {
      "id": "personal",
      "provider": "openai",
      "auth": "api_key",
      "api_key_env": "OPENAI_API_KEY_PERSONAL",
      "display_name": "OpenAI - Personal",
      "email": "me@gmail.com"
    },
    {
      "id": "work",
      "provider": "claude_code",
      "auth": "local",
      "config_dir": "$HOME/.claude-work",
      "display_name": "Claude Code - Work",
      "email": "work@company.com"
    },
    {
      "id": "personal",
      "provider": "claude_code",
      "auth": "local",
      "config_dir": "$HOME/.claude-personal",
      "display_name": "Claude Code - Personal",
      "email": "me@gmail.com"
    }
  ]
}
```

## Appendix B: Directory Structure for Multi-Account

```
~/.config/openusage/
├── settings.json              # Main config
├── credentials.json           # Encrypted credentials
└── claude-sessions/           # Claude Code auth backups (optional)
    ├── work.json
    └── personal.json

~/.claude-work/                # Work Claude Code installation
├── stats-cache.json
└── projects/

~/.claude-personal/            # Personal Claude Code installation
├── stats-cache.json
└── projects/
```

## Appendix C: Quick Start Guide for Users

### Setting Up Multiple Claude Code Accounts

```bash
# 1. Set up work account
export CLAUDE_CONFIG_DIR="$HOME/.claude-work"
claude auth login
# ... complete OAuth flow with work email ...

# 2. Set up personal account
export CLAUDE_CONFIG_DIR="$HOME/.claude-personal"
claude auth login
# ... complete OAuth flow with personal email ...

# 3. Add both accounts to OpenUsage
openusage accounts add claude_code --id work --config-dir ~/.claude-work --display-name "Work"
openusage accounts add claude_code --id personal --config-dir ~/.claude-personal --display-name "Personal"

# 4. Run OpenUsage
openusage
```

### Using Multiple OpenAI Accounts

```bash
# 1. Set environment variables
export OPENAI_API_KEY_WORK="sk-work-..."
export OPENAI_API_KEY_PERSONAL="sk-personal-..."

# 2. Add to OpenUsage
openusage accounts add openai --id work --env-var OPENAI_API_KEY_WORK --display-name "Work"
openusage accounts add openai --id personal --env-var OPENAI_API_KEY_PERSONAL --display-name "Personal"

# 3. Or let auto-detection find them
openusage --auto-detect
```
