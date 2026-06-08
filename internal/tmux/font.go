package tmux

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// iconFontTTF is the bundled provider-icon font, generated from the project's
// SVG icon set by scripts/gen-icon-font.py (source of truth: assets/icons.json).
// It is embedded so the binary can install it on demand and so `tmux font
// status` can report a checksum without a separate download.
//
//go:embed assets/openusage-icons.ttf
var iconFontTTF []byte

// iconFontFileName is the on-disk name used when the font is installed into the
// user's font directory.
const iconFontFileName = "openusage-icons.ttf"

// EmbeddedIconFont returns the raw bytes of the bundled icon font.
func EmbeddedIconFont() []byte { return iconFontTTF }

// FontInstallDir returns the per-user font directory for the current OS, where
// `tmux font install` writes the bundled font. Empty string means the platform
// is unsupported or the home directory could not be resolved.
func FontInstallDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Fonts")
	case "windows":
		return filepath.Join(home, "AppData", "Local", "Microsoft", "Windows", "Fonts")
	default: // linux and other unixes
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "fonts")
		}
		return filepath.Join(home, ".local", "share", "fonts")
	}
}

// FontInstallPath returns the full path the font is (or would be) installed to.
func FontInstallPath() string {
	dir := FontInstallDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, iconFontFileName)
}

// FontInstalled reports whether the bundled font is present in the user font
// directory. File presence is the cross-platform-reliable signal (fontconfig's
// fc-list is not available on a default macOS, for example).
func FontInstalled() bool {
	path := FontInstallPath()
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// EmbeddedFontSHA256 returns the hex sha256 of the font compiled into this
// binary — the "expected" version.
func EmbeddedFontSHA256() string {
	sum := sha256.Sum256(iconFontTTF)
	return hex.EncodeToString(sum[:])
}

// installedFontSHA256 returns the hex sha256 of the on-disk installed font, or
// "" if it is not installed / unreadable.
func installedFontSHA256() string {
	path := FontInstallPath()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// FontStatusInfo describes the install state of the bundled icon font, including
// whether the installed copy matches the version embedded in this binary.
type FontStatusInfo struct {
	Family       string
	Version      string
	Path         string
	Installed    bool
	UpToDate     bool // installed AND its bytes match the embedded font
	EmbeddedSHA  string
	InstalledSHA string
}

// FontStatus reports the current install state. UpToDate is determined by a
// content hash comparison (embedded vs installed), so any drift — new
// providers, refreshed glyphs, a regenerated font — is detected, not just a
// missing file.
func FontStatus() FontStatusInfo {
	st := FontStatusInfo{
		Family:      IconFontFamily(),
		Version:     IconFontVersion(),
		Path:        FontInstallPath(),
		Installed:   FontInstalled(),
		EmbeddedSHA: EmbeddedFontSHA256(),
	}
	if st.Installed {
		st.InstalledSHA = installedFontSHA256()
		st.UpToDate = st.InstalledSHA != "" && st.InstalledSHA == st.EmbeddedSHA
	}
	return st
}

// FontNeedsUpdate reports whether the font is installed but its bytes differ
// from the embedded version (i.e. an outdated install that should be refreshed
// with `tmux font install`).
func FontNeedsUpdate() bool {
	st := FontStatus()
	return st.Installed && !st.UpToDate
}

// InstallFont writes the embedded font into the user font directory and, when a
// fontconfig cache tool is available, refreshes the cache. It returns the path
// written. Refreshing the cache is best-effort: a failure there is not fatal
// because the font file itself is what terminals fall back to.
func InstallFont() (string, error) {
	if len(iconFontTTF) == 0 {
		return "", fmt.Errorf("tmux: embedded icon font is empty")
	}
	dir := FontInstallDir()
	if dir == "" {
		return "", fmt.Errorf("tmux: could not resolve a font directory for %s", runtime.GOOS)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("tmux: creating font dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, iconFontFileName)
	if err := os.WriteFile(path, iconFontTTF, 0o644); err != nil {
		return "", fmt.Errorf("tmux: writing font %s: %w", path, err)
	}
	refreshFontCache(dir)
	return path, nil
}

// UninstallFont removes the installed font file. A missing file is not an error.
func UninstallFont() (string, error) {
	path := FontInstallPath()
	if path == "" {
		return "", fmt.Errorf("tmux: could not resolve a font directory for %s", runtime.GOOS)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("tmux: removing font %s: %w", path, err)
	}
	refreshFontCache(filepath.Dir(path))
	return path, nil
}

// refreshFontCache runs `fc-cache -f <dir>` when available. No-op when the tool
// is absent (e.g. a default macOS without fontconfig installed).
func refreshFontCache(dir string) {
	bin, err := exec.LookPath("fc-cache")
	if err != nil {
		return
	}
	cmd := exec.Command(bin, "-f", dir)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run()
}
