package main

import (
	"encoding/json"
	"math"
	"testing"

	"ai-relay-forwarder/internal/store"
)

func TestModelPricingWailsDTOCarriesLongContext(t *testing.T) {
	inputValues := decodePricingJSON(t, `{
        "model_name": "gpt-long",
        "provider": "codex",
        "long_context_threshold": 272000,
        "long_context_input_price": 7.5,
        "long_context_cache_read_price": 0.75,
        "long_context_cache_write_price": 9.375,
        "long_context_output_price": 45
    }`, &CreateModelPricingInput{})
	assertPricingJSONNumber(t, inputValues, "long_context_threshold", 272000)
	assertPricingJSONNumber(t, inputValues, "long_context_input_price", 7.5)
	assertPricingJSONNumber(t, inputValues, "long_context_cache_read_price", 0.75)
	assertPricingJSONNumber(t, inputValues, "long_context_cache_write_price", 9.375)
	assertPricingJSONNumber(t, inputValues, "long_context_output_price", 45)

	info := (&App{}).pricingRecordToInfo(&store.ModelPricingRecord{
		LongContextThreshold:       300_000,
		LongContextInputPrice:      8,
		LongContextCacheReadPrice:  0.8,
		LongContextCacheWritePrice: 10,
		LongContextOutputPrice:     48,
	})
	infoValues := encodePricingJSON(t, info)
	assertPricingJSONNumber(t, infoValues, "long_context_threshold", 300000)
	assertPricingJSONNumber(t, infoValues, "long_context_input_price", 8)
	assertPricingJSONNumber(t, infoValues, "long_context_cache_read_price", 0.8)
	assertPricingJSONNumber(t, infoValues, "long_context_cache_write_price", 10)
	assertPricingJSONNumber(t, infoValues, "long_context_output_price", 48)
}

func decodePricingJSON(t *testing.T, payload string, target any) map[string]any {
	t.Helper()
	if err := json.Unmarshal([]byte(payload), target); err != nil {
		t.Fatalf("decode pricing JSON: %v", err)
	}
	return encodePricingJSON(t, target)
}

func encodePricingJSON(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode pricing JSON: %v", err)
	}
	var values map[string]any
	if err := json.Unmarshal(encoded, &values); err != nil {
		t.Fatalf("decode pricing JSON map: %v", err)
	}
	return values
}

func assertPricingJSONNumber(t *testing.T, values map[string]any, key string, expected float64) {
	t.Helper()
	actual, ok := values[key].(float64)
	if !ok {
		t.Fatalf("expected numeric JSON field %s, got %#v", key, values[key])
	}
	if math.Abs(actual-expected) > 0.000001 {
		t.Fatalf("expected %s=%f, got %f", key, expected, actual)
	}
}
