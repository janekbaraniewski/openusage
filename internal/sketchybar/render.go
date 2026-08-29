package sketchybar

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

//go:embed assets/*.tpl
var assetsFS embed.FS

var assetNames = []string{
	"ai-usage.sh",
	"usage-popup.sh",
	"provider-select.sh",
}

// AssetNames returns the generated script names installed by OpenUsage.
func AssetNames() []string {
	out := make([]string, len(assetNames))
	copy(out, assetNames)
	return out
}

// BuildSnippet renders the complete managed shell block. It does not touch
// the filesystem, which makes print mode and installer tests deterministic.
func BuildSnippet(opts InstallOptions) (string, error) {
	opts, preset, err := withDefaults(opts)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(SentinelStart)
	b.WriteByte('\n')
	fmt.Fprintf(&b, "# Generated scripts live outside ~/.config/sketchybar/plugins.\n")
	fmt.Fprintf(&b, "OPENUSAGE_SKETCHYBAR_DIR=%s\n", shellQuote(opts.DataDir))
	fmt.Fprintf(&b, "OPENUSAGE_BIN=%s\n", shellQuote(opts.Binary))
	fmt.Fprintf(&b, "OPENUSAGE_SKETCHYBAR_CACHE_DIR=\"${OPENUSAGE_SKETCHYBAR_CACHE_DIR:-$HOME/.cache/openusage/sketchybar}\"\n")
	fmt.Fprintf(&b, "OPENUSAGE_SKETCHYBAR_ICON=%s\n", shellQuote(preset.Icon))
	fmt.Fprintf(&b, "OPENUSAGE_SKETCHYBAR_GOOD_COLOR=%s\n", shellQuote(preset.Colors.Good))
	fmt.Fprintf(&b, "OPENUSAGE_SKETCHYBAR_WARN_COLOR=%s\n", shellQuote(preset.Colors.Warn))
	fmt.Fprintf(&b, "OPENUSAGE_SKETCHYBAR_BAD_COLOR=%s\n", shellQuote(preset.Colors.Bad))
	fmt.Fprintf(&b, "OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR=%s\n", shellQuote(preset.Colors.Unknown))
	fmt.Fprintf(&b, "OPENUSAGE_SKETCHYBAR_TEXT_COLOR=%s\n", shellQuote(preset.Colors.Text))
	fmt.Fprintf(&b, "OPENUSAGE_SKETCHYBAR_USAGE_TRIGGER=%s\n", shellQuote(opts.UsageTrigger))
	fmt.Fprintf(&b, "OPENUSAGE_SKETCHYBAR_SWITCHER_TRIGGER=%s\n", shellQuote(opts.SwitcherTrigger))
	b.WriteString("export OPENUSAGE_SKETCHYBAR_DIR OPENUSAGE_BIN OPENUSAGE_SKETCHYBAR_CACHE_DIR OPENUSAGE_SKETCHYBAR_USAGE_TRIGGER OPENUSAGE_SKETCHYBAR_SWITCHER_TRIGGER\n")
	b.WriteString("export OPENUSAGE_SKETCHYBAR_ICON OPENUSAGE_SKETCHYBAR_GOOD_COLOR OPENUSAGE_SKETCHYBAR_WARN_COLOR OPENUSAGE_SKETCHYBAR_BAD_COLOR OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR OPENUSAGE_SKETCHYBAR_TEXT_COLOR\n")

	item := shellQuote(preset.Item)
	switcher := shellQuote(preset.Switcher)
	position := shellQuote(preset.Position)
	fmt.Fprintf(&b, "sketchybar --add item %s %s >/dev/null 2>&1 || true\n", item, position)
	fmt.Fprintf(&b, "sketchybar --subscribe %s %s\n", item, usageTriggerEvents(opts.UsageTrigger))
	fmt.Fprintf(&b, "sketchybar --set %s update_freq=%d padding_left=10 popup.drawing=off popup.background.color=%s popup.background.border_color=%s popup.background.border_width=2 popup.background.corner_radius=6 popup.align=right popup.y_offset=2 script=\"$OPENUSAGE_SKETCHYBAR_DIR/ai-usage.sh\"\n",
		item, preset.UpdateFreq, shellQuote(preset.Colors.Bar), shellQuote(preset.Colors.Surface))

	fmt.Fprintf(&b, "sketchybar --add item %s %s >/dev/null 2>&1 || true\n", switcher, position)
	fmt.Fprintf(&b, "sketchybar --subscribe %s %s\n", switcher, switcherTriggerEvents(opts.SwitcherTrigger))
	fmt.Fprintf(&b, "sketchybar --set %s padding_left=2 padding_right=2 icon=%s icon.font=\"Hack Nerd Font:Regular:13.0\" icon.color=%s label.drawing=off popup.drawing=off popup.background.color=%s popup.background.border_color=%s popup.background.border_width=2 popup.background.corner_radius=6 popup.horizontal=on popup.align=right popup.y_offset=2 script=\"$OPENUSAGE_SKETCHYBAR_DIR/provider-select.sh\"\n",
		switcher, shellQuote(preset.SwitcherIcon), shellQuote(preset.Colors.Text), shellQuote(preset.Colors.Bar), shellQuote(preset.Colors.Surface))
	b.WriteString("sketchybar --update\n")
	b.WriteString(SentinelEnd)
	b.WriteByte('\n')
	return b.String(), nil
}

