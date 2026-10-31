package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
	"github.com/wsyangzy/Vane/internal/tracking"
	"github.com/wsyangzy/Vane/internal/tracking/sessionusage"
)

func dashboardFixture(t *testing.T, query UsageDashboardQuery) (UsageDashboardQuery, time.Time, time.Time, time.Time, []dashboardRecord, []sessionusage.Activity) {
	t.Helper()
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	query, start, end, err := dashboardRange(query, now)
	if err != nil {
		t.Fatal(err)
	}
	makeRecord := func(session, provider, model, project, stamp string, input, output, cache int64, cost float64, priced bool) dashboardRecord {
		at, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			t.Fatal(err)
		}
		return dashboardRecord{Call: sessionusage.Call{ID: session + stamp, Session: session, Provider: provider, Model: model, Project: project, At: at, Tokens: sessionusage.Tokens{Input: input, Output: output, CacheRead: cache}}, cost: cost, priced: priced}
	}
	records := []dashboardRecord{
		makeRecord("same", "claude", "sonnet", "D:/work/one", "2026-10-26T18:00:00Z", 100, 20, 1000, 1, true),
		makeRecord("same", "claude", "sonnet", "D:/work/one", "2026-10-27T03:00:00Z", 200, 30, 2000, 2, true),
		makeRecord("same", "codex", "gpt", "D:/work/two", "2026-10-26T10:00:00Z", 10, 40, 3000, 3, true),
		makeRecord("unknown", "codex", "no-price", "", "2026-10-27T03:00:00Z", 1, 2, 100, 0, false),
		makeRecord("old", "codex", "gpt", "D:/work/two", "2026-08-01T00:00:00Z", 10, 10, 100, 1, true),
	}
	activity := []sessionusage.Activity{{Session: "same", Provider: "claude", Model: "sonnet", Project: "D:/work/one", At: records[0].At, Milliseconds: 60000, Prompts: 1}, {Session: "same", Provider: "codex", Model: "gpt", Project: "D:/work/two", At: records[2].At, Milliseconds: 30000, Prompts: 1}}
	return query, start, end, now, records, activity
}

func TestUsageDashboardAggregatesSessionsWithoutCacheDoubleCounting(t *testing.T) {
	q, start, end, now, records, activity := dashboardFixture(t, UsageDashboardQuery{Source: "local", Period: "7d"})
	got := buildUsageDashboard(q, start, end, now, records, activity)
	m := got.Summary
	if m.Requests != 4 || m.Sessions != 3 || m.Tokens != 403 || m.CacheReadTokens != 6100 || m.CostUSD != 6 || m.Unpriced != 1 || m.Projects != 2 || m.ActiveSeconds != 90 || m.Messages != 6 || m.ActiveDays != 2 {
		t.Fatalf("summary=%+v", m)
	}
	if m.MedianTokens != 50 || m.P90Tokens != 350 {
		t.Fatalf("quantiles=%+v", m)
	}
	if len(got.Days) != 7 || got.Days[6].Tokens != 353 || got.Days[5].Tokens != 50 || got.Calendar[0].Date != "2025-10-27" {
		t.Fatalf("wrong calendar: %+v", got.Days)
	}
	if len(got.Models) != 3 || len(got.Projects) != 3 || got.RowCount != 3 || len(got.UnpricedModels) != 1 {
		t.Fatalf("grouping=%+v", got)
	}
	if got.Rows[0].ID == got.Rows[1].ID {
		t.Fatal("same session ID across providers must remain distinct")
	}
	var calendarTokens int64
	for _, day := range got.Calendar {
		calendarTokens += day.Tokens
	}
	if calendarTokens != 423 {
		t.Fatalf("calendar should include old period-independent data: %d", calendarTokens)
	}
}

func TestUsageDashboardFiltersRankingsRowsAndKeepsTrendContext(t *testing.T) {
	q, start, end, now, records, activity := dashboardFixture(t, UsageDashboardQuery{Source: "local", Period: "7d", Provider: "claude", Project: "D:/work/one", Model: "sonnet", Day: "2026-10-27", PageSize: 1, Sort: "tokens"})
	got := buildUsageDashboard(q, start, end, now, records, activity)
	if got.Summary.Requests != 2 || got.RowCount != 1 || len(got.Models) != 1 || got.Summary.Tokens != 350 || got.Summary.ActiveSeconds != 60 {
		t.Fatalf("filters=%+v", got)
	}
	if got.Rows[0].Requests != 2 || got.Rows[0].Models[0] != "sonnet" || got.Rows[0].InputTokens != 300 {
		t.Fatalf("session row=%+v", got.Rows[0])
	}
	q.Day = "2026-10-26"
	zero := buildUsageDashboard(q, start, end, now, records, activity)
	if zero.Summary.Requests != 0 || zero.Days[6].Requests != 2 {
		t.Fatal("day filter must filter totals/rows but keep full trend context")
	}
	q.Day = ""
	q.Project = "D:/other"
	if empty := buildUsageDashboard(q, start, end, now, records, activity); empty.RowCount != 0 || len(empty.ProjectOptions) != 1 {
		t.Fatal("empty exact filter must retain selectable option inventory")
	}
}

