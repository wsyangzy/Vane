package api

import (
	"github.com/wsyangzy/Vane/internal/app"
)

type PricingService struct{ app *app.App }

func (s *PricingService) GetModelPricingStorageStatus() app.ModelPricingStorageStatus {
	return s.app.GetModelPricingStorageStatus()
}

func (s *PricingService) GetModelPricings() ([]app.ModelPricingInfo, error) {
	return s.app.GetModelPricings()
}

func (s *PricingService) GetModelPricingsByProvider(provider string) ([]app.ModelPricingInfo, error) {
	return s.app.GetModelPricingsByProvider(provider)
}

func (s *PricingService) GetModelPricing(modelName string) (app.ModelPricingInfo, error) {
	return s.app.GetModelPricing(modelName)
}

func (s *PricingService) CreateModelPricing(input app.CreateModelPricingInput) error {
	return s.app.CreateModelPricing(input)
}

func (s *PricingService) UpdateModelPricing(modelName string, input app.CreateModelPricingInput) error {
	return s.app.UpdateModelPricing(modelName, input)
}

func (s *PricingService) DeleteModelPricing(provider, modelName string) error {
	return s.app.DeleteModelPricing(provider, modelName)
}

func (s *PricingService) DeleteModelPricingTier(provider, modelName, serviceTier string) error {
	return s.app.DeleteModelPricingTier(provider, modelName, serviceTier)
}

func (s *PricingService) SetDefaultModelPricing(modelName string) error {
	return s.app.SetDefaultModelPricing(modelName)
}
