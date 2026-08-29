package cursor

// LocalSourcePaths returns the on-disk locations the provider reads on each Fetch.
func (p *Provider) LocalSourcePaths() []string {
	if path := DefaultStatusFilePath(); path != "" {
		return []string{path}
	}
	return nil
}
