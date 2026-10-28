package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	pricingstore "github.com/wsyangzy/Vane/internal/pricing/store"
	"github.com/wsyangzy/Vane/internal/tracking"
	"github.com/wsyangzy/Vane/internal/tracking/sessionusage"
)

type UsageDashboardQuery struct {
	Source     string `json:"source"`
	Period     string `json:"period"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Project    string `json:"project"`
	Credential string `json:"credential"`
	Day        string `json:"day"`
	Search     string `json:"search"`
	Sort       string `json:"sort"`
	Page       int    `json:"page"`
	PageSize   int    `json:"page_size"`
}

type UsageDashboardMetrics struct {
	Requests            int64    `json:"requests"`
	Sessions            int64    `json:"sessions"`
	Messages            int64    `json:"messages"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	Tokens              int64    `json:"tokens"`
	CostUSD             float64  `json:"cost_usd"`
	Unpriced            int64    `json:"unpriced"`
	ActiveSeconds       float64  `json:"active_seconds"`
	ActiveDays          int64    `json:"active_days"`
	Projects            int64    `json:"projects"`
	CredentialGroups    int64    `json:"credential_groups"`
	MedianTokens        int64    `json:"median_tokens"`
	P90Tokens           int64    `json:"p90_tokens"`
	SuccessRequests     int64    `json:"success_requests"`
	FailedRequests      int64    `json:"failed_requests"`
	AvgDurationMs       float64  `json:"avg_duration_ms"`
	CacheHitRate        *float64 `json:"cache_hit_rate"`
}

type UsageDashboardDay struct {
	Date string `json:"date"`
	UsageDashboardMetrics
}

type UsageDashboardGroup struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Title    string `json:"title"`
	LastAt   string `json:"last_at"`
	Provider string `json:"provider"`
	Channel  string `json:"channel"`
	Group    string `json:"group"`
	UsageDashboardMetrics
}

type UsageDashboardRow struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Provider string   `json:"provider"`
	Project  string   `json:"project"`
	Channel  string   `json:"channel"`
	Group    string   `json:"group"`
	Models   []string `json:"models"`
	FirstAt  string   `json:"first_at"`
	LastAt   string   `json:"last_at"`
	Status   string   `json:"status"`
	UsageDashboardMetrics
}

type UsageDashboard struct {
	Source            string                `json:"source"`
	From              string                `json:"from"`
	To                string                `json:"to"`
	Timezone          string                `json:"timezone"`
	UpdatedAt         string                `json:"updated_at"`
	Summary           UsageDashboardMetrics `json:"summary"`
	Days              []UsageDashboardDay   `json:"days"`
	Calendar          []UsageDashboardDay   `json:"calendar"`
	Projects          []UsageDashboardGroup `json:"projects"`
	Models            []UsageDashboardGroup `json:"models"`
	Providers         []UsageDashboardGroup `json:"providers"`
	Credentials       []UsageDashboardGroup `json:"credentials"`
	ModelOptions      []string              `json:"model_options"`
	ProjectOptions    []string              `json:"project_options"`
	CredentialOptions []UsageDashboardGroup `json:"credential_options"`
	UnpricedModels    []string              `json:"unpriced_models"`
	Rows              []UsageDashboardRow   `json:"rows"`
	RowCount          int                   `json:"row_count"`
	Page              int                   `json:"page"`
	PageSize          int                   `json:"page_size"`
}

type dashboardRecord struct {
	sessionusage.Call
	channel, group, status string
	cost                   float64
	priced                 bool
	duration               int64
}

