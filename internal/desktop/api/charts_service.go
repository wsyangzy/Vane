package api

import (
	"github.com/wsyangzy/Vane/internal/app"
)

type ChartsService struct{ app *app.App }

func (s *ChartsService) GetRequestTrendChart(minutes int, provider string) []app.ChartDataPoint {
	return s.app.GetRequestTrendChart(minutes, provider)
}

func (s *ChartsService) GetResponseTimeChart(minutes int, provider string) []app.ChartDataPoint {
	return s.app.GetResponseTimeChart(minutes, provider)
}

func (s *ChartsService) GetConnectionActivityChart(minutes int, provider string) []app.ChartDataPoint {
	return s.app.GetConnectionActivityChart(minutes, provider)
}

func (s *ChartsService) GetEndpointHealthChart() app.EndpointHealthData {
	return s.app.GetEndpointHealthChart()
}

func (s *ChartsService) GetCodexEndpointHealthChart() app.EndpointHealthData {
	return s.app.GetCodexEndpointHealthChart()
}

func (s *ChartsService) GetEndpointCosts(provider string) []app.EndpointCostItem {
	return s.app.GetEndpointCosts(provider)
}

func (s *ChartsService) GetEndpointAvailability(provider, channel, endpointName string, hours int) (app.EndpointAvailabilityData, error) {
	return s.app.GetEndpointAvailability(provider, channel, endpointName, hours)
}
