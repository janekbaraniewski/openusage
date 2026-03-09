package dashboardapp

import (
	"context"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/integrations"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

type Service struct {
	ctx context.Context
}

func NewService(ctx context.Context) *Service {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Service{ctx: ctx}
}

func (s *Service) SaveTheme(themeName string) error {
	return config.SaveTheme(themeName)
}

func (s *Service) SaveDashboardProviders(providersCfg []config.DashboardProviderConfig) error {
	return config.SaveDashboardProviders(providersCfg)
}

func (s *Service) SaveDashboardView(view string) error {
	return config.SaveDashboardView(view)
}

func (s *Service) SaveDashboardWidgetSections(sections []config.DashboardWidgetSection) error {
	return config.SaveDashboardWidgetSections(sections)
}

func (s *Service) SaveDashboardHideSectionsWithNoData(hide bool) error {
	return config.SaveDashboardHideSectionsWithNoData(hide)
}

func (s *Service) SaveTimeWindow(window string) error {
	return config.SaveTimeWindow(window)
}

func (s *Service) ValidateAPIKey(accountID, providerID, apiKey string) (bool, string) {
	var provider core.UsageProvider
	for _, p := range providers.AllProviders() {
		if p.ID() == providerID {
			provider = p
			break
		}
	}
	if provider == nil {
		return false, "unknown provider"
	}

	parent := context.Background()
	if s != nil && s.ctx != nil {
		parent = s.ctx
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()

	snap, err := provider.Fetch(ctx, core.AccountConfig{
		ID:       accountID,
		Provider: providerID,
		Token:    apiKey,
	})
	if err != nil {
		return false, err.Error()
	}
	if snap.Status == core.StatusAuth || snap.Status == core.StatusError {
		msg := strings.TrimSpace(snap.Message)
		if msg == "" {
			msg = string(snap.Status)
		}
		return false, msg
	}
	return true, ""
}

func (s *Service) SaveCredential(accountID, apiKey string) error {
	return config.SaveCredential(accountID, apiKey)
}

func (s *Service) DeleteCredential(accountID string) error {
	return config.DeleteCredential(accountID)
}

func (s *Service) InstallIntegration(id integrations.ID) ([]integrations.Status, error) {
	manager := integrations.NewDefaultManager()
	err := manager.Install(id)
	return manager.ListStatuses(), err
}