// GetUsageDashboard reads each source independently. Local sessions are never
// imported into the proxy ledger or attributed to guessed channels/accounts.
func (a *App) GetUsageDashboard(query UsageDashboardQuery) (UsageDashboard, error) {
	a.mu.RLock()
	cfg, tracker, pricing := a.config, a.usageTracker, a.modelPricingService
	a.mu.RUnlock()
	loc := time.Local
	if cfg != nil && cfg.Timezone != "" {
		parsed, err := time.LoadLocation(cfg.Timezone)
		if err != nil {
			return UsageDashboard{}, fmt.Errorf("无效的统计时区: %w", err)
		}
		loc = parsed
	}
	now := time.Now().In(loc)
	query, start, end, err := dashboardRange(query, now)
	if err != nil {
		return UsageDashboard{}, err
	}
	calendarStart, _ := heatmapCalendarRange(now)
	readStart := start
	if readStart.IsZero() || calendarStart.Before(readStart) {
		readStart = calendarStart
	}
	if query.Period == "all" {
		readStart = time.Date(1970, 1, 1, 0, 0, 0, 0, loc)
	}
	if query.Day != "" {
		selected, _ := time.ParseInLocation(time.DateOnly, query.Day, loc)
		if selected.Before(readStart) {
			readStart = selected
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var records []dashboardRecord
	var activity []sessionusage.Activity
	if query.Source == "local" {
		roots, err := sessionusage.NativeRoots()
		if err != nil {
			return UsageDashboard{}, err
		}
		snapshot, err := a.usageReader().Read(ctx, roots, readStart, end, query.Provider, "")
		if err != nil {
			return UsageDashboard{}, err
		}
		var prices []*pricingstore.ModelPricingRecord
		if pricing != nil {
			prices, err = pricing.ListPricings(ctx)
			if err != nil {
				return UsageDashboard{}, fmt.Errorf("读取用量估算定价失败: %w", err)
			}
		}
		priceMap := make(map[string]*pricingstore.ModelPricingRecord)
		for _, price := range prices {
			if price.ServiceTier == "" || price.ServiceTier == "standard" {
				priceMap[price.Provider+"/"+price.ModelName] = price
			}
		}
		for _, call := range snapshot.Calls {
			cost, known := dashboardLocalCost(call, priceMap[call.Provider+"/"+call.Model])
			records = append(records, dashboardRecord{Call: call, cost: cost, priced: known})
		}
		activity = snapshot.Activity
	} else if tracker != nil {
		last := end.Add(-time.Nanosecond)
		items, err := tracker.QueryDashboardUsage(ctx, &tracking.QueryOptions{StartDate: &readStart, EndDate: &last, Provider: query.Provider})
		if err != nil {
			return UsageDashboard{}, err
		}
		for _, item := range items {
			records = append(records, dashboardRecord{Call: sessionusage.Call{ID: item.ID, Provider: item.Provider, Model: item.Model, At: item.At, Tokens: sessionusage.Tokens{Input: item.Input, Output: item.Output, CacheRead: item.CacheRead, CacheWrite: item.CacheWrite}}, channel: item.Channel, group: item.Group, status: item.Status, cost: item.Cost, priced: true, duration: item.DurationMs})
		}
	}
	return buildUsageDashboard(query, start, end, now, records, activity), nil
}

func dashboardRange(query UsageDashboardQuery, now time.Time) (UsageDashboardQuery, time.Time, time.Time, error) {
	if query.Source == "" {
		query.Source = "local"
	}
	if query.Source != "local" && query.Source != "proxy" {
		return query, time.Time{}, time.Time{}, fmt.Errorf("不支持的用量来源")
	}
	if query.Provider != "" && query.Provider != "claude" && query.Provider != "codex" {
		return query, time.Time{}, time.Time{}, fmt.Errorf("不支持的提供商")
	}
	if query.Period == "" {
		query.Period = "30d"
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := today.AddDate(0, 0, 1)
	var start time.Time
	switch query.Period {
	case "today":
		start = today
	case "7d":
		start = today.AddDate(0, 0, -6)
	case "30d":
		start = today.AddDate(0, 0, -29)
	case "90d":
		start = today.AddDate(0, 0, -89)
	case "all":
		start = time.Date(1970, 1, 1, 0, 0, 0, 0, now.Location())
	default:
		return query, time.Time{}, time.Time{}, fmt.Errorf("不支持的统计周期")
	}
	if query.Day != "" {
		if _, err := time.Parse(time.DateOnly, query.Day); err != nil {
			return query, start, end, fmt.Errorf("无效的统计日期")
		}
	}
	query.Page = max(1, query.Page)
	if query.PageSize < 1 || query.PageSize > 100 {
		query.PageSize = 20
	}
	query.Search = strings.TrimSpace(query.Search)
	return query, start, end, nil
}

func dashboardLocalCost(call sessionusage.Call, price *pricingstore.ModelPricingRecord) (float64, bool) {
	if price == nil {
		return 0, false
	}
	input := call.Input
	if call.Provider == "codex" {
		input += call.CacheRead + call.CacheWrite
	}
	u := &tracking.TokenUsage{InputTokens: input, OutputTokens: call.Output, CacheReadTokens: call.CacheRead, CacheCreationTokens: call.CacheWrite, CacheCreation5mTokens: call.CacheWrite - call.CacheWrite1h, CacheCreation1hTokens: call.CacheWrite1h, InputIncludesCacheRead: call.Provider == "codex"}
	p := &tracking.ModelPricing{Input: price.InputPrice, Output: price.OutputPrice, CacheRead: price.CacheReadPrice, CacheCreation: price.CacheCreationPrice5m, CacheCreation1h: price.CacheCreationPrice1h, LongContextThreshold: price.LongContextThreshold, LongContextInputPrice: price.LongContextInputPrice, LongContextOutputPrice: price.LongContextOutputPrice, LongContextCacheReadPrice: price.LongContextCacheReadPrice, LongContextCacheWritePrice: price.LongContextCacheWritePrice}
	return tracking.CalculateCost(u, p, nil).TotalCost, true
}
