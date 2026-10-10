package daemon

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"

	"github.com/janekbaraniewski/openusage/internal/version"
)

func ClassifyEnsureError(err error) DaemonState {
	if err == nil {
		return DaemonState{Status: DaemonStatusRunning}
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not installed"):
		return DaemonState{
			Status:      DaemonStatusNotInstalled,
			Message:     "Background helper is not set up.",
			InstallHint: "openusage telemetry daemon install",
		}
	case strings.Contains(msg, "out of date"):
		return DaemonState{
			Status:  DaemonStatusOutdated,
			Message: msg,
		}
	case strings.Contains(msg, "unsupported on"):
		return DaemonState{
			Status:  DaemonStatusError,
			Message: msg,
		}
	default:
		return DaemonState{
			Status:  DaemonStatusError,
			Message: msg,
		}
	}
}

// EnsureRunning returns a client for a healthy telemetry daemon, installing,
// starting, or upgrading the background service when needed.
//
// The returned warning is non-empty when the dashboard should keep going in a
// degraded state: the daemon on the socket is reachable and speaks a
// compatible API, but is not the build this binary expects and could not (or
// should not) be replaced automatically. Callers must treat a non-nil client
// as usable even when warning is set.
func EnsureRunning(ctx context.Context, socketPath string, verbose bool) (*Client, string, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil, "", fmt.Errorf("daemon socket path is empty")
	}
	client := NewClient(socketPath)

	health, healthErr := WaitForHealthInfo(ctx, client, 1200*time.Millisecond)
	if healthErr == nil && HealthCurrent(health) {
		return client, "", nil
	}

	if healthErr != nil {
		c, err := ensureViaServiceManager(ctx, client, socketPath, verbose, false, health)
		return c, "", err
	}

	// A daemon is answering but it is not the build we expect.
	if allowed, reason := autoUpgradeAllowed(health); !allowed {
		if HealthAPICompatible(health) {
			return client, reason, nil
		}
		return nil, "", fmt.Errorf("%s", reason)
	}

	c, err := ensureViaServiceManager(ctx, client, socketPath, verbose, true, health)
	if err == nil {
		return c, "", nil
	}
	// The upgrade failed. If a compatible daemon still answers, keep the
	// dashboard usable instead of parking it on the startup screen.
	if fallback, warning, ok := degradeToRunningDaemon(ctx, client, err); ok {
		return fallback, warning, nil
	}
	return nil, "", err
}

// autoUpgrade bookkeeping: a process performs at most one automatic service
// upgrade. If the daemon is incompatible again afterwards, something else
// (typically an older openusage dashboard still running in another terminal)
// reinstalled its own build, and fighting it only produces a restart loop and
// launchctl bootstrap races.
var (
	autoUpgradeMu   sync.Mutex
	autoUpgradeDone bool
)

func markAutoUpgradeAttempted() bool {
	autoUpgradeMu.Lock()
	defer autoUpgradeMu.Unlock()
	if autoUpgradeDone {
		return false
	}
	autoUpgradeDone = true
	return true
}

func resetAutoUpgradeForTest() {
	autoUpgradeMu.Lock()
	autoUpgradeDone = false
	autoUpgradeMu.Unlock()
}

// autoUpgradeAllowed decides whether this binary may replace the running
// daemon service without an explicit user action. It never downgrades: a
// daemon reporting a newer version than this binary is left alone.
func autoUpgradeAllowed(health HealthResponse) (bool, string) {
	ours := strings.TrimSpace(version.Version)
	running := HealthVersion(health)
	hint := fmt.Sprintf(
		"Background helper is %s, this openusage is %s; run `openusage telemetry daemon install` to switch",
		running, displayVersion(ours),
	)
	apiCompatible := HealthAPICompatible(health)

	if apiCompatible {
		cmp, comparable := compareBuildVersions(ours, running)
		switch {
		case comparable && cmp < 0:
			return false, hint + " (not downgrading automatically)"
		case !comparable && parseBuildVersion(ours) == nil && IsReleaseSemver(normalizeVersion(running)):
			// Unversioned local build against a released daemon.
			return false, hint
		}
	}

	if !markAutoUpgradeAttempted() {
		if !apiCompatible {
			return false, "telemetry daemon is out of date (API incompatible) and another openusage process keeps reinstalling it; " + hint
		}
		return false, hint + " (another openusage process keeps reinstalling it)"
	}
	return true, ""
}

