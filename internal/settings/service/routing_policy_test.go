package service

import (
	"context"
	"testing"
)

func TestUnifiedRuleFallbackInitializationPreservesSavedPreference(t *testing.T) {
	for _, legacy := range []string{"", "true", "false", "invalid"} {
		t.Run("legacy="+legacy, func(t *testing.T) {
			svc, _ := newSettingsServiceTestStore(t)
			ctx := context.Background()
			if legacy != "" {
				if err := svc.store.Set(ctx, CategoryRetry, "model_rule_fallback", legacy); err != nil {
					t.Fatal(err)
				}
			}
			// Other former type defaults must not override the unified choice.
			for _, key := range []string{"mapping_rule_fallback", "non_stream_rule_fallback", "intent_rule_fallback"} {
				if err := svc.store.Set(ctx, CategoryRetry, key, "false"); err != nil {
					t.Fatal(err)
				}
			}
			if err := svc.InitDefaults(ctx); err != nil {
				t.Fatal(err)
			}
			_, rules := svc.RoutingPolicy(ctx)
			if rules.FallbackToDefault != (legacy != "false") {
				t.Fatalf("unexpected migrated preference: %+v", rules)
			}
			// A saved unified value wins on later starts, even when legacy differs.
			for _, value := range []string{"false", "true"} {
				if err := svc.Set(ctx, CategoryRetry, "rule_fallback_to_default", value); err != nil {
					t.Fatal(err)
				}
				if err := svc.InitDefaults(ctx); err != nil {
					t.Fatal(err)
				}
				_, rules = svc.RoutingPolicy(ctx)
				if rules.FallbackToDefault != (value == "true") {
					t.Fatal("initialization overwrote the saved unified preference")
				}
			}
			if legacy != "" {
				record, err := svc.store.Get(ctx, CategoryRetry, "model_rule_fallback")
				if err != nil || record == nil || record.Value != legacy {
					t.Fatalf("legacy value was changed: %+v %v", record, err)
				}
			}
			for _, key := range []string{"model_rule_fallback", "mapping_rule_fallback", "non_stream_rule_fallback", "intent_rule_fallback"} {
				if IsCurrentRetrySetting(key) {
					t.Fatalf("retired setting is still visible: %s", key)
				}
				if err := svc.Set(ctx, CategoryRetry, key, "true"); err == nil {
					t.Fatalf("retired setting accepted a write: %s", key)
				}
			}
		})
	}
}
