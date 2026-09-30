// app_api_usage.go - 使用统计 API (Wails Bindings)
// 包含使用统计摘要、请求记录查询等功能

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	claudestore "ai-relay-forwarder/internal/providers/claude/store"
	"ai-relay-forwarder/internal/tracking"
	"ai-relay-forwarder/internal/utils"
)

const (
	usageDBQueryTimeout             = 8 * time.Second
	tokenUsageHeatmapHourlyMaxRange = 7 * 24 * time.Hour
)

// ============================================================
// 使用统计 API
// ============================================================

// UsageSummary 使用统计摘要
type UsageSummary struct {
	TotalRequests        int64   `json:"total_requests"`          // 运行时请求数
	AllTimeTotalRequests int64   `json:"all_time_total_requests"` // 全部历史请求数（数据库）
	TodayRequests        int64   `json:"today_requests"`          // 今日请求数（数据库）
	SuccessRequests      int64   `json:"success_requests"`
	FailedRequests       int64   `json:"failed_requests"`
	ActiveConnections    int64   `json:"active_connections"`    // 运行时活跃连接数（内存）
	AverageResponseTime  string  `json:"average_response_time"` // 运行时平均响应时间（内存）
	TotalTokens          int64   `json:"total_tokens"`          // 运行时总 token（内存，含缓存）
	TotalInputTokens     int64   `json:"total_input_tokens"`
	TotalOutputTokens    int64   `json:"total_output_tokens"`
	TotalCost            float64 `json:"total_cost"`            // 运行时成本
	TodayCost            float64 `json:"today_cost"`            // 今日成本（数据库）
	AllTimeTotalCost     float64 `json:"all_time_total_cost"`   // 全部历史成本（数据库）
	TodayTokens          int64   `json:"today_tokens"`          // 今日 tokens（数据库）
	AllTimeTotalTokens   int64   `json:"all_time_total_tokens"` // 全部历史 tokens（数据库）
}

type CredentialGroupDailyUsage struct {
	Provider     string  `json:"provider"`
	Channel      string  `json:"channel"`
	GroupName    string  `json:"group_name"`
	UsedUSD      float64 `json:"used_usd"`
	LimitUSD     float64 `json:"limit_usd"`
	RemainingUSD float64 `json:"remaining_usd"`
	Reached      bool    `json:"reached"`
	ResetAt      string  `json:"reset_at"`
}

func (a *App) GetCredentialGroupDailyUsage(provider, channel, groupName string, limitUSD float64) (CredentialGroupDailyUsage, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	channel = strings.TrimSpace(channel)
	groupName = strings.TrimSpace(groupName)
	if provider != "claude" && provider != "codex" {
		return CredentialGroupDailyUsage{}, fmt.Errorf("不支持的 provider: %s", provider)
	}
	if channel == "" || groupName == "" {
		return CredentialGroupDailyUsage{}, fmt.Errorf("渠道和凭证分组不能为空")
	}
	if limitUSD < 0 {
		return CredentialGroupDailyUsage{}, fmt.Errorf("每日成本限额不能小于 0")
	}

	a.mu.RLock()
	usageTracker := a.usageTracker
	a.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
	defer cancel()
	usedUSD, resetAt, err := usageTracker.CredentialGroupDailyCost(ctx, provider, channel, groupName)
	if err != nil {
		return CredentialGroupDailyUsage{}, fmt.Errorf("查询凭证分组当日成本失败: %w", err)
	}
	remainingUSD := 0.0
	if limitUSD > 0 {
		remainingUSD = math.Max(limitUSD-usedUSD, 0)
	}
	return CredentialGroupDailyUsage{
		Provider:     provider,
		Channel:      channel,
		GroupName:    groupName,
		UsedUSD:      usedUSD,
		LimitUSD:     limitUSD,
		RemainingUSD: remainingUSD,
		Reached:      limitUSD > 0 && usedUSD >= limitUSD,
		ResetAt:      resetAt.Format(time.RFC3339),
	}, nil
}

