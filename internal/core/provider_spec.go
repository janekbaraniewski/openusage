package core

type ProviderAuthType string

const (
	ProviderAuthTypeUnknown ProviderAuthType = ""
	ProviderAuthTypeAPIKey  ProviderAuthType = "api_key"
	ProviderAuthTypeOAuth   ProviderAuthType = "oauth"
	ProviderAuthTypeCLI     ProviderAuthType = "cli"
	ProviderAuthTypeLocal   ProviderAuthType = "local"
	ProviderAuthTypeToken   ProviderAuthType = "token"
)

// ProviderAuthSpec defines how a provider authenticates and how users configure it.
type ProviderAuthSpec struct {
	Type             ProviderAuthType
	APIKeyEnv        string
	DefaultAccountID string
}

// ProviderSetupSpec describes setup entry points and quickstart instructions.
type ProviderSetupSpec struct {
	DocsURL    string
	Quickstart []string
}

// ProviderSpec is the canonical provider definition used for registration and UI metadata.
type ProviderSpec struct {
	ID        string
	Info      ProviderInfo
	Auth      ProviderAuthSpec
	Setup     ProviderSetupSpec
	Dashboard DashboardWidget
	Detail    DetailWidget
}