func withDefaults(opts InstallOptions) (InstallOptions, Preset, error) {
	if strings.TrimSpace(opts.Preset) == "" {
		opts.Preset = DefaultPreset
	}
	preset, err := SamplePreset(opts.Preset)
	if err != nil {
		return InstallOptions{}, Preset{}, err
	}
	if strings.TrimSpace(opts.Binary) == "" {
		opts.Binary = "openusage"
	} else if strings.HasPrefix(strings.TrimSpace(opts.Binary), "~/") || strings.TrimSpace(opts.Binary) == "~" {
		opts.Binary, err = expandPath(opts.Binary)
		if err != nil {
			return InstallOptions{}, Preset{}, err
		}
	}
	if strings.TrimSpace(opts.DataDir) == "" {
		opts.DataDir, err = DefaultDataDir()
		if err != nil {
			return InstallOptions{}, Preset{}, err
		}
	} else {
		opts.DataDir, err = expandPath(opts.DataDir)
		if err != nil {
			return InstallOptions{}, Preset{}, err
		}
	}
	opts.UsageTrigger = normalizeTrigger(opts.UsageTrigger)
	opts.SwitcherTrigger = normalizeTrigger(opts.SwitcherTrigger)
	if preset.UpdateFreq <= 0 {
		preset.UpdateFreq = 60
	}
	if !validSketchybarName(preset.Item) || !validSketchybarName(preset.Switcher) {
		return InstallOptions{}, Preset{}, fmt.Errorf("sketchybar: invalid item name in preset %q", preset.Name)
	}
	if preset.Position != "left" && preset.Position != "right" && preset.Position != "center" {
		return InstallOptions{}, Preset{}, fmt.Errorf("sketchybar: invalid position %q in preset %q", preset.Position, preset.Name)
	}
	return opts, preset, nil
}

func writeAssets(dir string, opts InstallOptions, preset Preset) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("sketchybar: creating asset directory: %w", err)
	}
	for _, name := range assetNames {
		data, err := assetsFS.ReadFile("assets/" + name + ".tpl")
		if err != nil {
			return fmt.Errorf("sketchybar: reading embedded asset %s: %w", name, err)
		}
		data = injectAssetEnvironment(data, opts, preset)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o755); err != nil {
			return fmt.Errorf("sketchybar: writing asset %s: %w", path, err)
		}
	}
	return nil
}

func injectAssetEnvironment(data []byte, opts InstallOptions, preset Preset) []byte {
	newline := bytes.IndexByte(data, '\n')
	if newline < 0 {
		return data
	}
	header := fmt.Sprintf("OPENUSAGE_BIN=%s\nOPENUSAGE_SKETCHYBAR_ICON=%s\nOPENUSAGE_SKETCHYBAR_GOOD_COLOR=%s\nOPENUSAGE_SKETCHYBAR_WARN_COLOR=%s\nOPENUSAGE_SKETCHYBAR_BAD_COLOR=%s\nOPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR=%s\nOPENUSAGE_SKETCHYBAR_TEXT_COLOR=%s\nOPENUSAGE_SKETCHYBAR_USAGE_TRIGGER=%s\nOPENUSAGE_SKETCHYBAR_SWITCHER_TRIGGER=%s\nexport OPENUSAGE_BIN OPENUSAGE_SKETCHYBAR_ICON OPENUSAGE_SKETCHYBAR_GOOD_COLOR OPENUSAGE_SKETCHYBAR_WARN_COLOR OPENUSAGE_SKETCHYBAR_BAD_COLOR OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR OPENUSAGE_SKETCHYBAR_TEXT_COLOR OPENUSAGE_SKETCHYBAR_USAGE_TRIGGER OPENUSAGE_SKETCHYBAR_SWITCHER_TRIGGER\n",
		shellQuote(opts.Binary), shellQuote(preset.Icon), shellQuote(preset.Colors.Good), shellQuote(preset.Colors.Warn), shellQuote(preset.Colors.Bad), shellQuote(preset.Colors.Unknown), shellQuote(preset.Colors.Text), shellQuote(opts.UsageTrigger), shellQuote(opts.SwitcherTrigger))
	out := make([]byte, 0, len(data)+len(header))
	out = append(out, data[:newline+1]...)
	out = append(out, header...)
	out = append(out, data[newline+1:]...)
	return out
}

func printSnippet(out io.Writer, snippet string) error {
	if _, err := io.WriteString(out, snippet); err != nil {
		return fmt.Errorf("sketchybar: writing snippet: %w", err)
	}
	_, err := fmt.Fprintln(out, "\nTo apply, paste this block into sketchybarrc, or re-run with --write.")
	return err
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func normalizeTrigger(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "hover") {
		return "hover"
	}
	return "click"
}

// usageTriggerEvents lists the events the usage item subscribes to. The usage
// popup is read-only, so the item-scoped mouse.exited is what should dismiss
// it: the pointer moving off the item means the user is done looking.
func usageTriggerEvents(trigger string) string {
	if normalizeTrigger(trigger) == "hover" {
		return "mouse.entered mouse.exited mouse.exited.global"
	}
	return "mouse.clicked"
}

// switcherTriggerEvents deliberately omits mouse.exited. That event fires when
// the pointer leaves the *item*, which includes moving down into the item's
// own popup — and the picker's popup exists to be clicked. Subscribing to it
// would dismiss the menu the user is reaching for, making hover mode useless.
// Only mouse.exited.global ("the mouse leaves all parts of the bar") is safe.
func switcherTriggerEvents(trigger string) string {
	if normalizeTrigger(trigger) == "hover" {
		return "mouse.entered mouse.exited.global"
	}
	return "mouse.clicked"
}

func validSketchybarName(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}
