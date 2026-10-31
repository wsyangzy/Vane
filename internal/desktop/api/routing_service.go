package api

import (
	"github.com/wsyangzy/Vane/internal/app"
	"github.com/wsyangzy/Vane/internal/routing"
)

type RoutingService struct{ app *app.App }

func (s *RoutingService) ListManualBalances(provider string) ([]routing.ManualBalance, error) {
	return s.app.ListManualBalances(provider)
}
func (s *RoutingService) UpdateManualBalance(input routing.ManualBalanceUpdate) (*routing.ManualBalance, error) {
	return s.app.UpdateManualBalance(input)
}
func (s *RoutingService) ListManualBalanceEntries(provider string, channelID, groupID int64) ([]routing.BalanceEntry, error) {
	return s.app.ListManualBalanceEntries(provider, channelID, groupID)
}

func (s *RoutingService) ListRoutingProfiles(provider string) (app.RoutingProfileState, error) {
	return s.app.ListRoutingProfiles(provider)
}
func (s *RoutingService) GetRoutingProfile(provider string, id int64) (*routing.RoutingProfile, error) {
	return s.app.GetRoutingProfile(provider, id)
}
func (s *RoutingService) SaveRoutingProfile(profile routing.RoutingProfile) (routing.RoutingProfile, error) {
	return s.app.SaveRoutingProfile(profile)
}
func (s *RoutingService) DeleteRoutingProfile(provider string, id int64) error {
	return s.app.DeleteRoutingProfile(provider, id)
}
func (s *RoutingService) SetDefaultRoutingProfile(provider string, id int64) error {
	return s.app.SetDefaultRoutingProfile(provider, id)
}
func (s *RoutingService) PreviewDispatch(input app.DispatchInput) (routing.DispatchPlan, error) {
	return s.app.PreviewDispatch(input)
}
func (s *RoutingService) ClassifyIntent(input app.DispatchInput) (routing.IntentResult, error) {
	return s.app.ClassifyIntent(input)
}
func (s *RoutingService) ListRoutingQuota(provider string) ([]routing.QuotaSnapshot, error) {
	return s.app.ListRoutingQuota(provider)
}
func (s *RoutingService) RefreshRoutingQuota(provider string, channelID, groupID int64, model string) ([]routing.QuotaSnapshot, error) {
	return s.app.RefreshRoutingQuota(provider, channelID, groupID, model)
}
func (s *RoutingService) ListDispatchTraces(provider string, limit int) ([]routing.DispatchTrace, error) {
	return s.app.ListDispatchTraces(provider, limit)
}
func (s *RoutingService) CheckRoutingConfiguration(provider string) (app.RoutingConfigurationStatus, error) {
	return s.app.CheckRoutingConfiguration(provider)
}
func (s *RoutingService) CheckPoolEndpoint(provider string, id int64) error {
	return s.app.CheckPoolEndpoint(provider, id)
}

func (s *RoutingService) ListModelRoutes(provider string, sourceChannelID int64) ([]app.ModelRouteInfo, error) {
	return s.app.ListModelRoutes(provider, sourceChannelID)
}

func (s *RoutingService) SaveModelRoute(input app.SaveModelRouteInput) (app.RouteSaveResult, error) {
	return s.app.SaveModelRoute(input)
}

func (s *RoutingService) DeleteModelRoute(id int64) error {
	return s.app.DeleteModelRoute(id)
}

func (s *RoutingService) SetModelRouteEnabled(id int64, enabled bool) error {
	return s.app.SetModelRouteEnabled(id, enabled)
}

func (s *RoutingService) GetRouteTargetOptions(provider string) (app.RouteTargetOptions, error) {
	return s.app.GetRouteTargetOptions(provider)
}

func (s *RoutingService) PreviewRoute(provider string, sourceChannelID int64, model string, streaming bool) (app.RoutePreviewResult, error) {
	return s.app.PreviewRoute(provider, sourceChannelID, model, streaming)
}

func (s *RoutingService) SetChannelNonStreamRoute(provider string, channelID int64, input app.NonStreamRouteInput) (app.RouteSaveResult, error) {
	return s.app.SetChannelNonStreamRoute(provider, channelID, input)
}

func (s *RoutingService) SetChannelPrivacyLevel(provider string, channelID int64, level string) error {
	return s.app.SetChannelPrivacyLevel(provider, channelID, level)
}