func degradeToRunningDaemon(ctx context.Context, client *Client, upgradeErr error) (*Client, string, bool) {
	// The ensure context may already be spent by the failed install; probe
	// with a short budget of our own.
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	health, err := WaitForHealthInfo(probeCtx, client, 3*time.Second)
	if err != nil || !HealthAPICompatible(health) {
		return nil, "", false
	}
	first := upgradeErr.Error()
	if idx := strings.IndexByte(first, '\n'); idx >= 0 {
		first = first[:idx]
	}
	return client, fmt.Sprintf(
		"Background helper update failed, using running %s: %s",
		HealthVersion(health), first,
	), true
}

func displayVersion(v string) string {
	if strings.TrimSpace(v) == "" {
		return "dev"
	}
	return v
}

type buildVersion struct {
	base    string // canonical semver, e.g. v0.25.1
	commits int    // commits past base from `git describe` (0 for releases)
}

var gitDescribeSuffix = regexp.MustCompile(`^(v[0-9]+\.[0-9]+\.[0-9]+)-([0-9]+)-g[0-9a-f]+(-dirty)?$`)

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v != "" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

// parseBuildVersion understands release versions (0.25.1, v0.25.1) and
// `git describe` versions produced by `make build` (v0.25.1-6-g611051b[-dirty]).
// It returns nil for anything else ("dev", "unknown", empty).
func parseBuildVersion(raw string) *buildVersion {
	v := normalizeVersion(raw)
	if m := gitDescribeSuffix.FindStringSubmatch(v); m != nil {
		n, err := strconv.Atoi(m[2])
		if err != nil || !semver.IsValid(m[1]) {
			return nil
		}
		return &buildVersion{base: semver.Canonical(m[1]), commits: n}
	}
	v = strings.TrimSuffix(v, "-dirty")
	if !semver.IsValid(v) {
		return nil
	}
	return &buildVersion{base: v}
}

// compareBuildVersions compares two build versions. ok is false when either
// side is not a recognisable version.
func compareBuildVersions(a, b string) (int, bool) {
	va, vb := parseBuildVersion(a), parseBuildVersion(b)
	if va == nil || vb == nil {
		return 0, false
	}
	if c := semver.Compare(va.base, vb.base); c != 0 {
		return c, true
	}
	switch {
	case va.commits < vb.commits:
		return -1, true
	case va.commits > vb.commits:
		return 1, true
	}
	return 0, true
}

func ensureViaServiceManager(
	ctx context.Context,
	client *Client,
	socketPath string,
	verbose bool,
	needsUpgrade bool,
	health HealthResponse,
) (*Client, error) {
	manager, err := NewServiceManager(socketPath)
	if err != nil {
		return nil, err
	}

	if needsUpgrade && !manager.IsSupported() {
		return nil, fmt.Errorf(
			"telemetry daemon is out of date (running=%s expected=%s) and auto-upgrade is unsupported on %s",
			HealthVersion(health), strings.TrimSpace(version.Version), runtime.GOOS,
		)
	}

	if manager.IsSupported() {
		return startViaManagedService(ctx, client, manager, needsUpgrade, socketPath)
	}

	if err := spawnDaemonProcess(socketPath, verbose); err != nil {
		return nil, fmt.Errorf("start telemetry daemon: %w", err)
	}
	if err := waitAndVerifyDaemon(ctx, client, socketPath); err != nil {
		return nil, err
	}
	return client, nil
}

