// Package copilot implements a QuotaProvider for GitHub Copilot.
//
// GitHub Copilot does not expose a clean "remaining quota" API for individual
// users. However, the gh CLI provides several useful data points:
//
//   - `gh auth status` — confirms authentication and active account
//   - `gh api /user` — returns user info, plan type (free/pro/enterprise)
//   - `gh api /copilot_internal/v2/token` — returns Copilot token status
//   - `gh copilot --version` — confirms Copilot extension installed
//   - `gh api /user/copilot_billing/seats` — (org admins) shows seat usage
//
// For Copilot Business/Enterprise users, rate limits are per-seat and
// the exact remaining requests are not exposed. We report auth status,
// user plan, and any rate-limit indicators we find.
package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

// pricingSummary lists current GitHub Copilot subscription plans (as of 2025).
const pricingSummary = "Subscription plans (per seat/month) · " +
	"Copilot Free: $0 (2 000 code completions + 50 chat msgs/mo) · " +
	"Copilot Pro: $10/mo or $100/yr (unlimited completions, 300 premium requests/mo, multiple models) · " +
	"Copilot Pro+: $39/mo (unlimited completions, 1 500 premium requests/mo, all models) · " +
	"Copilot Business: $19/user/mo (org policies, audit logs, IP indemnity) · " +
	"Copilot Enterprise: $39/user/mo (knowledge bases, fine-tuned models, Bing search) · " +
	"Premium request costs vary by model: Claude 3.5 Sonnet = 1 req, Claude 3.7 Sonnet Thinking = 1 req, " +
	"GPT-4o = 1 req, o1 = 1 req, o3-mini = 1 req, Gemini 2.0 Flash = 1 req"

// Provider implements core.QuotaProvider for GitHub Copilot.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "copilot" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "GitHub Copilot",
		Capabilities: []string{"cli_status", "user_info", "rate_limit_check"},
		DocURL:       "https://docs.github.com/en/copilot/concepts/rate-limits",
	}
}

// ghUser is a subset of the /user API response.
type ghUser struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Plan  struct {
		Name string `json:"name"`
	} `json:"plan"`
}

// ghRateLimit is the response from /rate_limit.
type ghRateLimit struct {
	Resources struct {
		Core struct {
			Limit     int   `json:"limit"`
			Remaining int   `json:"remaining"`
			Reset     int64 `json:"reset"`
			Used      int   `json:"used"`
		} `json:"core"`
		Copilot struct {
			Limit     int   `json:"limit"`
			Remaining int   `json:"remaining"`
			Reset     int64 `json:"reset"`
			Used      int   `json:"used"`
		} `json:"copilot_chat"`
	} `json:"resources"`
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	binary := acct.Binary
	if binary == "" {
		binary = "gh"
	}

	if _, err := exec.LookPath(binary); err != nil {
		return core.QuotaSnapshot{
			ProviderID: p.ID(),
			AccountID:  acct.ID,
			Timestamp:  time.Now(),
			Status:     core.StatusError,
			Message:    fmt.Sprintf("%s binary not found in PATH", binary),
		}, nil
	}

	snap := core.QuotaSnapshot{
		ProviderID: p.ID(),
		AccountID:  acct.ID,
		Timestamp:  time.Now(),
		Metrics:    make(map[string]core.Metric),
		Resets:     make(map[string]time.Time),
		Raw:        make(map[string]string),
	}

	// 1. Check copilot extension
	if vOut, err := runGH(ctx, binary, "copilot", "--version"); err == nil {
		snap.Raw["copilot_version"] = strings.TrimSpace(vOut)
	} else {
		snap.Status = core.StatusError
		snap.Message = "gh copilot extension not available"
		return snap, nil
	}

	// 2. Check auth status
	authOut, authErr := runGH(ctx, binary, "auth", "status")
	authOutput := authOut
	if authErr != nil {
		authOutput = authOut // stderr is captured too
	}
	snap.Raw["auth_status"] = strings.TrimSpace(authOutput)

	if authErr != nil {
		snap.Status = core.StatusAuth
		snap.Message = "not authenticated with GitHub"
		return snap, nil
	}

	// 3. Get user info via gh api
	if userJSON, err := runGH(ctx, binary, "api", "/user"); err == nil {
		var user ghUser
		if json.Unmarshal([]byte(userJSON), &user) == nil {
			if user.Login != "" {
				snap.Raw["github_login"] = user.Login
			}
			if user.Name != "" {
				snap.Raw["github_name"] = user.Name
			}
			if user.Plan.Name != "" {
				snap.Raw["github_plan"] = user.Plan.Name
			}
		}
	}

	// 4. Check GitHub API rate limits (includes copilot_chat resource if available)
	if rlJSON, err := runGH(ctx, binary, "api", "/rate_limit"); err == nil {
		var rl ghRateLimit
		if json.Unmarshal([]byte(rlJSON), &rl) == nil {
			// GitHub API core rate limit
			if rl.Resources.Core.Limit > 0 {
				limit := float64(rl.Resources.Core.Limit)
				remaining := float64(rl.Resources.Core.Remaining)
				used := float64(rl.Resources.Core.Used)
				snap.Metrics["gh_api_rpm"] = core.Metric{
					Limit:     &limit,
					Remaining: &remaining,
					Used:      &used,
					Unit:      "requests",
					Window:    "1h",
				}
				if rl.Resources.Core.Reset > 0 {
					snap.Resets["gh_api_rpm_reset"] = time.Unix(rl.Resources.Core.Reset, 0)
				}
			}

			// Copilot Chat rate limit (if present)
			if rl.Resources.Copilot.Limit > 0 {
				limit := float64(rl.Resources.Copilot.Limit)
				remaining := float64(rl.Resources.Copilot.Remaining)
				used := float64(rl.Resources.Copilot.Used)
				snap.Metrics["copilot_chat"] = core.Metric{
					Limit:     &limit,
					Remaining: &remaining,
					Used:      &used,
					Unit:      "requests",
					Window:    "per-period",
				}
				if rl.Resources.Copilot.Reset > 0 {
					snap.Resets["copilot_chat_reset"] = time.Unix(rl.Resources.Copilot.Reset, 0)
				}
			}
		}
	}

	// 5. Try to get copilot billing for orgs (will fail for non-admins, that's OK)
	if orgBilling, err := runGH(ctx, binary, "api", "/user/copilot_billing/usage"); err == nil {
		snap.Raw["copilot_billing_response"] = strings.TrimSpace(orgBilling)
	}

	snap.Raw["pricing_summary"] = pricingSummary

	// Set final status
	lower := strings.ToLower(authOutput)
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "rate_limit") {
		snap.Status = core.StatusLimited
		snap.Message = "rate limited"
	} else if snap.Status == "" {
		snap.Status = core.StatusOK
		if login := snap.Raw["github_login"]; login != "" {
			snap.Message = fmt.Sprintf("Copilot (%s)", login)
		} else {
			snap.Message = "authenticated"
		}
	}

	return snap, nil
}

// runGH executes a gh command and returns stdout. Stderr is appended to stdout on error.
func runGH(ctx context.Context, binary string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stdout.String() + stderr.String(), err
	}
	return stdout.String(), nil
}
