package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/tracking/sessionusage"
)

func TestLocalHeatmapCalendarAndTimezone(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	data := `{"type":"turn_context","payload":{"model":"gpt"}}` + "\n" + `{"type":"event_msg","timestamp":"2026-10-26T18:00:00Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"output_tokens":20,"cached_input_tokens":70},"total_token_usage":{"input_tokens":100,"output_tokens":20,"cached_input_tokens":70}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "sessions", "a.jsonl"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, loc)
	days, err := queryLocalHeatmap(context.Background(), sessionusage.NewReader(), sessionusage.Roots{Codex: root}, now, "codex", "gpt")
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 366 || days[0].Date != "2025-10-27" || days[len(days)-1].Date != "2026-10-27" {
		t.Fatalf("range: %d %+v %+v", len(days), days[0], days[len(days)-1])
	}
	last := days[len(days)-1]
	if last.InputTokens != 30 || last.CacheReadTokens != 70 || last.OutputTokens != 20 || last.TotalTokens != 120 || last.RequestCount != 1 || last.Intensity != 1 {
		t.Fatalf("last=%+v", last)
	}
	if days[len(days)-2].TotalTokens != 0 {
		t.Fatal("UTC date used instead of configured local day")
	}
}

func TestHeatmapCalendarUsesCalendarDaysAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range []time.Time{time.Date(2026, 3, 8, 12, 0, 0, 0, loc), time.Date(2026, 11, 1, 12, 0, 0, 0, loc)} {
		start, end := heatmapCalendarRange(day)
		if start.Weekday() != time.Monday || start.Hour() != 0 || end.Hour() != 0 {
			t.Fatalf("range %v %v", start, end)
		}
		count := 0
		for cursor := start; cursor.Before(end); cursor = cursor.AddDate(0, 0, 1) {
			count++
		}
		if count != 371 {
			t.Fatalf("calendar days %d", count)
		}
	}
}

func TestTokenUsageHeatmapReplacesProxyDataThroughExistingAPI(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "missing-claude"))
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	data := `{"type":"turn_context","payload":{"model":"native-model"}}` + "\n" +
		`{"type":"event_msg","timestamp":"` + stamp + `","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"output_tokens":20,"cached_input_tokens":70},"total_token_usage":{"input_tokens":100,"output_tokens":20,"cached_input_tokens":70}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "sessions", "a.jsonl"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	// No usage tracker is installed. Old list-only filters must not hide native calls.
	a := &App{}
	days, err := a.GetTokenUsageHeatmap(RequestQueryParams{Provider: "codex", Model: "native-model", StartDate: "2000-01-01", EndDate: "2000-01-02", Status: "failed", Channel: "unrelated", Endpoint: "unrelated", Group: "unrelated"})
	if err != nil {
		t.Fatal(err)
	}
	var calls, tokens int64
	for _, day := range days {
		calls += day.RequestCount
		tokens += day.TotalTokens
		if day.Bucket != "day" {
			t.Fatal("non-calendar bucket")
		}
	}
	if calls != 1 || tokens != 120 || len(days) < 365 {
		t.Fatalf("calls=%d tokens=%d days=%d", calls, tokens, len(days))
	}
	filtered, err := a.GetTokenUsageHeatmap(RequestQueryParams{Model: "other-model"})
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range filtered {
		if day.TotalTokens != 0 || day.RequestCount != 0 {
			t.Fatal("model filter ignored")
		}
	}
}
