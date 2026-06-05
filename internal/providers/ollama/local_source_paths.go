package ollama

import "github.com/janekbaraniewski/openusage/internal/core"

// LocalSourcePaths returns the on-disk locations the provider reads. Mirrors
// resolveDesktopDBPath/resolveServerConfigPath/resolveServerLogFiles in
// local_paths.go using a zero-value account so the platform defaults apply.
// Used by internal/tmux active-tool detection.
func (p *Provider) LocalSourcePaths() []string {
	var acct core.AccountConfig
	paths := []string{}
	if v := resolveDesktopDBPath(acct); v != "" {
		paths = append(paths, v)
	}
	if v := resolveServerConfigPath(acct); v != "" {
		paths = append(paths, v)
	}
	paths = append(paths, resolveServerLogFiles(acct)...)
	return paths
}
