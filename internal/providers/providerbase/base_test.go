package providerbase

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestNew_AppliesAPIKeyAuthDefaults(t *testing.T) {
	base := New(core.ProviderSpec{
		ID: "sample",
		Info: core.ProviderInfo{
			Name:   "Sample",
			DocURL: "https://example.com/docs",
		},
		Auth: core.ProviderAuthSpec{
			Type:      core.ProviderAuthTypeAPIKey,
			APIKeyEnv: "SAMPLE_API_KEY",
		},
	})

	spec := base.Spec()
	if spec.Setup.DocsURL != "https://example.com/docs" {
		t.Fatalf("setup docs = %q, want %q", spec.Setup.DocsURL, "https://example.com/docs")
	}
	// Auth metadata lives in Spec().Auth, not in DashboardWidget.
	if got := spec.Auth.APIKeyEnv; got != "SAMPLE_API_KEY" {
		t.Fatalf("spec auth APIKeyEnv = %q, want %q", got, "SAMPLE_API_KEY")
	}
	// DashboardWidget should NOT have auth fields copied into it.
	if got := base.DashboardWidget().APIKeyEnv; got != "" {
		t.Fatalf("dashboard APIKeyEnv = %q, want empty (auth data should not be copied)", got)
	}
	if got := base.DashboardWidget().DefaultAccountID; got != "" {
		t.Fatalf("dashboard DefaultAccountID = %q, want empty (auth data should not be copied)", got)
	}
	if len(base.DetailWidget().Sections) == 0 {
		t.Fatal("detail sections should have default sections")
	}
}

func TestNew_AuthMetadataInSpecNotWidget(t *testing.T) {
	base := New(core.ProviderSpec{
		ID: "sample",
		Info: core.ProviderInfo{
			Name: "Sample",
		},
		Auth: core.ProviderAuthSpec{
			Type:             core.ProviderAuthTypeAPIKey,
			APIKeyEnv:        "SAMPLE_API_KEY",
			DefaultAccountID: "sample-auth",
		},
		Dashboard: core.DashboardWidget{
			// Legacy fields still exist on the struct but should not be
			// the source of truth. TUI reads from Spec().Auth instead.
			APIKeyEnv:        "CUSTOM_API_KEY",
			DefaultAccountID: "sample-widget",
		},
		Detail: core.DefaultDetailWidget(),
	})

	spec := base.Spec()
	// The canonical source for auth metadata is Spec().Auth.
	if got := spec.Auth.APIKeyEnv; got != "SAMPLE_API_KEY" {
		t.Fatalf("spec auth APIKeyEnv = %q, want %q", got, "SAMPLE_API_KEY")
	}
	if got := spec.Auth.DefaultAccountID; got != "sample-auth" {
		t.Fatalf("spec auth DefaultAccountID = %q, want %q", got, "sample-auth")
	}
	// DashboardWidget preserves whatever was set explicitly on spec.Dashboard
	// (no copy logic from Auth), but TUI code should not read auth from here.
	if got := base.DashboardWidget().APIKeyEnv; got != "CUSTOM_API_KEY" {
		t.Fatalf("dashboard APIKeyEnv = %q, want %q (explicit value preserved)", got, "CUSTOM_API_KEY")
	}
	if spec.Dashboard.APIKeyEnv != "CUSTOM_API_KEY" {
		t.Fatalf("spec dashboard APIKeyEnv = %q, want %q", spec.Dashboard.APIKeyEnv, "CUSTOM_API_KEY")
	}
}