// GetUsageSummary 获取使用统计摘要
// 当没有传递时间参数时，返回运行时统计（从内存）+ 全部历史请求总数（从数据库）
// 当传递时间参数时，返回历史数据（从数据库）
// provider 参数预留，当前运行时内存数据无 provider 维度
func (a *App) GetUsageSummary(startTimeStr, endTimeStr, provider string) (UsageSummary, error) {
	a.mu.RLock()
	monitoringMiddleware := a.monitoringMiddleware
	usageTracker := a.usageTracker
	cfg := a.config
	logger := a.logger
	a.mu.RUnlock()

	// 如果没有传递时间参数，返回运行时统计（与 Web 版本 /api/v1/connections 一致）
	if startTimeStr == "" && endTimeStr == "" {
		a.ensureCacheRegistry()
		if monitoringMiddleware == nil {
			return UsageSummary{}, nil
		}

		// 从内存中获取运行时统计
		metrics := monitoringMiddleware.GetMetrics()
		stats := metrics.GetMetrics()

		// 计算总 Token
		totalInputTokens := stats.TotalTokenUsage.InputTokens
		totalOutputTokens := stats.TotalTokenUsage.OutputTokens
		totalTokens := stats.TotalTokenUsage.InputTokens + stats.TotalTokenUsage.OutputTokens +
			stats.TotalTokenUsage.CacheCreationTokens + stats.TotalTokenUsage.CacheReadTokens
		activeConnections := int64(len(stats.ActiveConnections))
		avgResponseTime := utils.FormatResponseTime(stats.GetAverageResponseTime())

		// 查询数据库获取全部历史和今日统计
		var (
			allTimeTotalCost   float64
			allTimeTotalTokens int64
			allTimeTotal       int64
			todayCost          float64
			todayTokens        int64
			todayRequests      int64
		)

		if usageTracker != nil {
			loc := time.Local
			if cfg != nil && cfg.Timezone != "" {
				if parsedLoc, err := time.LoadLocation(cfg.Timezone); err == nil {
					loc = parsedLoc
				}
			}
			now := time.Now().In(loc)
			todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			todayEnd := todayStart.Add(24 * time.Hour)
			cacheKey := fmt.Sprintf("runtime|provider=%s|today=%s|tz=%s", provider, todayStart.Format("2006-01-02"), loc.String())

			snapshot, err := a.usageSummaryCache.GetOrLoad(cacheKey, func() (usageSummaryDBSnapshot, error) {
				ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
				allTimeCost, allTimeTokens, allTimeRequests, err := queryStatsFromDBStrict(ctx, logger, usageTracker, time.Time{}, time.Now(), provider)
				cancel()
				if err != nil {
					return usageSummaryDBSnapshot{}, err
				}

				ctx, cancel = context.WithTimeout(context.Background(), usageDBQueryTimeout)
				dayCost, dayTokens, dayRequests, err := queryStatsFromDBStrict(ctx, logger, usageTracker, todayStart, todayEnd, provider)
				cancel()
				if err != nil {
					return usageSummaryDBSnapshot{}, err
				}
				return usageSummaryDBSnapshot{
					allTimeTotalCost:   allTimeCost,
					allTimeTotalTokens: allTimeTokens,
					allTimeTotal:       allTimeRequests,
					todayCost:          dayCost,
					todayTokens:        dayTokens,
					todayRequests:      dayRequests,
				}, nil
			})
			if err != nil {
				return UsageSummary{}, fmt.Errorf("查询历史使用统计失败: %w", err)
			}
			allTimeTotalCost = snapshot.allTimeTotalCost
			allTimeTotalTokens = snapshot.allTimeTotalTokens
			allTimeTotal = snapshot.allTimeTotal
			todayCost = snapshot.todayCost
			todayTokens = snapshot.todayTokens
			todayRequests = snapshot.todayRequests
		}

		return UsageSummary{
			TotalRequests:        stats.TotalRequests,
			AllTimeTotalRequests: allTimeTotal,
			TodayRequests:        todayRequests,
			SuccessRequests:      stats.SuccessfulRequests,
			FailedRequests:       stats.FailedRequests,
			ActiveConnections:    activeConnections,
			AverageResponseTime:  avgResponseTime,
			TotalTokens:          totalTokens,
			TotalInputTokens:     totalInputTokens,
			TotalOutputTokens:    totalOutputTokens,
			TotalCost:            0, // 运行时统计不计算成本
			TodayCost:            todayCost,
			AllTimeTotalCost:     allTimeTotalCost,
			TodayTokens:          todayTokens,
			AllTimeTotalTokens:   allTimeTotalTokens,
		}, nil
	}

	// 传递了时间参数，从数据库查询历史数据
	if usageTracker == nil {
		return UsageSummary{}, nil
	}

	// 解析时间范围
	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			startTime = time.Now().AddDate(0, 0, -7) // 默认最近7天
		}
	} else {
		startTime = time.Now().AddDate(0, 0, -7)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			endTime = time.Now()
		}
	} else {
		endTime = time.Now()
	}

	ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
	defer cancel()
	a.ensureCacheRegistry()
	cacheKey := fmt.Sprintf("history|provider=%s|start=%s|end=%s", provider, startTime.Format(time.RFC3339Nano), endTime.Format(time.RFC3339Nano))
	return a.usageHistoryCache.GetOrLoad(cacheKey, func() (UsageSummary, error) {
		summaries, err := usageTracker.QueryUsageSummary(ctx, &tracking.QueryOptions{
			StartDate: &startTime,
			EndDate:   &endTime,
			Provider:  provider,
		})
		if err != nil {
			return UsageSummary{}, err
		}

		// 聚合所有摘要
		result := UsageSummary{}
		for _, s := range summaries {
			result.TotalRequests += int64(s.RequestCount)
			result.SuccessRequests += int64(s.SuccessCount)
			result.FailedRequests += int64(s.ErrorCount)
			result.TotalInputTokens += s.TotalInputTokens
			result.TotalOutputTokens += s.TotalOutputTokens
			result.TotalCost += s.TotalCostUSD
		}
		return result, nil
	})
}

// queryStatsFromDB 直接从 request_logs 表查询成本、tokens 和请求数
func queryStatsFromDB(ctx context.Context, logger *slog.Logger, usageTracker *tracking.UsageTracker, startTime, endTime time.Time, provider string) (cost float64, tokens int64, requests int64) {
	cost, tokens, requests, _ = queryStatsFromDBStrict(ctx, logger, usageTracker, startTime, endTime, provider)
	return cost, tokens, requests
}

func queryStatsFromDBStrict(ctx context.Context, logger *slog.Logger, usageTracker *tracking.UsageTracker, startTime, endTime time.Time, provider string) (cost float64, tokens int64, requests int64, err error) {
	if usageTracker == nil {
		return 0, 0, 0, fmt.Errorf("usage tracker is nil")
	}
	opts := &tracking.QueryOptions{Provider: provider}
	if !startTime.IsZero() {
		opts.StartDate = &startTime
		opts.EndDate = &endTime
	}
	totals, err := usageTracker.QueryUsageStatsTotals(ctx, opts)
	if err != nil {
		if logger != nil {
			logger.Debug("查询统计数据失败", "error", err)
		}
		return 0, 0, 0, err
	}
	return totals.TotalCostUSD, totals.TotalTokens, totals.TotalRequests, nil
}

