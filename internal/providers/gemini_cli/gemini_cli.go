package gemini_cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	oauthClientID     = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	oauthClientSecret = "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"
	tokenEndpoint     = "https://oauth2.googleapis.com/token"

	codeAssistEndpoint   = "https://cloudcode-pa.googleapis.com"
	codeAssistAPIVersion = "v1internal"

	defaultUsageWindowLabel = "all-time"

	maxBreakdownMetrics = 8
	maxBreakdownRaw     = 6

	quotaNearLimitFraction = 0.15
)

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "gemini_cli",
			Info: core.ProviderInfo{
				Name:         "Gemini CLI",
				Capabilities: []string{"local_config", "oauth_status", "conversation_count", "local_sessions", "token_usage", "by_model", "by_client", "mcp_config", "code_generation", "quota_api"},
				DocURL:       "https://github.com/google-gemini/gemini-cli",
			},
			Auth: core.ProviderAuthSpec{
				Type: core.ProviderAuthTypeOAuth,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install and authenticate Gemini CLI locally.",
					"Verify OAuth credentials are available in the Gemini CLI config directory.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
	}
}

func (p *Provider) DetailWidget() core.DetailWidget {
	return core.CodingToolDetailWidget(true)
}

type oauthCreds struct {
	AccessToken  string `json:"access_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`
	ExpiryDate   int64  `json:"expiry_date"` // Unix millis
	RefreshToken string `json:"refresh_token"`
}

type googleAccounts struct {
	Active string   `json:"active"`
	Old    []string `json:"old"`
}

type geminiSettings struct {
	Security struct {
		Auth struct {
			SelectedType string `json:"selectedType"`
		} `json:"auth"`
	} `json:"security"`
	General struct {
		PreviewFeatures  bool `json:"previewFeatures"`
		EnableAutoUpdate bool `json:"enableAutoUpdate"`
	} `json:"general"`
	Experimental struct {
		Plan bool `json:"plan"`
	} `json:"experimental"`
	MCPServers map[string]geminiMCPServer `json:"mcpServers,omitempty"`
}

type geminiMCPServer struct {
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"`
}

type geminiMCPEnablement struct {
	Enabled bool `json:"enabled"`
}

type tokenRefreshResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

type loadCodeAssistRequest struct {
	CloudAICompanionProject string         `json:"cloudaicompanionProject,omitempty"`
	Metadata                clientMetadata `json:"metadata"`
}

type clientMetadata struct {
	IDEType    string `json:"ideType"`
	Platform   string `json:"platform"`
	PluginType string `json:"pluginType"`
	Project    string `json:"duetProject,omitempty"`
}

type loadCodeAssistResponse struct {
	CurrentTier             *geminiTierInfo            `json:"currentTier,omitempty"`
	AllowedTiers            []geminiTierInfo           `json:"allowedTiers,omitempty"`
	IneligibleTiers         []geminiIneligibleTier     `json:"ineligibleTiers,omitempty"`
	CloudAICompanionProject string                     `json:"cloudaicompanionProject,omitempty"`
	GCPManaged              bool                       `json:"gcpManaged,omitempty"`
	UpgradeSubscriptionURI  string                     `json:"upgradeSubscriptionUri,omitempty"`
	UpgradeSubscriptionText string                     `json:"upgradeSubscriptionText,omitempty"`
	UpgradeSubscriptionType string                     `json:"upgradeSubscriptionType,omitempty"`
	Diagnostics             map[string]json.RawMessage `json:"-"`
}

type geminiTierInfo struct {
	ID                                 string `json:"id,omitempty"`
	Name                               string `json:"name,omitempty"`
	Description                        string `json:"description,omitempty"`
	UserDefinedCloudAICompanionProject bool   `json:"userDefinedCloudaicompanionProject,omitempty"`
	IsDefault                          bool   `json:"isDefault,omitempty"`
	UsesGCPTOS                         bool   `json:"usesGcpTos,omitempty"`
}

type geminiIneligibleTier struct {
	ReasonCode    string `json:"reasonCode,omitempty"`
	ReasonMessage string `json:"reasonMessage,omitempty"`
	TierID        string `json:"tierId,omitempty"`
	TierName      string `json:"tierName,omitempty"`
}

type retrieveUserQuotaRequest struct {
	Project string `json:"project"`
}

type retrieveUserQuotaResponse struct {
	Buckets []bucketInfo `json:"buckets,omitempty"`
}