func TestUsageDashboardCalendarDayOutsidePeriodKeepsTrendAndAnnualContext(t *testing.T) {
	for _, source := range []string{"local", "proxy"} {
		t.Run(source, func(t *testing.T) {
			q, start, end, now, records, activity := dashboardFixture(t, UsageDashboardQuery{Source: source, Period: "7d", Day: "2026-08-01"})
			got := buildUsageDashboard(q, start, end, now, records, activity)
			if got.Summary.Requests != 1 || got.Summary.Tokens != 20 || got.RowCount != 1 || len(got.Models) != 1 {
				t.Fatalf("selected day outside the trend was dropped: %+v", got)
			}
			if got.From != "2026-10-21" || len(got.Days) != 7 || got.Days[6].Tokens != 353 {
				t.Fatal("selecting a calendar day widened or replaced the trend period")
			}
			var annual int64
			for _, day := range got.Calendar {
				annual += day.Tokens
			}
			if annual != 423 {
				t.Fatal("day selection changed the annual activity distribution")
			}
		})
	}
	q, start, end, now, records, activity := dashboardFixture(t, UsageDashboardQuery{Source: "local", Period: "today", Day: "2026-10-26"})
	got := buildUsageDashboard(q, start, end, now, records, activity)
	if got.Summary.ActiveSeconds != 30 || got.Summary.Messages != 2 || got.Summary.Tokens != 50 || len(got.Days) != 1 {
		t.Fatal("activity and call totals must use the same selected day independently of today's trend")
	}
}

