package cursor

// LocalSourcePaths returns the on-disk locations the provider reads on each
// Fetch. The path resolution mirrors defaultTrackingDBPath / defaultStateDBPath
// in telemetry.go, with platform-specific roots resolved at call time.
func (p *Provider) LocalSourcePaths() []string {
	paths := make([]string, 0, 2)
	if v := defaultTrackingDBPath(); v != "" {
		paths = append(paths, v)
	}
	if v := defaultStateDBPath(); v != "" {
		paths = append(paths, v)
	}
	return paths
}