// RequestRecord 请求记录
type RequestRecord struct {
	ID                       string      `json:"id"`
	RequestID                string      `json:"request_id"`
	Timestamp                string      `json:"timestamp"`
	ClientIP                 string      `json:"client_ip"`
	UserAgent                string      `json:"user_agent"`
	Method                   string      `json:"method"`
	Path                     string      `json:"path"`
	Channel                  string      `json:"channel"` // 渠道标签
	Endpoint                 string      `json:"endpoint"`
	EndpointChain            []chainNode `json:"endpoint_chain"` // 端点尝试链路（结构化 JSON 数组）
	Group                    string      `json:"group"`
	Model                    string      `json:"model"`
	OriginalModelName        string      `json:"original_model_name"`
	ModelRewriteSource       string      `json:"model_rewrite_source"`
	ModelRewriteTarget       string      `json:"model_rewrite_target"`
	RouteSnapshot            string      `json:"route_snapshot,omitempty"` // 请求级路由快照 JSON，未命中路由为空
	ReasoningEffort          string      `json:"reasoning_effort"`
	ServiceTier              string      `json:"service_tier"`
	PrivacyRedacted          bool        `json:"privacy_redacted"`
	PrivacyRedactionSummary  string      `json:"privacy_redaction_summary,omitempty"`
	Provider                 string      `json:"provider"` // 提供商标识: claude/codex
	Status                   string      `json:"status"`
	StreamOutcome            string      `json:"stream_outcome,omitempty"` // 终态枚举；历史行与活跃请求为空
	HTTPStatus               int         `json:"http_status"`
	HTTPStatusIsUpstream     bool        `json:"http_status_is_upstream"`
	RetryCount               int         `json:"retry_count"`                   // 重试次数
	FailureReason            string      `json:"failure_reason,omitempty"`      // 失败原因分类
	LastFailureReason        string      `json:"last_failure_reason,omitempty"` // 最后一次失败的原始详情
	CancelReason             string      `json:"cancel_reason,omitempty"`       // 取消原因
	InputTokens              int64       `json:"input_tokens"`
	OutputTokens             int64       `json:"output_tokens"`
	ReasoningTokens          int64       `json:"reasoning_tokens"`
	BillableOutputTokens     int64       `json:"billable_output_tokens"`
	BillingOutputTokenSource string      `json:"billing_output_token_source"`
	BillingRuleSnapshot      string      `json:"billing_rule_snapshot"`
	CacheCreationTokens      int64       `json:"cache_creation_tokens"`    // 总缓存创建（向后兼容）
	CacheCreation5mTokens    int64       `json:"cache_creation_5m_tokens"` // 5分钟缓存
	CacheCreation1hTokens    int64       `json:"cache_creation_1h_tokens"` // 1小时缓存
	CacheReadTokens          int64       `json:"cache_read_tokens"`
	ResponseTime             int64       `json:"response_time"`
	FirstByteMs              *int64      `json:"first_byte_ms,omitempty"`
	IsStreaming              bool        `json:"is_streaming"`
	Cost                     float64     `json:"cost"`
	InputCostUSD             float64     `json:"input_cost_usd"`             // 输入成本（计费明细）
	OutputCostUSD            float64     `json:"output_cost_usd"`            // 输出成本（计费明细）
	CacheCreationCostUSD     float64     `json:"cache_creation_cost_usd"`    // 总缓存创建成本（向后兼容）
	CacheCreation5mCostUSD   float64     `json:"cache_creation_5m_cost_usd"` // 5分钟缓存创建成本
	CacheCreation1hCostUSD   float64     `json:"cache_creation_1h_cost_usd"` // 1小时缓存创建成本
	CacheReadCostUSD         float64     `json:"cache_read_cost_usd"`        // 缓存读取成本

	// 端点倍率快照（用于详情页计费过程展示）
	CostMultiplier                float64 `json:"cost_multiplier"`
	InputCostMultiplier           float64 `json:"input_cost_multiplier"`
	OutputCostMultiplier          float64 `json:"output_cost_multiplier"`
	CacheCreationCostMultiplier   float64 `json:"cache_creation_cost_multiplier"`
	CacheCreationCostMultiplier1h float64 `json:"cache_creation_cost_multiplier_1h"`
	CacheReadCostMultiplier       float64 `json:"cache_read_cost_multiplier"`
	ModelCostMultiplier           float64 `json:"model_cost_multiplier"`
	ModelInputCostMultiplier      float64 `json:"model_input_cost_multiplier"`
	ModelOutputCostMultiplier     float64 `json:"model_output_cost_multiplier"`
	ModelCacheReadCostMultiplier  float64 `json:"model_cache_read_cost_multiplier"`
}

