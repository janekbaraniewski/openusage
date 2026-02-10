// Package version holds build-time metadata injected via ldflags.
package version

// These variables are set at build time using -ldflags:
//
//	-X 'github.com/janekbaraniewski/agentusage/internal/version.Version=...'
//	-X 'github.com/janekbaraniewski/agentusage/internal/version.CommitHash=...'
//	-X 'github.com/janekbaraniewski/agentusage/internal/version.BuildDate=...'
var (
	Version    = "dev"
	CommitHash = "unknown"
	BuildDate  = "unknown"
)

// String returns a formatted version string.
func String() string {
	return Version + " (" + CommitHash + ") built " + BuildDate
}
