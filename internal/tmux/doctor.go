package tmux

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/daemon"
)

// DoctorOptions configures the Run helper. All fields are optional; the
// defaults call the live tmux binary, the live daemon socket, and the
// production active-tool detector.
type DoctorOptions struct {
	// TmuxBinary overrides exec.LookPath("tmux"). Tests use this to point
	// at a stub binary.
	TmuxBinary string
	// SocketPath overrides the daemon socket resolution. Empty uses
	// daemon.ResolveSocketPath().
	SocketPath string
	// ConfPath overrides DetectTmuxConf. Tests use this to point at a
	// fixture.
	ConfPath string
	// Now is injected by tests; zero means time.Now().
	Now time.Time
}

// Run prints a one-section-per-check diagnostic summary to out. Each check
// is independent and continues on failure so users get a complete report
// even when some subsystems are broken. Returns the first non-recoverable
// error (currently never set; reserved for future use).
func Run(out io.Writer, opts DoctorOptions) error {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	fmt.Fprintln(out, "openusage tmux doctor")
	fmt.Fprintln(out, strings.Repeat("-", 32))

	checkTmuxBinary(out, opts)
	checkTmuxEnv(out)
	checkTerminalTruecolor(out)
	checkDaemon(out, opts)
	checkActiveProvider(out, opts)
	checkSnippet(out, opts)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "done.")
	return nil
}

// checkTmuxBinary reports the installed tmux version and warns when it is
// below the recommended floors. tmux 3.0 is the minimum we test against;
// display-popup needs 3.2.
func checkTmuxBinary(out io.Writer, opts DoctorOptions) {
	bin := strings.TrimSpace(opts.TmuxBinary)
	if bin == "" {
		path, err := exec.LookPath("tmux")
		if err != nil {
			fmt.Fprintln(out, "[FAIL] tmux: not found in PATH (install tmux to use the status bar)")
			return
		}
		bin = path
	}
	out2, err := exec.Command(bin, "-V").CombinedOutput()
	if err != nil {
		fmt.Fprintf(out, "[FAIL] tmux: invoking %s -V failed: %v\n", bin, err)
		return
	}
	verStr := strings.TrimSpace(string(out2))
	major, minor, ok := parseTmuxVersion(verStr)
	switch {
	case !ok:
		fmt.Fprintf(out, "[WARN] tmux: %s (could not parse version)\n", verStr)
	case major < 3:
		fmt.Fprintf(out, "[WARN] tmux: %s (3.0+ recommended)\n", verStr)
	case major == 3 && minor < 2:
		fmt.Fprintf(out, "[ OK ] tmux: %s (3.2+ needed for --bind-popup)\n", verStr)
	default:
		fmt.Fprintf(out, "[ OK ] tmux: %s\n", verStr)
	}
}

// parseTmuxVersion extracts the leading major.minor numbers from `tmux -V`
// output like "tmux 3.4" or "tmux next-3.5". Returns ok=false if the prefix
// does not match.
var tmuxVersionRE = regexp.MustCompile(`(\d+)\.(\d+)`)

func parseTmuxVersion(s string) (int, int, bool) {
	m := tmuxVersionRE.FindStringSubmatch(s)
	if len(m) != 3 {
		return 0, 0, false
	}
	major, err1 := strconv.Atoi(m[1])
	minor, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func checkTmuxEnv(out io.Writer) {
	if v := strings.TrimSpace(os.Getenv("TMUX")); v != "" {
		fmt.Fprintf(out, "[ OK ] $TMUX: set (running inside tmux: %s)\n", v)
		return
	}
	fmt.Fprintln(out, "[INFO] $TMUX: unset (not running inside a tmux session)")
}

func checkTerminalTruecolor(out io.Writer) {
	cterm := strings.ToLower(strings.TrimSpace(os.Getenv("COLORTERM")))
	term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	switch {
	case strings.Contains(cterm, "truecolor") || strings.Contains(cterm, "24bit"):
		fmt.Fprintf(out, "[ OK ] terminal: truecolor advertised (COLORTERM=%s)\n", cterm)
	case strings.Contains(term, "256"):
		fmt.Fprintf(out, "[INFO] terminal: 256-color (TERM=%s); use --color-mode 256 if colors look off\n", term)
	default:
		fmt.Fprintf(out, "[WARN] terminal: COLORTERM unset (TERM=%s); pass --color-mode 256 or ansi\n", term)
	}
}

func checkDaemon(out io.Writer, opts DoctorOptions) {
	socket := strings.TrimSpace(opts.SocketPath)
	if socket == "" {
		socket = daemon.ResolveSocketPath()
	}
	if socket == "" {
		fmt.Fprintln(out, "[INFO] daemon: no socket path resolved (will fall back to direct mode)")
		return
	}
	if _, err := os.Stat(socket); err != nil {
		fmt.Fprintf(out, "[INFO] daemon: not running at %s (tmux render falls back to direct mode)\n", socket)
		return
	}
	cli := daemon.NewClient(socket)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := cli.HealthInfo(ctx)
	if err != nil {
		fmt.Fprintf(out, "[WARN] daemon: socket present but health check failed: %v\n", err)
		return
	}
	status := strings.TrimSpace(resp.Status)
	if status == "" {
		status = "ok"
	}
	fmt.Fprintf(out, "[ OK ] daemon: %s (%s)\n", status, socket)
}

func checkActiveProvider(out io.Writer, opts DoctorOptions) {
	res := Detect(DetectOptions{Now: opts.Now, NoCache: true})
	if res.Primary == "" {
		fmt.Fprintln(out, "[WARN] active provider: none detected (no recent local activity, no configured priority match)")
		return
	}
	suffix := ""
	if len(res.Ordered) > 1 {
		suffix = fmt.Sprintf(" (also seen: %s)", strings.Join(res.Ordered[1:], ", "))
	}
	fmt.Fprintf(out, "[ OK ] active provider: %s via %s%s\n", res.Primary, res.Source, suffix)
}

func checkSnippet(out io.Writer, opts DoctorOptions) {
	path := strings.TrimSpace(opts.ConfPath)
	if path == "" {
		detected, err := DetectTmuxConf(nil)
		if err != nil {
			fmt.Fprintf(out, "[WARN] tmux.conf: %v\n", err)
			return
		}
		path = detected
	}
	present, err := SentinelPresent(path)
	if err != nil {
		fmt.Fprintf(out, "[WARN] tmux.conf: %v\n", err)
		return
	}
	if !present {
		fmt.Fprintf(out, "[INFO] tmux.conf: %s has no openusage block (run `openusage tmux install --write` to add it)\n", path)
		return
	}
	fmt.Fprintf(out, "[ OK ] tmux.conf: openusage block present at %s\n", path)
}