type bucketInfo struct {
	RemainingAmount   string   `json:"remainingAmount,omitempty"`
	RemainingFraction *float64 `json:"remainingFraction,omitempty"`
	ResetTime         string   `json:"resetTime,omitempty"` // ISO-8601
	TokenType         string   `json:"tokenType,omitempty"`
	ModelID           string   `json:"modelId,omitempty"`
}

type geminiChatFile struct {
	SessionID   string              `json:"sessionId"`
	StartTime   string              `json:"startTime"`
	LastUpdated string              `json:"lastUpdated"`
	ProjectHash string              `json:"projectHash"`
	Messages    []geminiChatMessage `json:"messages"`
}

type geminiChatMessage struct {
	ID        string              `json:"id,omitempty"`
	Type      string              `json:"type"`
	Timestamp string              `json:"timestamp"`
	Model     string              `json:"model"`
	Content   json.RawMessage     `json:"content,omitempty"`
	Tokens    *geminiMessageToken `json:"tokens,omitempty"`
	ToolCalls []geminiToolCall    `json:"toolCalls,omitempty"`
}

type geminiToolCall struct {
	ID                     string          `json:"id,omitempty"`
	Name                   string          `json:"name"`
	Status                 string          `json:"status,omitempty"`
	Timestamp              string          `json:"timestamp,omitempty"`
	DisplayName            string          `json:"displayName,omitempty"`
	Description            string          `json:"description,omitempty"`
	RenderOutputAsMarkdown *bool           `json:"renderOutputAsMarkdown,omitempty"`
	Result                 json.RawMessage `json:"result,omitempty"`
	ResultDisplay          json.RawMessage `json:"resultDisplay,omitempty"`
	Args                   json.RawMessage `json:"args,omitempty"`
}

type geminiDiffStat struct {
	ModelAddedLines   int `json:"model_added_lines"`
	ModelRemovedLines int `json:"model_removed_lines"`
	ModelAddedChars   int `json:"model_added_chars"`
	ModelRemovedChars int `json:"model_removed_chars"`
	UserAddedLines    int `json:"user_added_lines"`
	UserRemovedLines  int `json:"user_removed_lines"`
	UserAddedChars    int `json:"user_added_chars"`
	UserRemovedChars  int `json:"user_removed_chars"`
}

type geminiMessageToken struct {
	Input    int `json:"input"`
	Output   int `json:"output"`
	Cached   int `json:"cached"`
	Thoughts int `json:"thoughts"`
	Tool     int `json:"tool"`
	Total    int `json:"total"`
}

type tokenUsage struct {
	InputTokens       int
	CachedInputTokens int
	OutputTokens      int
	ReasoningTokens   int
	ToolTokens        int
	TotalTokens       int
}

type usageEntry struct {
	Name string
	Data tokenUsage
}

