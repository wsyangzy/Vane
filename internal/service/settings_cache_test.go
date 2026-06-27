package service

import "testing"

func TestSettingsServiceIncludesCacheDefaults(t *testing.T) {
	service := NewSettingsService(nil)

	var foundCategory bool
	for _, category := range service.GetCategories() {
		if category.Name == CategoryCache {
			foundCategory = true
			if category.Label == "" {
				t.Fatal("expected cache category label")
			}
		}
	}
	if !foundCategory {
		t.Fatal("expected cache category")
	}

	defaults := service.GetAllDefaults()
	values := make(map[string]string, len(defaults))
	for _, setting := range defaults {
		if setting.Category == CategoryCache {
			values[setting.Key] = setting.Value
		}
	}

	expected := map[string]string{
		"enabled":                   "true",
		"usage_stats_ttl":           "5s",
		"usage_summary_ttl":         "30s",
		"token_usage_ttl":           "5s",
		"endpoint_cost_today_ttl":   "10s",
		"endpoint_cost_history_ttl": "5m",
		"max_entries":               "1000",
		"stale_on_error":            "true",
	}
	for key, value := range expected {
		if values[key] != value {
			t.Fatalf("expected cache.%s default %q, got %q", key, value, values[key])
		}
	}
}