// RequestListResult 请求列表结果
type RequestListResult struct {
	Requests []RequestRecord `json:"requests"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// RequestQueryParams 请求查询参数
type RequestQueryParams struct {
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
	StartDate string `json:"start_date"` // 格式：2025-12-05T00:00 或 2025-12-05T00:00:00+08:00
	EndDate   string `json:"end_date"`   // 格式：2025-12-05T23:59 或 2025-12-05T23:59:59+08:00
	Status    string `json:"status"`     // 可选：completed, failed, pending 等
	Model     string `json:"model"`      // 可选：模型名称
	Channel   string `json:"channel"`    // 可选：渠道名称
	Endpoint  string `json:"endpoint"`   // 可选：端点名称
	Group     string `json:"group"`      // 可选：组名称
	Provider  string `json:"provider"`   // 可选：提供商标识 claude/codex
}

func buildRequestQueryOptions(params RequestQueryParams, timezone string, limit, offset int) *tracking.QueryOptions {
	loc := time.Local
	if timezone != "" {
		if parsedLoc, err := time.LoadLocation(timezone); err == nil {
			loc = parsedLoc
		}
	}

	var startTime, endTime time.Time
	if params.StartDate != "" {
		if t, err := parseTimeWithLocation(params.StartDate, loc); err == nil {
			startTime = t.In(loc)
		}
	}
	if params.EndDate != "" {
		if t, err := parseTimeWithLocation(params.EndDate, loc); err == nil {
			endTime = t.In(loc)
		}
	}

	if startTime.IsZero() {
		now := time.Now().In(loc)
		startTime = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	}
	if endTime.IsZero() {
		now := time.Now().In(loc)
		endTime = time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, loc)
	}

	return &tracking.QueryOptions{
		StartDate:    &startTime,
		EndDate:      &endTime,
		ModelName:    params.Model,
		Channel:      params.Channel,
		EndpointName: params.Endpoint,
		GroupName:    params.Group,
		Status:       params.Status,
		Provider:     params.Provider,
		Limit:        limit,
		Offset:       offset,
	}
}

// GetRequests 获取请求记录列表（热池+数据库双源查询）
// 支持筛选参数：时间范围、状态、模型等
func (a *App) GetRequests(params RequestQueryParams) (RequestListResult, error) {
	a.mu.RLock()
	usageTracker := a.usageTracker
	cfg := a.config
	endpointManager := a.endpointManager
	endpointStore := a.endpointStore
	claudeCredentialGroupStore := a.claudeCredentialGroupStore
	a.mu.RUnlock()

	if usageTracker == nil {
		return RequestListResult{}, nil
	}

	page := params.Page
	pageSize := params.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize

	ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
	defer cancel()

	timezone := ""
	if cfg != nil {
		timezone = cfg.Timezone
	}
	opts := buildRequestQueryOptions(params, timezone, pageSize, offset)

	requests, total, err := usageTracker.QueryRequestDetailsWithHotPool(ctx, opts)
	if err != nil {
		return RequestListResult{}, err
	}

	result := RequestListResult{
		Requests: make([]RequestRecord, 0, len(requests)),
		Page:     page,
		PageSize: pageSize,
		Total:    int(total),
	}

	credentialGroupLookup := endpointChainCredentialGroupLookup(ctx, endpointManager, endpointStore, claudeCredentialGroupStore)
	for _, r := range requests {
		record := requestDetailToRecord(r)
		enrichEndpointChainCredentialGroups(record.Provider, record.EndpointChain, credentialGroupLookup)
		result.Requests = append(result.Requests, record)
	}

	return result, nil
}

func requestDetailToRecord(r tracking.RequestDetail) RequestRecord {
	// 使用统一的时间格式（2025-12-04 17:18:48）
	endpointChain := parseEndpointChain(r.EndpointChain)
	retryCount := r.RetryCount
	// Historical endpoint_chain nodes provide a lower bound for routing attempts, not an exact transport retry count.
	if chainRetryFloor := len(endpointChain) - 1; chainRetryFloor > retryCount {
		retryCount = chainRetryFloor
	}
	record := RequestRecord{
		RequestID:                     r.RequestID,
		Timestamp:                     r.StartTime.Format("2006-01-02 15:04:05"),
		ClientIP:                      r.ClientIP,
		UserAgent:                     r.UserAgent,
		Method:                        r.Method,
		Path:                          r.Path,
		Channel:                       r.Channel, // 渠道标签
		Endpoint:                      r.EndpointName,
		EndpointChain:                 endpointChain,
		Group:                         r.GroupName,
		Model:                         r.ModelName,
		OriginalModelName:             r.OriginalModelName,
		ModelRewriteSource:            r.ModelRewriteSource,
		ModelRewriteTarget:            r.ModelRewriteTarget,
		RouteSnapshot:                 r.RouteSnapshot,
		ReasoningEffort:               r.ReasoningEffort,
		ServiceTier:                   r.ServiceTier,
		PrivacyRedacted:               r.PrivacyRedacted,
		PrivacyRedactionSummary:       r.PrivacyRedactionSummary,
		Provider:                      r.Provider, // 提供商标识
		Status:                        r.Status,
		StreamOutcome:                 r.StreamOutcome,
		HTTPStatusIsUpstream:          r.HTTPStatusIsUpstream,
		RetryCount:                    retryCount,
		FailureReason:                 r.FailureReason,
		LastFailureReason:             r.LastFailureReason,
		CancelReason:                  r.CancelReason,
		InputTokens:                   r.InputTokens,
		OutputTokens:                  r.OutputTokens,
		ReasoningTokens:               r.ReasoningTokens,
		BillableOutputTokens:          r.BillableOutputTokens,
		BillingOutputTokenSource:      r.BillingOutputTokenSource,
		BillingRuleSnapshot:           r.BillingRuleSnapshot,
		CacheCreationTokens:           r.CacheCreationTokens,
		CacheCreation5mTokens:         r.CacheCreation5mTokens,
		CacheCreation1hTokens:         r.CacheCreation1hTokens,
		CacheReadTokens:               r.CacheReadTokens,
		IsStreaming:                   r.IsStreaming,
		Cost:                          r.TotalCostUSD,
		InputCostUSD:                  r.InputCostUSD,
		OutputCostUSD:                 r.OutputCostUSD,
		CacheCreationCostUSD:          r.CacheCreationCostUSD,
		CacheCreation5mCostUSD:        r.CacheCreation5mCostUSD,
		CacheCreation1hCostUSD:        r.CacheCreation1hCostUSD,
		CacheReadCostUSD:              r.CacheReadCostUSD,
		CostMultiplier:                r.CostMultiplier,
		InputCostMultiplier:           r.InputCostMultiplier,
		OutputCostMultiplier:          r.OutputCostMultiplier,
		CacheCreationCostMultiplier:   r.CacheCreationCostMultiplier,
		CacheCreationCostMultiplier1h: r.CacheCreationCostMultiplier1h,
		CacheReadCostMultiplier:       r.CacheReadCostMultiplier,
		ModelCostMultiplier:           r.ModelCostMultiplier,
		ModelInputCostMultiplier:      r.ModelInputCostMultiplier,
		ModelOutputCostMultiplier:     r.ModelOutputCostMultiplier,
		ModelCacheReadCostMultiplier:  r.ModelCacheReadCostMultiplier,
	}

	if r.HTTPStatusCode != nil && r.HTTPStatusIsUpstream {
		record.HTTPStatus = *r.HTTPStatusCode
	}
	if r.DurationMs != nil {
		record.ResponseTime = *r.DurationMs
	}
	if r.FirstByteMs != nil {
		firstByteMs := *r.FirstByteMs
		record.FirstByteMs = &firstByteMs
	}
	return record
}

// chainNode 链路节点（API 响应用）
type chainNode struct {
	Hop             int    `json:"hop"`
	Channel         string `json:"channel"`
	CredentialGroup string `json:"credential_group,omitempty"`
	Endpoint        string `json:"endpoint"`
	Attempts        int    `json:"attempts,omitempty"` // 本跳实际发出的请求次数；旧记录为 0
	StatusCode      int    `json:"status_code,omitempty"`
	Error           string `json:"error,omitempty"`
}

// parseEndpointChain 解析端点链路字符串（兼容三种历史格式）
//   - 新格式: [{"channel":"A","credential_group":"G","endpoint":"B"},...]
//   - 兼容格式: [{"channel":"A","endpoint":"B"},...]
//   - 中间格式: ["A::G::B","C::D"]
//   - 旧格式: "A::B -> C::D -> ..."
func parseEndpointChain(chain string) []chainNode {
	if chain == "" {
		return nil
	}
	var nodes []chainNode
	// JSON 格式
	if strings.HasPrefix(chain, "[") {
		// 尝试结构化对象数组
		if json.Unmarshal([]byte(chain), &nodes) == nil && len(nodes) > 0 {
			fillHopNumbers(nodes)
			return nodes
		}
		// 尝试字符串数组（中间格式）
		var items []string
		if json.Unmarshal([]byte(chain), &items) == nil {
			return keyStringsToNodes(items)
		}
	}
	// 旧格式 " -> " 分隔
	return keyStringsToNodes(strings.Split(chain, " -> "))
}

// fillHopNumbers 为缺失 hop 的历史数据回填跳数（1-based）
func fillHopNumbers(nodes []chainNode) {
	for i := range nodes {
		if nodes[i].Hop == 0 {
			nodes[i].Hop = i + 1
		}
	}
}

// keyStringsToNodes 将 "channel::endpoint" 或 "channel::group::endpoint" 格式字符串转为结构化节点
func keyStringsToNodes(items []string) []chainNode {
	nodes := make([]chainNode, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		hop := len(nodes) + 1
		parts := strings.Split(trimmed, "::")
		if len(parts) >= 3 && strings.TrimSpace(parts[0]) != "" {
			nodes = append(nodes, chainNode{
				Hop:             hop,
				Channel:         strings.TrimSpace(parts[0]),
				CredentialGroup: strings.TrimSpace(parts[1]),
				Endpoint:        strings.TrimSpace(strings.Join(parts[2:], "::")),
			})
		} else if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
			nodes = append(nodes, chainNode{
				Hop:      hop,
				Channel:  strings.TrimSpace(parts[0]),
				Endpoint: strings.TrimSpace(parts[1]),
			})
		} else {
			nodes = append(nodes, chainNode{Hop: hop, Endpoint: trimmed})
		}
	}
	return nodes
}

func endpointChainCredentialGroupLookup(
	ctx context.Context,
	endpointManager interface {
		GetEndpoints() []*endpoint.Endpoint
	},
	endpointStore interface {
		List(context.Context) ([]*claudestore.EndpointRecord, error)
	},
	groupStore interface {
		GetByID(context.Context, int64) (*claudestore.ClaudeCredentialGroupRecord, error)
		GetDefaultByChannel(context.Context, string) (*claudestore.ClaudeCredentialGroupRecord, error)
	},
) map[string]string {
	lookup := make(map[string]string)
	addEndpointManagerCredentialGroupLookup(lookup, endpointManager)
	addEndpointStoreCredentialGroupLookup(ctx, lookup, endpointStore, groupStore)
	return lookup
}

func addEndpointManagerCredentialGroupLookup(lookup map[string]string, endpointManager interface {
	GetEndpoints() []*endpoint.Endpoint
}) {
	if endpointManager == nil {
		return
	}
	for _, ep := range endpointManager.GetEndpoints() {
		if ep == nil {
			continue
		}
		channel := strings.TrimSpace(ep.Config.Channel)
		endpointName := strings.TrimSpace(ep.Config.Name)
		if channel == "" || endpointName == "" {
			continue
		}
		groupName := strings.TrimSpace(ep.Config.CredentialGroupName)
		if groupName == "" {
			groupName = strings.TrimSpace(ep.Config.Group)
		}
		if groupName == "" && ep.Config.CredentialGroupID > 0 {
			groupName = fmt.Sprintf("g%d", ep.Config.CredentialGroupID)
		}
		if groupName == "" {
			continue
		}

		addEndpointChainLookup(lookup, channel, endpointName, groupName)
		addEndpointChainLookup(lookup, channel, ep.Config.URL, groupName)
	}
}

func addEndpointStoreCredentialGroupLookup(
	ctx context.Context,
	lookup map[string]string,
	endpointStore interface {
		List(context.Context) ([]*claudestore.EndpointRecord, error)
	},
	groupStore interface {
		GetByID(context.Context, int64) (*claudestore.ClaudeCredentialGroupRecord, error)
		GetDefaultByChannel(context.Context, string) (*claudestore.ClaudeCredentialGroupRecord, error)
	},
) {
	if endpointStore == nil {
		return
	}
	records, err := endpointStore.List(ctx)
	if err != nil {
		return
	}
	groupByID := make(map[int64]string)
	defaultGroupByChannel := make(map[string]string)
	for _, record := range records {
		if record == nil {
			continue
		}
		channel := strings.TrimSpace(record.Channel)
		groupName := endpointRecordCredentialGroupName(ctx, record, groupStore, groupByID, defaultGroupByChannel)
		if channel == "" || groupName == "" {
			continue
		}
		addEndpointChainLookup(lookup, channel, record.Name, groupName)
		addEndpointChainLookup(lookup, channel, record.URL, groupName)
	}
}

func endpointRecordCredentialGroupName(
	ctx context.Context,
	record *claudestore.EndpointRecord,
	groupStore interface {
		GetByID(context.Context, int64) (*claudestore.ClaudeCredentialGroupRecord, error)
		GetDefaultByChannel(context.Context, string) (*claudestore.ClaudeCredentialGroupRecord, error)
	},
	groupByID map[int64]string,
	defaultGroupByChannel map[string]string,
) string {
	if record == nil {
		return ""
	}
	if groupName := strings.TrimSpace(record.CredentialGroupName); groupName != "" {
		return groupName
	}
	if groupStore != nil && record.CredentialGroupID > 0 {
		groupName, ok := groupByID[record.CredentialGroupID]
		if !ok {
			if group, err := groupStore.GetByID(ctx, record.CredentialGroupID); err == nil && group != nil {
				groupName = strings.TrimSpace(group.Name)
			}
			groupByID[record.CredentialGroupID] = groupName
		}
		if groupName != "" {
			return groupName
		}
	}
	channel := strings.TrimSpace(record.Channel)
	if groupStore != nil && channel != "" {
		groupName, ok := defaultGroupByChannel[channel]
		if !ok {
			if group, err := groupStore.GetDefaultByChannel(ctx, channel); err == nil && group != nil {
				groupName = strings.TrimSpace(group.Name)
			}
			defaultGroupByChannel[channel] = groupName
		}
		if groupName != "" {
			return groupName
		}
	}
	if record.CredentialGroupID > 0 {
		return fmt.Sprintf("g%d", record.CredentialGroupID)
	}
	return ""
}

func addEndpointChainLookup(lookup map[string]string, channel, endpointName, groupName string) {
	key := endpointChainLookupKey(channel, endpointName)
	if key == "" {
		return
	}
	if existing, ok := lookup[key]; ok && existing != groupName {
		lookup[key] = ""
		return
	}
	lookup[key] = groupName
}

func enrichEndpointChainCredentialGroups(provider string, nodes []chainNode, lookup map[string]string) {
	if len(nodes) == 0 || len(lookup) == 0 || provider == "codex" {
		return
	}
	for i := range nodes {
		if strings.TrimSpace(nodes[i].CredentialGroup) != "" {
			continue
		}
		groupName := lookup[endpointChainLookupKey(nodes[i].Channel, nodes[i].Endpoint)]
		if groupName == "" {
			continue
		}
		nodes[i].CredentialGroup = groupName
	}
}

func endpointChainLookupKey(channel, endpointName string) string {
	channel = strings.TrimSpace(channel)
	endpointName = strings.TrimSpace(endpointName)
	if channel == "" || endpointName == "" {
		return ""
	}
	return channel + "\x00" + endpointName
}

// ============================================================
// 使用统计 API (与 HTTP API 格式一致)
// ============================================================

// UsageStatsData 使用统计数据（与 HTTP API /api/v1/usage/stats 格式一致）
type UsageStatsData struct {
	Period        string  `json:"period"`
	TotalRequests int     `json:"total_requests"`
	SuccessRate   float64 `json:"success_rate"`
	AvgDurationMs float64 `json:"avg_duration_ms"`
	TotalCostUSD  float64 `json:"total_cost_usd"`
	TotalTokens   int64   `json:"total_tokens"`
	FailedCount   int     `json:"failed_requests"`
	// CacheHitRate token 加权缓存命中率（0~1）；没有输入用量时为 null，前端显示 "-"
	CacheHitRate *float64 `json:"cache_hit_rate"`
}

// UsageStatsQueryParams 使用统计查询参数
type UsageStatsQueryParams struct {
	Period    string `json:"period"`     // 时间周期: "1h", "1d", "7d", "30d", "90d"
	StartDate string `json:"start_date"` // 开始时间（优先于 period）
	EndDate   string `json:"end_date"`   // 结束时间（优先于 period）
	Status    string `json:"status"`     // 可选：状态筛选
	Model     string `json:"model"`      // 可选：模型筛选
	Channel   string `json:"channel"`    // 可选：渠道筛选
	Endpoint  string `json:"endpoint"`   // 可选：端点筛选
	Group     string `json:"group"`      // 可选：组筛选
	Provider  string `json:"provider"`   // 可选：提供商标识 claude/codex
}

// GetUsageStats 获取使用统计（与 HTTP API 格式一致）
// 支持完整筛选参数，与前端 buildQueryParams() 配合
func (a *App) GetUsageStats(params UsageStatsQueryParams) (UsageStatsData, error) {
	a.mu.RLock()
	usageTracker := a.usageTracker
	cfg := a.config
	a.mu.RUnlock()

	period := params.Period
	if period == "" {
		period = "30d"
	}

	result := UsageStatsData{
		Period: period,
	}

	// 解析时间参数（使用配置的时区）
	loc := time.Local
	if cfg != nil && cfg.Timezone != "" {
		if l, err := time.LoadLocation(cfg.Timezone); err == nil {
			loc = l
		}
	}

	var startTime, endTime time.Time
	var useCustomRange bool

	// 优先使用自定义时间范围
	if params.StartDate != "" {
		if t, err := parseTimeWithLocation(params.StartDate, loc); err == nil {
			startTime = t
			useCustomRange = true
		}
	}
	if params.EndDate != "" {
		if t, err := parseTimeWithLocation(params.EndDate, loc); err == nil {
			endTime = t
			useCustomRange = true
		}
	}

	// 如果没有自定义时间，使用 period 计算
	if !useCustomRange {
		endTime = time.Now()
		switch period {
		case "1h":
			startTime = endTime.Add(-1 * time.Hour)
		case "1d":
			startTime = endTime.AddDate(0, 0, -1)
		case "7d":
			startTime = endTime.AddDate(0, 0, -7)
		case "30d":
			startTime = endTime.AddDate(0, 0, -30)
		case "90d":
			startTime = endTime.AddDate(0, 0, -90)
		default:
			startTime = endTime.AddDate(0, 0, -30)
			result.Period = "30d"
		}
	}

	// 如果有 usageTracker，从热池+数据库组合查询（与 HTTP API 一致）
	if usageTracker != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 使用聚合查询：热池 + 数据库双源统计（避免拉取海量明细导致 UI 卡顿）
		opts := &tracking.QueryOptions{
			StartDate:    &startTime,
			EndDate:      &endTime,
			ModelName:    params.Model,
			Channel:      params.Channel, // 渠道筛选
			EndpointName: params.Endpoint,
			GroupName:    params.Group,
			Status:       params.Status,
			Provider:     params.Provider, // 提供商筛选
			Limit:        0,
			Offset:       0,
		}

		totals, err := usageTracker.QueryUsageStatsTotalsWithHotPool(ctx, opts)
		if err != nil {
			return result, err
		}
		if totals != nil {
			result.TotalRequests = int(totals.TotalRequests)
			result.FailedCount = int(totals.FailedRequests)
			result.TotalTokens = totals.TotalTokens
			result.TotalCostUSD = totals.TotalCostUSD
			result.AvgDurationMs = totals.AvgDurationMs()
			result.CacheHitRate = totals.CacheHitRate()

			if totals.TotalRequests > 0 {
				result.SuccessRate = float64(totals.SuccessRequests) / float64(totals.TotalRequests) * 100
			}

			return result, nil
		}
	}

	// 从运行时 Metrics 获取统计（降级方案，或无 usageTracker 时）
	return a.getUsageStatsFromRuntime(period), nil
}

// getUsageStatsFromRuntime 从运行时内存获取统计数据
func (a *App) getUsageStatsFromRuntime(period string) UsageStatsData {
	result := UsageStatsData{
		Period: period,
	}

	if a.monitoringMiddleware == nil {
		return result
	}

	metrics := a.monitoringMiddleware.GetMetrics()
	if metrics == nil {
		return result
	}

	stats := metrics.GetMetrics()

	// 从运行时统计计算
	result.TotalRequests = int(stats.TotalRequests)
	result.FailedCount = int(stats.FailedRequests)

	// 计算成功率
	if stats.TotalRequests > 0 {
		result.SuccessRate = float64(stats.SuccessfulRequests) / float64(stats.TotalRequests) * 100
	}

	// 计算总 Token
	result.TotalTokens = stats.TotalTokenUsage.InputTokens + stats.TotalTokenUsage.OutputTokens +
		stats.TotalTokenUsage.CacheCreationTokens + stats.TotalTokenUsage.CacheReadTokens

	// 计算平均耗时（使用 Metrics 提供的方法）
	avgResponseTime := metrics.GetAverageResponseTime()
	result.AvgDurationMs = float64(avgResponseTime.Milliseconds())

	// 运行时不计算成本
	result.TotalCostUSD = 0

	return result
}

// ============================================================
// Token 使用统计 API
// ============================================================

// TokenUsageData Token 使用数据结构
type TokenUsageData struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

// TokenUsageHeatmapDay Token 使用热力图单日数据
type TokenUsageHeatmapDay struct {
	Date                string  `json:"date"`
	Bucket              string  `json:"bucket"`
	RequestCount        int64   `json:"request_count"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	Intensity           float64 `json:"intensity"`
}

