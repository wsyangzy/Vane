package api

import (
	"github.com/wsyangzy/Vane/internal/app"
)

type UsageService struct{ app *app.App }

func (s *UsageService) GetUsageDashboard(query app.UsageDashboardQuery) (app.UsageDashboard, error) {
	return s.app.GetUsageDashboard(query)
}

func (s *UsageService) GetCredentialGroupDailyUsage(provider, channel, groupName string, limitUSD float64) (app.CredentialGroupDailyUsage, error) {
	return s.app.GetCredentialGroupDailyUsage(provider, channel, groupName, limitUSD)
}

func (s *UsageService) GetUsageSummary(startTimeStr, endTimeStr, provider string) (app.UsageSummary, error) {
	return s.app.GetUsageSummary(startTimeStr, endTimeStr, provider)
}

func (s *UsageService) GetRequests(params app.RequestQueryParams) (app.RequestListResult, error) {
	return s.app.GetRequests(params)
}

func (s *UsageService) GetUsageStats(params app.UsageStatsQueryParams) (app.UsageStatsData, error) {
	return s.app.GetUsageStats(params)
}

func (s *UsageService) GetTokenUsage(provider string) (app.TokenUsageData, error) {
	return s.app.GetTokenUsage(provider)
}

func (s *UsageService) GetTokenUsageHeatmap(params app.RequestQueryParams) ([]app.TokenUsageHeatmapDay, error) {
	return s.app.GetTokenUsageHeatmap(params)
}
