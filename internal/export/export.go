package export

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/version"
)

// runner bundles the orchestration dependencies so tests can swap them out.
type runner struct {
	direct *directCollector
	dmn    *daemonCollector
	stderr io.Writer
	now    func() time.Time
	openOutput func(path string) (io.WriteCloser, error)
}

// Run is the entry point invoked from the cobra command. It resolves
// defaults, picks the right collection source, encodes the envelope, and
// writes it to the requested destination.
func Run(opts Options) error {
	if err := validateOptions(&opts); err != nil {
		return err
	}
	r := newRunner(opts)
	return r.run(context.Background(), opts)
}

func newRunner(opts Options) *runner {
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	now := func() time.Time {
		if !opts.Now.IsZero() {
			return opts.Now
		}
		return time.Now().UTC()
	}
	return &runner{
		direct: newDirectCollector(),
		dmn:    newDaemonCollector(daemon.ResolveSocketPath()),
		stderr: stderr,
		now:    now,
		openOutput: defaultOpenOutput,
	}
}

func validateOptions(opts *Options) error {
	if strings.TrimSpace(opts.Output) == "" {
		return errors.New("export: --output is required (use '-' for stdout)")
	}
	if opts.Format == "" {
		opts.Format = FormatJSON
	}
	switch opts.Format {
	case FormatJSON, FormatCSV:
	default:
		return fmt.Errorf("export: unsupported --format %q (use json or csv)", opts.Format)
	}
	if opts.Source == "" {
		opts.Source = SourceAuto
	}
	switch opts.Source {
	case SourceAuto, SourceDirect, SourceDaemon:
	default:
		return fmt.Errorf("export: unsupported --source %q (use auto, direct, or daemon)", opts.Source)
	}
	return nil
}

func (r *runner) run(ctx context.Context, opts Options) error {
	snaps, resolvedSource, err := r.collect(ctx, opts.Source)
	if err != nil {
		return err
	}

	env := ExportEnvelope{
		SchemaVersion:    SchemaVersion,
		GeneratedAt:      r.now(),
		OpenUsageVersion: resolveVersion(opts.Version),
		Source:           resolvedSource,
		Snapshots:        snaps,
	}
	if env.Snapshots == nil {
		env.Snapshots = []core.UsageSnapshot{}
	}

	writer, err := r.openOutput(opts.Output)
	if err != nil {
		return err
	}
	defer writer.Close()

	return encode(writer, env, opts.Format)
}

// collect picks the collection path based on opts.Source. SourceAuto tries
// the daemon first; on any daemon failure it logs to stderr and falls back to
// the direct collector. SourceDaemon never falls back; if the socket is not
// reachable the user gets an actionable error.
func (r *runner) collect(ctx context.Context, src Source) ([]core.UsageSnapshot, Source, error) {
	switch src {
	case SourceDirect:
		snaps, err := r.direct.collect(ctx)
		return snaps, SourceDirect, err

	case SourceDaemon:
		snaps, err := r.dmn.collect(ctx)
		return snaps, SourceDaemon, err

	case SourceAuto:
		if r.dmn.available(ctx) {
			snaps, err := r.dmn.collect(ctx)
			if err == nil {
				return snaps, SourceDaemon, nil
			}
			fmt.Fprintf(r.stderr,
				"export: telemetry daemon read failed (%v); falling back to direct mode\n", err)
		}
		snaps, err := r.direct.collect(ctx)
		return snaps, SourceDirect, err
	}

	return nil, src, fmt.Errorf("export: unsupported source %q", src)
}

func resolveVersion(override string) string {
	if v := strings.TrimSpace(override); v != "" {
		return v
	}
	return strings.TrimSpace(version.Version)
}

// defaultOpenOutput translates the user-facing output path into a writer.
// "-" means stdout (wrapped to satisfy io.WriteCloser without closing the
// underlying fd). Otherwise the parent directory is created with mkdir-p
// semantics and the file is opened with 0o600 to avoid leaking usage data to
// other users on the workstation.
func defaultOpenOutput(path string) (io.WriteCloser, error) {
	if path == "-" {
		return nopWriteCloser{w: os.Stdout}, nil
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("export: creating output directory %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("export: opening output %s: %w", path, err)
	}
	return f, nil
}

type nopWriteCloser struct{ w io.Writer }

func (n nopWriteCloser) Write(p []byte) (int, error) { return n.w.Write(p) }
func (n nopWriteCloser) Close() error                { return nil }