// HasChanged reports whether Gemini CLI's local data files have been modified since the given time.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	configDir := acct.Hint("config_dir", "")
	if configDir == "" {
		if home, _ := os.UserHomeDir(); home != "" {
			configDir = filepath.Join(home, ".gemini")
		}
	}
	if configDir == "" {
		return true, nil
	}
	return shared.AnyPathModifiedAfter([]string{
		filepath.Join(configDir, "antigravity/conversations"),
		filepath.Join(configDir, "settings.json"),
		filepath.Join(configDir, "oauth_creds.json"),
		filepath.Join(configDir, "tmp"),
	}, since), nil
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snap := core.UsageSnapshot{
		ProviderID:  p.ID(),
		AccountID:   acct.ID,
		Timestamp:   time.Now(),
		Status:      core.StatusOK,
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	configDir := acct.Hint("config_dir", "")
	if configDir == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			configDir = filepath.Join(home, ".gemini")
		}
	}
	if configDir == "" {
		snap.Status = core.StatusError
		snap.Message = "Cannot determine Gemini CLI config directory"
		return snap, nil
	}

	var hasData bool
	var creds oauthCreds

	oauthFile := filepath.Join(configDir, "oauth_creds.json")
	if data, err := os.ReadFile(oauthFile); err == nil {
		if json.Unmarshal(data, &creds) == nil {
			hasData = true

			if creds.ExpiryDate > 0 {
				expiry := time.Unix(creds.ExpiryDate/1000, 0)
				if time.Now().Before(expiry) {
					snap.Raw["oauth_status"] = "valid"
					snap.Raw["oauth_expires"] = expiry.Format(time.RFC3339)
				} else if creds.RefreshToken != "" {
					snap.Raw["oauth_status"] = "expired (will refresh)"
				} else {
					snap.Raw["oauth_status"] = "expired"
					snap.Raw["oauth_expired_at"] = expiry.Format(time.RFC3339)
					snap.Status = core.StatusAuth
					snap.Message = "OAuth token expired — run `gemini` to re-authenticate"
				}
			}

			if creds.Scope != "" {
				snap.Raw["oauth_scope"] = creds.Scope
			}
		}
	}

	accountsFile := filepath.Join(configDir, "google_accounts.json")
	if data, err := os.ReadFile(accountsFile); err == nil {
		var accounts googleAccounts
		if json.Unmarshal(data, &accounts) == nil {
			hasData = true
			if accounts.Active != "" {
				snap.Raw["account_email"] = accounts.Active
			}
		}
	}

	settingsFile := filepath.Join(configDir, "settings.json")
	if data, err := os.ReadFile(settingsFile); err == nil {
		var settings geminiSettings
		if json.Unmarshal(data, &settings) == nil {
			hasData = true
			if settings.Security.Auth.SelectedType != "" {
				snap.Raw["auth_type"] = settings.Security.Auth.SelectedType
			}
			if settings.Experimental.Plan {
				snap.Raw["plan_mode"] = "enabled"
			}
			if settings.General.PreviewFeatures {
				snap.Raw["preview_features"] = "enabled"
			}
			applyGeminiMCPMetadata(&snap, settings, filepath.Join(configDir, "mcp-server-enablement.json"))
		}
	}

	idFile := filepath.Join(configDir, "installation_id")
	if data, err := os.ReadFile(idFile); err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			snap.Raw["installation_id"] = id
		}
	}

	convDir := filepath.Join(configDir, "antigravity", "conversations")
	if entries, err := os.ReadDir(convDir); err == nil {
		count := 0
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".pb") {
				count++
			}
		}
		if count > 0 {
			hasData = true
			convCount := float64(count)
			snap.Metrics["total_conversations"] = core.Metric{
				Used:   &convCount,
				Unit:   "conversations",
				Window: "all-time",
			}
		}
	}

	sessionCount, err := p.readSessionUsageBreakdowns(filepath.Join(configDir, "tmp"), &snap)
	if err != nil {
		snap.Raw["session_usage_error"] = err.Error()
	}
	if sessionCount > 0 {
		hasData = true
		existing, ok := snap.Metrics["total_conversations"]
		if !ok || existing.Used == nil || *existing.Used < float64(sessionCount) {
			conversations := float64(sessionCount)
			snap.Metrics["total_conversations"] = core.Metric{
				Used:   &conversations,
				Unit:   "conversations",
				Window: defaultUsageWindowLabel,
			}
		}
	}

	binary := acct.Binary
	if binary == "" {
		binary = "gemini"
	}
	if binPath, err := exec.LookPath(binary); err == nil {
		snap.Raw["binary"] = binPath
		var vOut strings.Builder
		vCmd := exec.CommandContext(ctx, binary, "--version")
		vCmd.Stdout = &vOut
		if vCmd.Run() == nil {
			version := strings.TrimSpace(vOut.String())
			if version != "" {
				snap.Raw["cli_version"] = version
			}
		}
	}

	if acct.ExtraData != nil {
		if email := acct.ExtraData["email"]; email != "" && snap.Raw["account_email"] == "" {
			snap.Raw["account_email"] = email
		}
	}

	if creds.RefreshToken != "" {
		if err := p.fetchUsageFromAPI(ctx, &snap, creds, acct); err != nil {
			log.Printf("[gemini_cli] quota API error: %v", err)
			snap.Raw["quota_api_error"] = err.Error()
		}
	} else {
		snap.Raw["quota_api"] = "skipped (no refresh token)"
	}

	if !hasData {
		snap.Status = core.StatusError
		snap.Message = "No Gemini CLI data found"
		return snap, nil
	}

	if snap.Message == "" {
		if email := snap.Raw["account_email"]; email != "" {
			snap.Message = fmt.Sprintf("Gemini CLI (%s)", email)
		} else {
			snap.Message = "Gemini CLI local data"
		}
	}

	return snap, nil
}
