package sketchybar

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DefaultDataDir is the neutral directory used for generated scripts.
func DefaultDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("sketchybar: resolving home directory: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("sketchybar: no home directory")
	}
	return filepath.Join(home, ".local", "share", "openusage", "sketchybar"), nil
}

// DetectConfig returns the preferred sketchybarrc path. An existing
// user-configured path wins over creation of a new default file.
func DetectConfig(warnOut io.Writer) (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("SKETCHYBAR_CONFIG")); explicit != "" {
		return expandPath(explicit)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("sketchybar: resolving home directory: %w", err)
	}
	var candidates []string
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "sketchybar", "sketchybarrc"))
	}
	candidates = append(candidates, filepath.Join(home, ".config", "sketchybar", "sketchybarrc"))
	existing := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			existing = append(existing, candidate)
		}
	}
	if len(existing) == 0 {
		return candidates[0], nil
	}
	if len(existing) > 1 && warnOut != nil {
		fmt.Fprintf(warnOut, "sketchybar: multiple configs found (%s); using %s\n", strings.Join(existing, ", "), existing[0])
	}
	return existing[0], nil
}

// Install prints or writes the managed SketchyBar block. When writing, only
// the config file and the neutral OpenUsage data directory are touched; the
// user's SketchyBar plugins directory is never inspected or modified.
func Install(out io.Writer, opts InstallOptions) (string, error) {
	opts, preset, err := withDefaults(opts)
	if err != nil {
		return "", err
	}
	snippet, err := BuildSnippet(opts)
	if err != nil {
		return "", err
	}
	if !opts.Write {
		if err := printSnippet(out, snippet); err != nil {
			return "", err
		}
		return "", nil
	}

	configPath := strings.TrimSpace(opts.ConfigPath)
	if configPath == "" {
		configPath, err = DetectConfig(out)
		if err != nil {
			return "", err
		}
	} else {
		configPath, err = expandPath(configPath)
		if err != nil {
			return "", err
		}
	}
	target, err := resolveWriteTarget(configPath)
	if err != nil {
		return "", err
	}
	existing, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("sketchybar: reading %s: %w", configPath, err)
	}
	if len(existing) > 0 {
		if err := os.WriteFile(target+".bak", existing, 0o600); err != nil {
			return "", fmt.Errorf("sketchybar: writing backup: %w", err)
		}
	}
	if err := writeAssets(opts.DataDir, opts, preset); err != nil {
		return "", err
	}
	updated := replaceBlock(existing, snippet)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("sketchybar: creating config directory: %w", err)
	}
	if err := os.WriteFile(target, updated, 0o644); err != nil {
		return "", fmt.Errorf("sketchybar: writing %s: %w", configPath, err)
	}

	fmt.Fprintf(out, "installed SketchyBar integration at %s\n", configPath)
	fmt.Fprintf(out, "  generated scripts: %s\n", opts.DataDir)
	fmt.Fprintln(out, "  reload with: sketchybar --reload")
	return configPath, nil
}

// Uninstall removes only the sentinel block. Generated scripts are left in
// the neutral data directory so a failed reload can be recovered without
// touching the user's dotfiles.
func Uninstall(out io.Writer, configPath string) error {
	path, err := configPathOrDefault(configPath, out)
	if err != nil {
		return err
	}
	target, err := resolveWriteTarget(path)
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(out, "no sketchybarrc found at %s; nothing to uninstall\n", path)
			return nil
		}
		return fmt.Errorf("sketchybar: reading %s: %w", path, err)
	}
	if !bytes.Contains(existing, []byte(SentinelStart)) {
		fmt.Fprintf(out, "no openusage block in %s; nothing to uninstall\n", path)
		return nil
	}
	if err := os.WriteFile(target+".bak", existing, 0o600); err != nil {
		return fmt.Errorf("sketchybar: writing backup: %w", err)
	}
	cleaned := removeBlock(existing)
	if err := os.WriteFile(target, cleaned, 0o644); err != nil {
		return fmt.Errorf("sketchybar: writing %s: %w", path, err)
	}
	fmt.Fprintf(out, "removed openusage block from %s\n", path)
	return nil
}

// SentinelPresent reports whether path contains a complete managed block.
func SentinelPresent(path string) (bool, error) {
	target, err := resolveReadTarget(path)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("sketchybar: reading %s: %w", path, err)
	}
	return bytes.Contains(data, []byte(SentinelStart)) && bytes.Contains(data, []byte(SentinelEnd)), nil
}

func configPathOrDefault(path string, out io.Writer) (string, error) {
	if strings.TrimSpace(path) != "" {
		return expandPath(path)
	}
	return DetectConfig(out)
}

func resolveWriteTarget(path string) (string, error) {
	path, err := expandPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return "", fmt.Errorf("sketchybar: resolving config symlink %s: %w", path, err)
			}
			return resolved, nil
		}
		return path, nil
	}
	if os.IsNotExist(err) {
		return path, nil
	}
	return "", fmt.Errorf("sketchybar: inspecting config path %s: %w", path, err)
}

func resolveReadTarget(path string) (string, error) {
	return resolveWriteTarget(path)
}

func expandPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("sketchybar: empty path")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("sketchybar: resolving home directory: %w", err)
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return filepath.Clean(path), nil
}

func replaceBlock(existing []byte, block string) []byte {
	start := bytes.Index(existing, []byte(SentinelStart))
	if start < 0 {
		var out bytes.Buffer
		if len(existing) > 0 {
			out.Write(existing)
			if !bytes.HasSuffix(existing, []byte("\n")) {
				out.WriteByte('\n')
			}
			out.WriteByte('\n')
		}
		out.WriteString(block)
		return out.Bytes()
	}
	end := bytes.Index(existing[start:], []byte(SentinelEnd))
	if end < 0 {
		return existing
	}
	end += start + len(SentinelEnd)
	if end < len(existing) && existing[end] == '\n' {
		end++
	}
	updated := append([]byte{}, existing[:start]...)
	updated = append(updated, []byte(block)...)
	return append(updated, existing[end:]...)
}

func removeBlock(existing []byte) []byte {
	start := bytes.Index(existing, []byte(SentinelStart))
	if start < 0 {
		return existing
	}
	end := bytes.Index(existing[start:], []byte(SentinelEnd))
	if end < 0 {
		return existing
	}
	end += start + len(SentinelEnd)
	if end < len(existing) && existing[end] == '\n' {
		end++
	}
	leading := start
	for leading > 0 && existing[leading-1] == '\n' {
		leading--
		if start-leading >= 2 {
			break
		}
	}
	cleaned := append([]byte{}, existing[:leading]...)
	return append(cleaned, existing[end:]...)
}