// GetTokenUsage 获取今日 Token 使用统计（从数据库查询，覆盖 Claude 和 Codex）
// provider 参数过滤：空字符串 = 全部，"claude" / "codex" = 按 provider 过滤
func (a *App) GetTokenUsage(provider string) (TokenUsageData, error) {
	a.mu.RLock()
	usageTracker := a.usageTracker
	cfg := a.config
	a.mu.RUnlock()

	if usageTracker == nil {
		return TokenUsageData{}, nil
	}

	loc := time.Local
	if cfg != nil && cfg.Timezone != "" {
		if parsedLoc, err := time.LoadLocation(cfg.Timezone); err == nil {
			loc = parsedLoc
		}
	}
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	tomorrowStart := todayStart.Add(24 * time.Hour)
	a.ensureCacheRegistry()
	cacheKey := fmt.Sprintf("provider=%s|date=%s|tz=%s", provider, todayStart.Format("2006-01-02"), loc.String())
	data, err := a.tokenUsageCache.GetOrLoad(cacheKey, func() (TokenUsageData, error) {
		return queryTokenUsageForRange(usageTracker, todayStart, tomorrowStart, provider)
	})
	if err != nil {
		return TokenUsageData{}, err
	}
	return data, nil
}

// GetTokenUsageHeatmap 按请求追踪筛选条件聚合每日 Token 使用分布
func (a *App) GetTokenUsageHeatmap(params RequestQueryParams) ([]TokenUsageHeatmapDay, error) {
	a.mu.RLock()
	usageTracker := a.usageTracker
	cfg := a.config
	a.mu.RUnlock()

	if usageTracker == nil {
		return []TokenUsageHeatmapDay{}, nil
	}

	timezone := ""
	if cfg != nil {
		timezone = cfg.Timezone
	}
	data, err := queryTokenUsageHeatmap(usageTracker, buildRequestQueryOptions(params, timezone, 0, 0))
	if err != nil {
		return nil, err
	}
	return data, nil
}

