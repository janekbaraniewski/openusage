package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

const (
	LaunchdDaemonLabel = "com.openusage.telemetryd"
	SystemdDaemonUnit  = "openusage-telemetry.service"
)

type ServiceManager struct {
	Kind       string
	exePath    string
	socketPath string
	stateDir   string
	unitPath   string
}

func (m ServiceManager) StdoutLogPath() string {
	if strings.TrimSpace(m.stateDir) == "" {
		return ""
	}
	return filepath.Join(m.stateDir, "daemon.stdout.log")
}

func (m ServiceManager) StderrLogPath() string {
	if strings.TrimSpace(m.stateDir) == "" {
		return ""
	}
	return filepath.Join(m.stateDir, "daemon.stderr.log")
}

func (m ServiceManager) StatusHint() string {
	switch m.Kind {
	case "darwin":
		return "launchctl print gui/$(id -u)/" + LaunchdDaemonLabel
	case "linux":
		return "systemctl --user status " + SystemdDaemonUnit
	default:
		return ""
	}
}

func NewServiceManager(socketPath string) (ServiceManager, error) {
	exePath, err := os.Executable()
	if err != nil {
		return ServiceManager{}, fmt.Errorf("resolve executable path: %w", err)
	}
	stateDir, err := telemetry.DefaultStateDir()
	if err != nil {
		return ServiceManager{}, err
	}

	manager := ServiceManager{
		Kind:       runtime.GOOS,
		exePath:    exePath,
		socketPath: strings.TrimSpace(socketPath),
		stateDir:   stateDir,
	}

	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return ServiceManager{}, fmt.Errorf("resolve home dir: %w", err)
		}
		manager.unitPath = filepath.Join(home, "Library", "LaunchAgents", LaunchdDaemonLabel+".plist")
	case "linux":
		home, err := os.UserHomeDir()
		if err != nil {
			return ServiceManager{}, fmt.Errorf("resolve home dir: %w", err)
		}
		manager.unitPath = filepath.Join(home, ".config", "systemd", "user", SystemdDaemonUnit)
	default:
		manager.Kind = "unsupported"
	}
	return manager, nil
}

func (m ServiceManager) IsSupported() bool {
	return m.Kind == "darwin" || m.Kind == "linux"
}

func (m ServiceManager) IsInstalled() bool {
	if strings.TrimSpace(m.unitPath) == "" {
		return false
	}
	_, err := os.Stat(m.unitPath)
	return err == nil
}

func (m ServiceManager) InstallHint() string {
	return "openusage telemetry daemon install"
}

func (m ServiceManager) domainCandidates() []string {
	uid := fmt.Sprintf("%d", os.Getuid())
	return []string{"gui/" + uid, "user/" + uid}
}

func RunCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		if trimmed != "" {
			return trimmed, fmt.Errorf("%s %s failed: %w (%s)", name, strings.Join(args, " "), err, trimmed)
		}
		return trimmed, fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return trimmed, nil
}

func InstallService(socketPath string) error {
	manager, err := NewServiceManager(socketPath)
	if err != nil {
		return err
	}
	if !manager.IsSupported() {
		return fmt.Errorf("daemon service install is unsupported on %s", runtime.GOOS)
	}
	return manager.Install()
}

func UninstallService(socketPath string) error {
	manager, err := NewServiceManager(socketPath)
	if err != nil {
		return err
	}
	if !manager.IsSupported() {
		return fmt.Errorf("daemon service uninstall is unsupported on %s", runtime.GOOS)
	}
	return manager.Uninstall()
}

