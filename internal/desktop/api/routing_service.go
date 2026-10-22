package api

import (
	"github.com/wsyangzy/Vane/internal/app"
	"github.com/wsyangzy/Vane/internal/routing"
)

type RoutingService struct{ app *app.App }

func (s *RoutingService) CheckPoolEndpoint(provider string, id int64) error {
	return s.app.CheckPoolEndpoint(provider, id)
}

func (s *RoutingService) ListAccountPools(provider string) (app.AccountPoolState, error) {
	return s.app.ListAccountPools(provider)
}
func (s *RoutingService) SaveAccountPool(input routing.Pool) (int64, error) {
	return s.app.SaveAccountPool(input)
}
func (s *RoutingService) DeleteAccountPool(provider string, id int64) error {
	return s.app.DeleteAccountPool(provider, id)
}
func (s *RoutingService) RotateAccountPoolKey(provider string, id int64) (app.PoolKeyResult, error) {
	return s.app.RotateAccountPoolKey(provider, id)
}
func (s *RoutingService) SetPoolAccountEnabled(provider string, channelID, groupID int64, enabled bool) error {
	return s.app.SetPoolAccountEnabled(provider, channelID, groupID, enabled)
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