func startViaManagedService(
	ctx context.Context,
	client *Client,
	manager ServiceManager,
	needsUpgrade bool,
	socketPath string,
) (*Client, error) {
	if needsUpgrade {
		if err := manager.Install(); err != nil {
			return nil, fmt.Errorf("upgrade telemetry daemon service: %w", err)
		}
	}
	if !manager.IsInstalled() {
		return nil, fmt.Errorf("telemetry daemon service is not installed; run `%s`", manager.InstallHint())
	}
	if err := manager.Start(); err != nil {
		// If start returned an ambiguous manager-level error, still check whether
		// a daemon reached health on the socket before failing hard.
		if waitErr := waitAndVerifyDaemon(ctx, client, socketPath); waitErr == nil {
			return client, nil
		}
		return nil, fmt.Errorf("start telemetry daemon service: %w\n%s", err, StartupDiagnostics(manager, socketPath))
	}
	if err := waitAndVerifyDaemon(ctx, client, socketPath); err != nil {
		return nil, fmt.Errorf("%w\n%s", err, StartupDiagnostics(manager, socketPath))
	}
	return client, nil
}

func waitAndVerifyDaemon(ctx context.Context, client *Client, socketPath string) error {
	if err := WaitForHealth(ctx, client, 25*time.Second); err != nil {
		return err
	}
	health, err := WaitForHealthInfo(ctx, client, 1500*time.Millisecond)
	if err != nil {
		return nil
	}
	if !HealthCurrent(health) {
		return fmt.Errorf(
			"telemetry daemon is out of date (running=%s expected=%s)",
			HealthVersion(health), strings.TrimSpace(version.Version),
		)
	}
	return nil
}

func HealthVersion(health HealthResponse) string {
	if v := strings.TrimSpace(health.DaemonVersion); v != "" {
		return v
	}
	return "unknown"
}

func HealthCurrent(health HealthResponse) bool {
	expected := strings.TrimSpace(version.Version)
	if expected == "" || strings.EqualFold(expected, "dev") || !IsReleaseSemver(expected) {
		return HealthAPICompatible(health) && HealthProviderRegistryCompatible(health)
	}
	return strings.TrimSpace(health.DaemonVersion) == expected &&
		HealthAPICompatible(health) &&
		HealthProviderRegistryCompatible(health)
}

func HealthAPICompatible(health HealthResponse) bool {
	apiVersion := strings.TrimSpace(health.APIVersion)
	return apiVersion == "" || apiVersion == APIVersion
}

func HealthProviderRegistryCompatible(health HealthResponse) bool {
	expected := ProviderRegistryHash()
	if expected == "" {
		return true
	}
	got := strings.TrimSpace(health.ProviderRegistry)
	if got == "" {
		current := strings.TrimSpace(version.Version)
		// Backward-compatible for local/dev snapshots so `go run` workflows don't
		// force service reinstalls against transient executable paths.
		if current == "" || strings.EqualFold(current, "dev") || !IsReleaseSemver(current) {
			return true
		}
		return false
	}
	return strings.EqualFold(got, expected)
}

func IsReleaseSemver(value string) bool {
	v := strings.TrimSpace(value)
	if !semver.IsValid(v) {
		return false
	}
	if semver.Prerelease(v) != "" || semver.Build(v) != "" {
		return false
	}
	return v == semver.Canonical(v)
}

func WaitForHealth(ctx context.Context, client *Client, timeout time.Duration) error {
	_, err := WaitForHealthInfo(ctx, client, timeout)
	return err
}

func WaitForHealthInfo(
	ctx context.Context,
	client *Client,
	timeout time.Duration,
) (HealthResponse, error) {
	if client == nil {
		return HealthResponse{}, fmt.Errorf("daemon client is nil")
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if pingCtx.Err() != nil {
			break
		}
		hc, hcCancel := context.WithTimeout(pingCtx, 700*time.Millisecond)
		health, err := client.HealthInfo(hc)
		hcCancel()
		if err == nil {
			return health, nil
		}
		lastErr = err
		time.Sleep(220 * time.Millisecond)
	}
	if pingCtx.Err() != nil && pingCtx.Err() != context.Canceled {
		return HealthResponse{}, pingCtx.Err()
	}
	if lastErr != nil {
		return HealthResponse{}, fmt.Errorf("telemetry daemon did not become ready at %s: %w", client.SocketPath, lastErr)
	}
	return HealthResponse{}, fmt.Errorf("telemetry daemon did not become ready at %s", client.SocketPath)
}