func ServiceStatus(socketPath string, details bool) error {
	socketPath = strings.TrimSpace(socketPath)
	manager, err := NewServiceManager(socketPath)
	if err != nil {
		return err
	}

	supported := manager.IsSupported()
	installed := manager.IsInstalled()
	exePath := strings.TrimSpace(manager.exePath)
	exeExists := false
	if _, statErr := os.Stat(exePath); statErr == nil {
		exeExists = true
	}

	client := NewClient(socketPath)
	healthCtx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	health, healthErr := client.HealthInfo(healthCtx)

	fmt.Println("Telemetry Daemon Status")
	fmt.Printf("  Manager: %s (supported: %s)\n", manager.Kind, yesNo(supported))
	fmt.Printf("  Installed: %s\n", yesNo(installed))
	fmt.Printf("  Running: %s\n", yesNo(healthErr == nil))
	fmt.Printf("  Socket: %s\n", socketPath)
	fmt.Printf("  Unit file: %s\n", valueOrNA(strings.TrimSpace(manager.unitPath)))
	fmt.Printf("  Executable: %s\n", valueOrNA(exePath))
	fmt.Printf("  Executable exists: %s\n", yesNo(exeExists))
	fmt.Printf("  Executable transient: %s\n", yesNo(isTransientExecutablePath(manager.exePath)))

	if healthErr != nil {
		fmt.Printf("  Health error: %v\n", healthErr)
		if owner := SocketOwnerSummary(socketPath); strings.TrimSpace(owner) != "" {
			fmt.Printf("  Socket owner: %s\n", owner)
		}
		lastError := LastErrorLine(manager.StderrLogPath())
		if lastError != "" {
			fmt.Printf("  Last daemon error: %s\n", lastError)
		}
		fmt.Println("")
		fmt.Println("Next steps")
		if !installed {
			fmt.Println("  1. Install the service: ./bin/openusage telemetry daemon install")
		} else {
			fmt.Println("  1. Reinstall to refresh the unit and restart: ./bin/openusage telemetry daemon install")
		}
		fmt.Printf("  2. Check manager state: %s\n", manager.StatusHint())
		if stderrPath := strings.TrimSpace(manager.StderrLogPath()); stderrPath != "" {
			fmt.Printf("  3. Tail daemon logs: tail -n 80 %s\n", stderrPath)
		}
		if details {
			fmt.Println("")
			fmt.Println("Diagnostics")
			fmt.Println(StartupDiagnostics(manager, socketPath))
		} else {
			fmt.Println("")
			fmt.Println("Tip: rerun with --details for full diagnostics.")
		}
	} else {
		fmt.Printf("  Daemon version: %s\n", strings.TrimSpace(health.DaemonVersion))
		fmt.Printf("  API version: %s (compatible: %s)\n", strings.TrimSpace(health.APIVersion), yesNo(HealthAPICompatible(health)))
		fmt.Printf("  Integration version: %s\n", strings.TrimSpace(health.IntegrationVersion))
		fmt.Printf("  Provider registry: %s (compatible: %s)\n", strings.TrimSpace(health.ProviderRegistry), yesNo(HealthProviderRegistryCompatible(health)))
		fmt.Printf("  Overall compatibility: %s\n", yesNo(HealthCurrent(health)))
		if details {
			fmt.Println("")
			fmt.Println("Diagnostics")
			fmt.Println(StartupDiagnostics(manager, socketPath))
		}
	}
	return nil
}

func LastErrorLine(path string) string {
	tail := TailFile(strings.TrimSpace(path), 120)
	if strings.TrimSpace(tail) == "" {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(tail, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "Error: ") {
			return strings.TrimPrefix(line, "Error: ")
		}
	}
	return ""
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func valueOrNA(v string) string {
	if strings.TrimSpace(v) == "" {
		return "n/a"
	}
	return v
}

func SocketOwnerSummary(socketPath string) string {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return ""
	}
	if _, err := os.Stat(socketPath); err != nil {
		return ""
	}
	out, err := RunCommand("lsof", "-n", "-Fpcn", socketPath)
	if err == nil {
		if summary := parseLSOFFirstRecord(out); summary != "" {
			return summary
		}
	}

	out, err = RunCommand("lsof", "-n", socketPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(TailTextLines(out, 2))
}

func isTransientExecutablePath(path string) bool {
	p := strings.TrimSpace(path)
	if p == "" {
		return true
	}
	normalized := filepath.ToSlash(strings.ToLower(filepath.Clean(p)))
	if strings.Contains(normalized, "/go-build") && strings.Contains(normalized, "/exe/") {
		return true
	}
	tmpRoot := filepath.ToSlash(strings.ToLower(filepath.Clean(os.TempDir())))
	if tmpRoot == "" || tmpRoot == "." {
		return false
	}
	return strings.HasPrefix(normalized, tmpRoot+"/go-build")
}

func parseLSOFFirstRecord(out string) string {
	var (
		pid  string
		cmd  string
		name string
	)
	for _, rawLine := range strings.Split(out, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			if pid == "" {
				pid = strings.TrimSpace(line[1:])
			}
		case 'c':
			if cmd == "" {
				cmd = strings.TrimSpace(line[1:])
			}
		case 'n':
			if name == "" {
				name = strings.TrimSpace(line[1:])
			}
		}
		if pid != "" && cmd != "" && name != "" {
			break
		}
	}
	var parts []string
	if pid != "" {
		parts = append(parts, "pid="+pid)
	}
	if cmd != "" {
		parts = append(parts, "command="+cmd)
	}
	if name != "" {
		parts = append(parts, "socket="+name)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}