func TestUsageDashboardDesktopCalendarDayDoesNotRequireAllHistory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "absent"))
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	selected := time.Now().AddDate(0, 0, -50)
	lines := `{"type":"session_meta","payload":{"id":"selected-day","cwd":"D:\\work\\Vane"}}` + "\n" +
		`{"type":"turn_context","payload":{"model":"day-model"}}` + "\n" +
		`{"type":"token_usage_record","timestamp":"` + selected.UTC().Format(time.RFC3339Nano) + `","payload":{"response_id":"old-day","usage":{"input_tokens":100,"output_tokens":20}}}` + "\n" +
		`{"type":"token_usage_record","timestamp":"` + time.Now().UTC().Format(time.RFC3339Nano) + `","payload":{"response_id":"today","usage":{"input_tokens":300,"output_tokens":60}}}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "sessions", "days.jsonl"), []byte(lines), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	got, err := a.GetUsageDashboard(UsageDashboardQuery{Source: "local", Period: "today", Day: selected.Format(time.DateOnly)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary.Tokens != 120 || got.RowCount != 1 || len(got.Days) != 1 || got.Days[0].Tokens != 360 || got.From != time.Now().Format(time.DateOnly) {
		t.Fatalf("selected-day desktop query expanded history or lost records: %+v", got)
	}
}

func TestUsageDashboardProxyMergesEndpointsWithinCredentialGroups(t *testing.T) {
	tracker := newAppUsageTestTracker(t)
	defer tracker.Close()
	for _, row := range []struct {
		id, provider, channel, group, endpoint, environment string
		input, cache                                        int64
	}{
		{"a", "codex", "A", "same", "east", "release", 100, 70},
		{"b", "codex", "A", "same", "west", "release", 200, 100},
		{"c", "codex", "B", "same", "east", "release", 100, 0},
		{"d", "claude", "A", "same", "east", "release", 100, 70},
		{"dev", "codex", "A", "same", "east", "dev", 10000, 0},
	} {
		_, err := tracker.GetDB().Exec(`INSERT INTO request_logs(request_id,start_time,status,provider,channel,group_name,endpoint_name,environment,model_name,input_tokens,output_tokens,cache_read_tokens,total_cost_usd,duration_ms) VALUES(?,'2026-10-27 12:00:00','completed',?,?,?,?,?,'model',?,20,?,1,1500)`, row.id, row.provider, row.channel, row.group, row.endpoint, row.environment, row.input, row.cache)
		if err != nil {
			t.Fatal(err)
		}
	}
	items, err := tracker.QueryDashboardUsage(context.Background(), &tracking.QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("environment isolation: %+v", items)
	}
	q, start, end, now, _, _ := dashboardFixture(t, UsageDashboardQuery{Source: "proxy", Period: "7d"})
	records := make([]dashboardRecord, 0, len(items))
	for _, item := range items {
		records = append(records, dashboardRecord{Call: sessionusage.Call{ID: item.ID, Provider: item.Provider, Model: item.Model, At: item.At, Tokens: sessionusage.Tokens{Input: item.Input, Output: item.Output, CacheRead: item.CacheRead, CacheWrite: item.CacheWrite}}, channel: item.Channel, group: item.Group, status: item.Status, cost: item.Cost, priced: true, duration: item.DurationMs})
	}
	got := buildUsageDashboard(q, start, end, now, records, nil)
	if got.Summary.CredentialGroups != 3 || len(got.Credentials) != 3 || got.Summary.Tokens != 410 || got.Summary.CacheReadTokens != 240 || got.Summary.CostUSD != 4 || got.Summary.AvgDurationMs != 1500 {
		t.Fatalf("proxy=%+v", got.Summary)
	}
	if got.Credentials[0].Requests != 2 || got.Credentials[0].Tokens != 170 {
		t.Fatalf("group merge=%+v", got.Credentials)
	}
	q.Credential = credentialKey("codex", "A", "same")
	filtered := buildUsageDashboard(q, start, end, now, records, nil)
	if filtered.Summary.Requests != 2 || filtered.RowCount != 2 {
		t.Fatal("credential filter must include both endpoints only")
	}
}

func TestUsageDashboardPricingIncludesCacheAndUnknownModelsStayUnpriced(t *testing.T) {
	call := sessionusage.Call{Provider: "codex", Tokens: sessionusage.Tokens{Input: 20, Output: 10, CacheRead: 80}}
	price := &pricingstore.ModelPricingRecord{InputPrice: 10, OutputPrice: 20, CacheReadPrice: 1}
	cost, known := dashboardLocalCost(call, price)
	if !known || math.Abs(cost-.00048) > 1e-12 {
		t.Fatalf("cache cost=%f known=%v", cost, known)
	}
	call.CacheWrite = 10
	price.CacheWritePrice30m, price.CacheWritePrice12h = 1.25, 99
	cost, _ = dashboardLocalCost(call, price)
	if math.Abs(cost-.0004925) > 1e-12 {
		t.Fatalf("local estimates must use 30m write prices without TTL counters: %.8f", cost)
	}
	call.CacheWrite = 0
	price.LongContextThreshold = 99
	price.LongContextInputPrice = 30
	price.LongContextOutputPrice = 40
	cost, _ = dashboardLocalCost(call, price)
	if math.Abs(cost-.00108) > 1e-12 {
		t.Fatalf("long-context threshold must use original inclusive input: %.8f", cost)
	}
	call.Provider = "claude"
	call.Tokens = sessionusage.Tokens{Input: 20, CacheWrite: 100, CacheWrite1h: 40}
	price = &pricingstore.ModelPricingRecord{InputPrice: 10, CacheCreationPrice5m: 2, CacheCreationPrice1h: 5}
	cost, _ = dashboardLocalCost(call, price)
	if math.Abs(cost-.00052) > 1e-12 {
		t.Fatalf("cache TTL split cost %.8f", cost)
	}
	if cost, known := dashboardLocalCost(call, nil); known || cost != 0 {
		t.Fatal("unknown model must not get default guessed pricing")
	}
}

func TestUsageDashboardRangeAllPaginationAndErrors(t *testing.T) {
	q, start, end, now, records, activity := dashboardFixture(t, UsageDashboardQuery{Source: "local", Period: "all", Page: 999, PageSize: 1, Sort: "tokens"})
	got := buildUsageDashboard(q, start, end, now, records, activity)
	if got.From != "2026-08-01" || got.Page != 4 || got.RowCount != 4 || len(got.Rows) != 1 {
		t.Fatalf("range/page=%+v", got)
	}
	empty := buildUsageDashboard(q, start, end, now, nil, nil)
	if len(empty.Days) != 1 || empty.RowCount != 0 || empty.Page != 1 {
		t.Fatal("all empty should have today's single bucket")
	}
	for _, bad := range []UsageDashboardQuery{{Source: "unknown"}, {Provider: "unknown"}, {Period: "-1d"}, {Day: "secret"}} {
		if _, _, _, err := dashboardRange(bad, now); err == nil {
			t.Fatalf("bad query accepted: %+v", bad)
		}
	}
	for _, month := range []int{3, 11} {
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Fatal(err)
		}
		_, from, to, err := dashboardRange(UsageDashboardQuery{Period: "7d"}, time.Date(2026, time.Month(month), 9, 12, 0, 0, 0, loc))
		if err != nil {
			t.Fatal(err)
		}
		days := 0
		for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
			days++
		}
		if days != 7 {
			t.Fatal("DST changed calendar-day count")
		}
	}
}

func TestUsageDashboardDesktopPathReadsMetadataWithoutTranscriptContent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "absent"))
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	lines := `{"type":"session_meta","payload":{"id":"test-session","cwd":"D:\\work\\Vane"}}` + "\n" + `{"type":"turn_context","payload":{"model":"unknown-model"}}` + "\n" + `{"type":"event_msg","timestamp":"` + stamp + `","payload":{"type":"user_message","message":"优化会话明细 ` + strings.Repeat("示例", 120) + ` private-payload-never-retain"}}` + "\n" + `{"type":"token_usage_record","timestamp":"` + stamp + `","payload":{"response_id":"r","usage":{"input_tokens":100,"cached_input_tokens":70,"output_tokens":20}}}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "sessions", "sample.jsonl"), []byte(lines), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	got, err := a.GetUsageDashboard(UsageDashboardQuery{Source: "local", Period: "today"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary.Sessions != 1 || got.Summary.Tokens != 50 || got.Summary.Unpriced != 1 || got.Summary.Projects != 1 || got.Summary.Messages != 2 {
		t.Fatalf("native DTO=%+v", got)
	}
	data, _ := json.Marshal(got)
	if strings.Contains(string(data), "private-payload") {
		t.Fatal("transcript payload leaked into DTO")
	}
	if len(got.Rows) != 1 || !strings.HasPrefix(got.Rows[0].Title, "优化会话明细") {
		t.Fatal("display title absent from DTO")
	}
	proxy, err := a.GetUsageDashboard(UsageDashboardQuery{Source: "proxy"})
	if err != nil || proxy.Summary.Requests != 0 {
		t.Fatal("native records must not populate proxy source")
	}
}

func TestUsageDashboardSessionTitlesSearchAndProjectMetadataSurvivePagination(t *testing.T) {
	query, start, end, now, _, _ := dashboardFixture(t, UsageDashboardQuery{Source: "local", Period: "30d", PageSize: 1})
	var records []dashboardRecord
	for i := 0; i < 26; i++ {
		project := "D:/work/team-a/Vane"
		if i == 25 {
			project = "D:/work/team-b/Vane"
		}
		records = append(records, dashboardRecord{Call: sessionusage.Call{ID: fmt.Sprintf("call-%d", i), Provider: "codex", Session: fmt.Sprintf("session-%d", i), Project: project, Title: fmt.Sprintf("修复会话问题 %d", i), At: now.Add(-time.Duration(i) * time.Minute), Tokens: sessionusage.Tokens{Input: 100}}, priced: true})
	}
	got := buildUsageDashboard(query, start, end, now, records, nil)
	if len(got.Rows) != 1 || got.RowCount != 26 || got.Rows[0].Title != "修复会话问题 0" || len(got.Projects) != 2 {
		t.Fatalf("rows=%+v projects=%+v", got.Rows, got.Projects)
	}
	for _, project := range got.Projects {
		if project.LastAt == "" || project.Title == "" {
			t.Fatal("project metadata absent")
		}
		if project.Key == "D:/work/team-a/Vane" && (project.Sessions != 25 || project.Title != "修复会话问题 0") {
			t.Fatalf("project used only the current page: %+v", project)
		}
	}
	query.Search = "会话问题 24"
	matched := buildUsageDashboard(query, start, end, now, records, nil)
	if matched.RowCount != 1 || matched.Rows[0].Title != "修复会话问题 24" || matched.Summary.Sessions != 1 {
		t.Fatalf("title search=%+v", matched)
	}
	query.Search, query.Project, query.Page = "", "D:/work/team-a/Vane", 2
	project := buildUsageDashboard(query, start, end, now, records, nil)
	if project.RowCount != 25 || project.Rows[0].Project != query.Project || project.Page != 2 {
		t.Fatalf("project pagination=%+v", project)
	}
	query.Project = "\x00"
	records[0].Project = ""
	unknown := buildUsageDashboard(query, start, end, now, records, nil)
	if unknown.RowCount != 1 || unknown.Rows[0].Project != "" {
		t.Fatal("unrecorded project must not select every project")
	}
}