func StartupDiagnostics(manager ServiceManager, socketPath string) string {
	lines := []string{
		fmt.Sprintf("manager_kind=%s", strings.TrimSpace(manager.Kind)),
		fmt.Sprintf("service_supported=%t", manager.IsSupported()),
		fmt.Sprintf("service_installed=%t", manager.IsInstalled()),
		fmt.Sprintf("socket_path=%s", strings.TrimSpace(socketPath)),
		fmt.Sprintf("unit_path=%s", strings.TrimSpace(manager.unitPath)),
		fmt.Sprintf("service_executable=%s", strings.TrimSpace(manager.exePath)),
		fmt.Sprintf("service_executable_transient=%t", isTransientExecutablePath(manager.exePath)),
	}
	if _, err := os.Stat(strings.TrimSpace(manager.exePath)); err == nil {
		lines = append(lines, "service_executable_exists=true")
	} else {
		lines = append(lines, fmt.Sprintf("service_executable_exists=false err=%v", err))
	}
	if hint := strings.TrimSpace(manager.StatusHint()); hint != "" {
		lines = append(lines, "status_cmd="+hint)
	}
	if owner := SocketOwnerSummary(socketPath); strings.TrimSpace(owner) != "" {
		lines = append(lines, "socket_owner:\n"+owner)
	}
	if stderrPath := strings.TrimSpace(manager.StderrLogPath()); stderrPath != "" {
		lines = append(lines, "stderr_log="+stderrPath)
		if tail := TailFile(stderrPath, 30); strings.TrimSpace(tail) != "" {
			lines = append(lines, "stderr_tail:\n"+tail)
		}
	}
	if stdoutPath := strings.TrimSpace(manager.StdoutLogPath()); stdoutPath != "" {
		lines = append(lines, "stdout_log="+stdoutPath)
		if tail := TailFile(stdoutPath, 30); strings.TrimSpace(tail) != "" {
			lines = append(lines, "stdout_tail:\n"+tail)
		}
	}
	if manager.Kind == "darwin" {
		var launchctlErr error
		for _, domain := range manager.domainCandidates() {
			target := domain + "/" + LaunchdDaemonLabel
			out, err := RunCommand("launchctl", "print", target)
			if err != nil {
				launchctlErr = err
				continue
			}
			if tail := TailTextLines(out, 30); strings.TrimSpace(tail) != "" {
				lines = append(lines, "launchctl_print_tail("+target+"):\n"+tail)
			}
			launchctlErr = nil
			break
		}
		if launchctlErr != nil {
			lines = append(lines, fmt.Sprintf("launchctl_print_error=%v", launchctlErr))
		}
	}
	if manager.Kind == "linux" {
		if out, err := RunCommand("systemctl", "--user", "status", SystemdDaemonUnit, "--no-pager", "-n", "30"); err == nil {
			if tail := TailTextLines(out, 30); strings.TrimSpace(tail) != "" {
				lines = append(lines, "systemctl_status_tail:\n"+tail)
			}
		}
		if out, err := RunCommand("journalctl", "--user-unit", SystemdDaemonUnit, "-n", "30", "--no-pager"); err == nil {
			if tail := TailTextLines(out, 30); strings.TrimSpace(tail) != "" {
				lines = append(lines, "journalctl_tail:\n"+tail)
			}
		}
	}
	if manager.Kind == "windows" {
		if out, err := RunCommand("schtasks", "/Query", "/TN", WindowsScheduledTask, "/V", "/FO", "LIST"); err == nil {
			if tail := TailTextLines(out, 40); strings.TrimSpace(tail) != "" {
				lines = append(lines, "schtasks_query_tail:\n"+tail)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func TailFile(path string, maxLines int) string {
	raw, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return ""
	}
	return TailTextLines(string(raw), maxLines)
}

func TailTextLines(text string, maxLines int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return ""
	}
	if maxLines <= 0 {
		maxLines = 20
	}
	parts := strings.Split(text, "\n")
	if len(parts) <= maxLines {
		return strings.Join(parts, "\n")
	}
	return strings.Join(parts[len(parts)-maxLines:], "\n")
}

func spawnDaemonProcess(socketPath string, verbose bool) error {
	_ = verbose
	_ = socketPath
	return fmt.Errorf("daemon process auto-spawn is unsupported on %s without a managed service", runtime.GOOS)
}
