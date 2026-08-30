package cursor

// LocalSourcePaths returns the on-disk locations the provider reads on each Fetch.
func (p *Provider) LocalSourcePaths() []string {
	var paths []string
	if path := DefaultStatusFilePath(); path != "" {
		paths = append(paths, path)
	}
	if path := DefaultStateDBPath(); path != "" {
		paths = append(paths, path)
	}
	return paths
}
