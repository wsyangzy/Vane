package api

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/wsyangzy/Vane/internal/app"
)

func Services(runtime *app.App) []application.Service {
	return []application.Service{
		application.NewService(&CacheService{app: runtime}),
		application.NewService(&ChartsService{app: runtime}),
		application.NewService(&ClaudeService{app: runtime}),
		application.NewService(&CodexService{app: runtime}),
		application.NewService(&LogsService{app: runtime}),
		application.NewService(&PricingService{app: runtime}),
		application.NewService(&PrivacyService{app: runtime}),
		application.NewService(&RoutingService{app: runtime}),
		application.NewService(&SettingsService{app: runtime}),
		application.NewService(&SubscriptionsService{app: runtime}),
		application.NewService(&SystemService{app: runtime}),
		application.NewService(&UsageService{app: runtime}),
	}
}
