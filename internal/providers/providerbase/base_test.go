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
	if got := base.DashboardWidget().APIKeyEnv; got != "SAMPLE_API_KEY" {
		t.Fatalf("dashboard APIKeyEnv = %q, want %q", got, "SAMPLE_API_KEY")
	}
	if got := base.DashboardWidget().DefaultAccountID; got != "sample" {
		t.Fatalf("default account ID = %q, want %q", got, "sample")
	}
	if len(base.DetailWidget().Sections) == 0 {
		t.Fatal("detail sections should have default sections")
	}
}

func TestNew_RespectsExplicitDashboardAuthFields(t *testing.T) {
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
			APIKeyEnv:        "CUSTOM_API_KEY",
			DefaultAccountID: "sample-widget",
		},
		Detail: core.DefaultDetailWidget(),
	})

	spec := base.Spec()
	if got := base.DashboardWidget().APIKeyEnv; got != "CUSTOM_API_KEY" {
		t.Fatalf("dashboard APIKeyEnv = %q, want %q", got, "CUSTOM_API_KEY")
	}
	if got := base.DashboardWidget().DefaultAccountID; got != "sample-widget" {
		t.Fatalf("default account ID = %q, want %q", got, "sample-widget")
	}
	if spec.Dashboard.APIKeyEnv != "CUSTOM_API_KEY" {
		t.Fatalf("spec dashboard APIKeyEnv = %q, want %q", spec.Dashboard.APIKeyEnv, "CUSTOM_API_KEY")
	}
}
