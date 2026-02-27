package appupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	defaultLatestReleaseURL = "https://api.github.com/repos/janekbaraniewski/openusage/releases/latest"
	defaultInstallScriptURL = "https://github.com/janekbaraniewski/openusage/releases/latest/download/install.sh"
	defaultRequestTimeout   = 1500 * time.Millisecond
)

type InstallMethod string

const (
	InstallMethodUnknown       InstallMethod = "unknown"
	InstallMethodHomebrew      InstallMethod = "homebrew"
	InstallMethodGoInstall     InstallMethod = "go_install"
	InstallMethodInstallScript InstallMethod = "install_script"
	InstallMethodScoop         InstallMethod = "scoop"
	InstallMethodChocolatey    InstallMethod = "chocolatey"
)

type CheckOptions struct {
	CurrentVersion   string
	ExecutablePath   string
	LatestReleaseURL string
	Timeout          time.Duration
	HTTPClient       *http.Client
}

type Result struct {
	UpdateAvailable bool
	CurrentVersion  string
	LatestVersion   string
	InstallMethod   InstallMethod
	UpgradeHint     string
	ExecutablePath  string
}

func Check(ctx context.Context, opts CheckOptions) (Result, error) {
	currentVersion := normalizeReleaseVersion(opts.CurrentVersion)
	executablePath := resolveExecutablePath(opts.ExecutablePath)
	method := detectInstallMethod(executablePath)

	result := Result{
		CurrentVersion: currentVersion,
		InstallMethod:  method,
		UpgradeHint:    upgradeHint(method),
		ExecutablePath: executablePath,
	}

	// Only check updates for stable semver releases.
	if currentVersion == "" {
		return result, nil
	}

	latestVersion, err := fetchLatestReleaseVersion(ctx, opts, currentVersion)
	if err != nil {
		return result, err
	}

	result.LatestVersion = latestVersion
	result.UpdateAvailable = semver.Compare(latestVersion, currentVersion) > 0
	return result, nil
}

func fetchLatestReleaseVersion(ctx context.Context, opts CheckOptions, currentVersion string) (string, error) {
	latestURL := strings.TrimSpace(opts.LatestReleaseURL)
	if latestURL == "" {
		latestURL = defaultLatestReleaseURL
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}

	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, latestURL, nil)
	if err != nil {
		return "", fmt.Errorf("build latest release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "openusage/"+currentVersion)
	if token := strings.TrimSpace(os.Getenv("OPENUSAGE_GITHUB_TOKEN")); token != "" && shouldAttachGitHubToken(latestURL) {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch latest release: HTTP %d", resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode latest release payload: %w", err)
	}

	latest := normalizeReleaseVersion(payload.TagName)
	if latest == "" {
		return "", fmt.Errorf("latest release tag is not a stable semver: %q", payload.TagName)
	}
	return latest, nil
}

func resolveExecutablePath(explicitPath string) string {
	if p := strings.TrimSpace(explicitPath); p != "" {
		return normalizePathForMatch(p)
	}
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil && strings.TrimSpace(resolved) != "" {
		exePath = resolved
	}
	return normalizePathForMatch(exePath)
}

func normalizePathForMatch(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
}

func detectInstallMethod(executablePath string) InstallMethod {
	path := normalizePathForMatch(executablePath)
	if path == "" {
		return InstallMethodUnknown
	}

	switch {
	case strings.Contains(path, "/cellar/openusage/"), strings.Contains(path, "/homebrew/cellar/openusage/"), path == "/opt/homebrew/bin/openusage":
		return InstallMethodHomebrew
	case strings.Contains(path, "/scoop/apps/openusage/"):
		return InstallMethodScoop
	case strings.Contains(path, "/chocolatey/lib/openusage/"), strings.Contains(path, "/chocolatey/bin/openusage"):
		return InstallMethodChocolatey
	case looksLikeGoInstallPath(path):
		return InstallMethodGoInstall
	case looksLikeInstallScriptPath(path):
		return InstallMethodInstallScript
	default:
		return InstallMethodUnknown
	}
}

func looksLikeGoInstallPath(path string) bool {
	if strings.HasSuffix(path, "/go/bin/openusage") || strings.HasSuffix(path, "/go/bin/openusage.exe") {
		return true
	}

	if gobin := normalizePathForMatch(os.Getenv("GOBIN")); gobin != "" {
		if path == gobin+"/openusage" || path == gobin+"/openusage.exe" {
			return true
		}
	}

	for _, gp := range filepath.SplitList(os.Getenv("GOPATH")) {
		gopath := normalizePathForMatch(gp)
		if gopath == "" {
			continue
		}
		if path == gopath+"/bin/openusage" || path == gopath+"/bin/openusage.exe" {
			return true
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		homePath := normalizePathForMatch(home)
		if homePath != "" {
			if path == homePath+"/go/bin/openusage" || path == homePath+"/go/bin/openusage.exe" {
				return true
			}
		}
	}

	return false
}

func looksLikeInstallScriptPath(path string) bool {
	if path == "/usr/local/bin/openusage" || path == "/usr/bin/openusage" {
		return true
	}

	if home, err := os.UserHomeDir(); err == nil {
		homePath := normalizePathForMatch(home)
		if homePath != "" {
			if path == homePath+"/.local/bin/openusage" || path == homePath+"/bin/openusage" || path == homePath+"/bin/openusage.exe" {
				return true
			}
		}
	}

	return false
}

func upgradeHint(method InstallMethod) string {
	switch method {
	case InstallMethodHomebrew:
		return "brew upgrade janekbaraniewski/tap/openusage"
	case InstallMethodGoInstall:
		return "go install github.com/janekbaraniewski/openusage/cmd/openusage@latest"
	case InstallMethodInstallScript:
		return "curl -fsSL " + defaultInstallScriptURL + " | bash"
	case InstallMethodScoop:
		return "scoop update openusage"
	case InstallMethodChocolatey:
		return "choco upgrade openusage -y"
	default:
		return "curl -fsSL " + defaultInstallScriptURL + " | bash"
	}
}

func normalizeReleaseVersion(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return ""
	}
	if semver.Prerelease(v) != "" || semver.Build(v) != "" {
		return ""
	}
	return semver.Canonical(v)
}

func shouldAttachGitHubToken(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), "api.github.com")
}
