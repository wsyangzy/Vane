package service

import (
	"testing"

	"ai-relay-forwarder/internal/store"
)

func TestModelPricingService_ToTrackingPricingLongContext(t *testing.T) {
	service := NewModelPricingService(nil)
	pricing := service.ToTrackingPricing(&store.ModelPricingRecord{
		InputPrice:                 5,
		OutputPrice:                30,
		CacheCreationPrice5m:       6.25,
		CacheReadPrice:             0.5,
		LongContextThreshold:       272_000,
		LongContextInputPrice:      7.5,
		LongContextCacheReadPrice:  0.75,
		LongContextCacheWritePrice: 9.375,
		LongContextOutputPrice:     45,
	})

	if pricing.LongContextThreshold != 272_000 ||
		pricing.LongContextInputPrice != 7.5 ||
		pricing.LongContextCacheReadPrice != 0.75 ||
		pricing.LongContextCacheWritePrice != 9.375 ||
		pricing.LongContextOutputPrice != 45 {
		t.Fatalf("long-context pricing was not mapped: %+v", pricing)
	}
}

func TestModelPricingService_RejectsNegativeLongContextPricing(t *testing.T) {
	service := NewModelPricingService(nil)
	tests := []struct {
		name   string
		record *store.ModelPricingRecord
	}{
		{name: "threshold", record: &store.ModelPricingRecord{ModelName: "m", LongContextThreshold: -1}},
		{name: "input", record: &store.ModelPricingRecord{ModelName: "m", LongContextInputPrice: -1}},
		{name: "cache read", record: &store.ModelPricingRecord{ModelName: "m", LongContextCacheReadPrice: -1}},
		{name: "cache write", record: &store.ModelPricingRecord{ModelName: "m", LongContextCacheWritePrice: -1}},
		{name: "output", record: &store.ModelPricingRecord{ModelName: "m", LongContextOutputPrice: -1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := service.validateRecord(test.record); err == nil {
				t.Fatal("expected negative long-context value to be rejected")
			}
		})
	}
}
