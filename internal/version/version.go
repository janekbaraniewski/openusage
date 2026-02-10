package version

var (
	Version    = "dev"
	CommitHash = "unknown"
	BuildDate  = "unknown"
)

func String() string {
	return Version + " (" + CommitHash + ") built " + BuildDate
}