func queryTokenUsageForRange(usageTracker *tracking.UsageTracker, startTime, endTime time.Time, provider string) (TokenUsageData, error) {
	if usageTracker == nil {
		return TokenUsageData{}, fmt.Errorf("usage tracker is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
	defer cancel()
	totals, err := usageTracker.QueryTokenUsageTotals(ctx, startTime, endTime, provider)
	if err != nil {
		return TokenUsageData{}, err
	}
	return TokenUsageData{
		InputTokens:         totals.InputTokens,
		OutputTokens:        totals.OutputTokens,
		CacheCreationTokens: totals.CacheCreationTokens,
		CacheReadTokens:     totals.CacheReadTokens,
		TotalTokens:         totals.TotalTokens,
	}, nil
}

func queryTokenUsageHeatmap(usageTracker *tracking.UsageTracker, opts *tracking.QueryOptions) ([]TokenUsageHeatmapDay, error) {
	if usageTracker == nil {
		return nil, fmt.Errorf("usage tracker is nil")
	}
	if opts == nil || opts.StartDate == nil || opts.EndDate == nil {
		return []TokenUsageHeatmapDay{}, nil
	}
	startTime := *opts.StartDate
	endTime := *opts.EndDate
	if endTime.Before(startTime) {
		return []TokenUsageHeatmapDay{}, nil
	}

	bucket := "day"
	if endTime.Sub(startTime) <= tokenUsageHeatmapHourlyMaxRange {
		bucket = "hour"
	}

	ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
	defer cancel()
	buckets, err := usageTracker.QueryTokenUsageBuckets(ctx, opts, bucket)
	if err != nil {
		return nil, err
	}

	byBucket := make(map[string]TokenUsageHeatmapDay)
	var maxTokens int64
	for _, value := range buckets {
		item := TokenUsageHeatmapDay{
			Date:                value.Key,
			RequestCount:        value.RequestCount,
			InputTokens:         value.InputTokens,
			OutputTokens:        value.OutputTokens,
			CacheCreationTokens: value.CacheCreationTokens,
			CacheReadTokens:     value.CacheReadTokens,
			TotalTokens:         value.TotalTokens,
		}
		item.Bucket = bucket
		if item.TotalTokens > maxTokens {
			maxTokens = item.TotalTokens
		}
		byBucket[item.Date] = item
	}

	if bucket == "hour" {
		rangeStart := time.Date(startTime.Year(), startTime.Month(), startTime.Day(), startTime.Hour(), 0, 0, 0, startTime.Location())
		rangeEnd := time.Date(endTime.Year(), endTime.Month(), endTime.Day(), endTime.Hour(), 0, 0, 0, endTime.Location()).Add(time.Hour)
		hours := make([]TokenUsageHeatmapDay, 0, int(rangeEnd.Sub(rangeStart).Hours())+1)
		for cursor := rangeStart; cursor.Before(rangeEnd); cursor = cursor.Add(time.Hour) {
			key := cursor.Format("2006-01-02 15")
			item := byBucket[key]
			item.Date = key
			item.Bucket = bucket
			if maxTokens > 0 && item.TotalTokens > 0 {
				item.Intensity = float64(item.TotalTokens) / float64(maxTokens)
			}
			hours = append(hours, item)
		}
		return hours, nil
	}

	rangeStart := time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 0, 0, 0, 0, startTime.Location())
	rangeEnd := time.Date(endTime.Year(), endTime.Month(), endTime.Day(), 0, 0, 0, 0, endTime.Location()).AddDate(0, 0, 1)
	days := make([]TokenUsageHeatmapDay, 0, int(rangeEnd.Sub(rangeStart).Hours()/24)+1)
	for cursor := rangeStart; cursor.Before(rangeEnd); cursor = cursor.AddDate(0, 0, 1) {
		date := cursor.Format("2006-01-02")
		day := byBucket[date]
		day.Date = date
		day.Bucket = bucket
		if maxTokens > 0 && day.TotalTokens > 0 {
			day.Intensity = float64(day.TotalTokens) / float64(maxTokens)
		}
		days = append(days, day)
	}
	return days, nil
}
