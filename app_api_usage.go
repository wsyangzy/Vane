// app_api_usage.go - 使用统计 API (Wails Bindings)
// 包含使用统计摘要、请求记录查询等功能

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"ai-relay-forwarder/internal/tracking"
	"ai-relay-forwarder/internal/utils"
)

const usageDBQueryTimeout = 8 * time.Second
const usageDBStatsCacheTTL = 30 * time.Second

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

type usageDBStatsCache struct {
	mu sync.Mutex

	updatedAt          time.Time
	allTimeTotalCost   float64
	allTimeTotalTokens int64
	allTimeTotal       int64
	todayCost          float64
	todayTokens        int64
	todayRequests      int64
}

var globalUsageDBStatsCache usageDBStatsCache

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
			// DB 统计做缓存，避免前端轮询/事件推送时频繁查询导致阻塞
			// 仅对无 provider 过滤的全量查询使用缓存；按 provider 过滤时直接查询
			useCache := provider == ""
			if useCache {
				cache := &globalUsageDBStatsCache
				cache.mu.Lock()
				cacheFresh := !cache.updatedAt.IsZero() && time.Since(cache.updatedAt) < usageDBStatsCacheTTL
				if cacheFresh {
					allTimeTotalCost = cache.allTimeTotalCost
					allTimeTotalTokens = cache.allTimeTotalTokens
					allTimeTotal = cache.allTimeTotal
					todayCost = cache.todayCost
					todayTokens = cache.todayTokens
					todayRequests = cache.todayRequests
				}
				cache.mu.Unlock()

				if !cacheFresh {
					loc := time.Local
					if cfg != nil && cfg.Timezone != "" {
						if parsedLoc, err := time.LoadLocation(cfg.Timezone); err == nil {
							loc = parsedLoc
						}
					}

					ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
					allTimeTotalCost, allTimeTotalTokens, allTimeTotal = queryStatsFromDB(ctx, logger, usageTracker, time.Time{}, time.Now(), "")
					cancel()

					now := time.Now().In(loc)
					todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
					todayEnd := todayStart.Add(24 * time.Hour)
					ctx, cancel = context.WithTimeout(context.Background(), usageDBQueryTimeout)
					todayCost, todayTokens, todayRequests = queryStatsFromDB(ctx, logger, usageTracker, todayStart, todayEnd, "")
					cancel()

					cache := &globalUsageDBStatsCache
					cache.mu.Lock()
					cache.updatedAt = time.Now()
					cache.allTimeTotalCost = allTimeTotalCost
					cache.allTimeTotalTokens = allTimeTotalTokens
					cache.allTimeTotal = allTimeTotal
					cache.todayCost = todayCost
					cache.todayTokens = todayTokens
					cache.todayRequests = todayRequests
					cache.mu.Unlock()
				}
			} else {
				// 按 provider 过滤，不使用缓存
				loc := time.Local
				if cfg != nil && cfg.Timezone != "" {
					if parsedLoc, err := time.LoadLocation(cfg.Timezone); err == nil {
						loc = parsedLoc
					}
				}

				ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
				allTimeTotalCost, allTimeTotalTokens, allTimeTotal = queryStatsFromDB(ctx, logger, usageTracker, time.Time{}, time.Now(), provider)
				cancel()

				now := time.Now().In(loc)
				todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
				todayEnd := todayStart.Add(24 * time.Hour)
				ctx, cancel = context.WithTimeout(context.Background(), usageDBQueryTimeout)
				todayCost, todayTokens, todayRequests = queryStatsFromDB(ctx, logger, usageTracker, todayStart, todayEnd, provider)
				cancel()
			}
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
	summaries, err := usageTracker.GetUsageSummary(ctx, startTime, endTime)
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
}

// queryStatsFromDB 直接从 request_logs 表查询成本、tokens 和请求数
func queryStatsFromDB(ctx context.Context, logger *slog.Logger, usageTracker *tracking.UsageTracker, startTime, endTime time.Time, provider string) (cost float64, tokens int64, requests int64) {
	if usageTracker == nil {
		return 0, 0, 0
	}

	db := usageTracker.GetDB()
	if db == nil {
		return 0, 0, 0
	}

	var query string
	var args []any
	totalTokensExpr := tracking.TotalTokensSQLExpression()

	if startTime.IsZero() {
		query = "SELECT COALESCE(SUM(total_cost_usd), 0), COALESCE(SUM(" + totalTokensExpr + "), 0), COUNT(*) FROM request_logs"
		if provider != "" {
			query += " WHERE provider = ?"
			args = append(args, provider)
		}
	} else {
		query = "SELECT COALESCE(SUM(total_cost_usd), 0), COALESCE(SUM(" + totalTokensExpr + "), 0), COUNT(*) FROM request_logs WHERE start_time >= ? AND start_time < ?"
		args = append(args, startTime, endTime)
		if provider != "" {
			query += " AND provider = ?"
			args = append(args, provider)
		}
	}

	err := db.QueryRowContext(ctx, query, args...).Scan(&cost, &tokens, &requests)
	if err != nil {
		if logger != nil {
			logger.Debug("查询统计数据失败", "error", err)
		}
		return 0, 0, 0
	}

	return cost, tokens, requests
}

// RequestRecord 请求记录
type RequestRecord struct {
	ID                       string      `json:"id"`
	RequestID                string      `json:"request_id"`
	Timestamp                string      `json:"timestamp"`
	Channel                  string      `json:"channel"` // 渠道标签
	Endpoint                 string      `json:"endpoint"`
	EndpointChain            []chainNode `json:"endpoint_chain"` // 端点尝试链路（结构化 JSON 数组）
	Group                    string      `json:"group"`
	Model                    string      `json:"model"`
	OriginalModelName        string      `json:"original_model_name"`
	ModelRewriteSource       string      `json:"model_rewrite_source"`
	ModelRewriteTarget       string      `json:"model_rewrite_target"`
	Provider                 string      `json:"provider"` // 提供商标识: claude/codex
	Status                   string      `json:"status"`
	HTTPStatus               int         `json:"http_status"`
	RetryCount               int         `json:"retry_count"`              // 重试次数
	FailureReason            string      `json:"failure_reason,omitempty"` // 失败原因
	CancelReason             string      `json:"cancel_reason,omitempty"`  // 取消原因
	InputTokens              int64       `json:"input_tokens"`
	OutputTokens             int64       `json:"output_tokens"`
	ReasoningTokens          int64       `json:"reasoning_tokens"`
	BillableOutputTokens     int64       `json:"billable_output_tokens"`
	BillingOutputTokenSource string      `json:"billing_output_token_source"`
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

// GetRequests 获取请求记录列表（热池+数据库双源查询）
// 支持筛选参数：时间范围、状态、模型等
func (a *App) GetRequests(params RequestQueryParams) (RequestListResult, error) {
	a.mu.RLock()
	usageTracker := a.usageTracker
	cfg := a.config
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

	// 解析时间参数（使用配置的时区）
	loc := time.Local
	if cfg != nil && cfg.Timezone != "" {
		if l, err := time.LoadLocation(cfg.Timezone); err == nil {
			loc = l
		}
	}

	var startTime, endTime time.Time

	// 解析开始时间
	if params.StartDate != "" {
		if t, err := parseTimeWithLocation(params.StartDate, loc); err == nil {
			startTime = t
		}
	}
	// 解析结束时间
	if params.EndDate != "" {
		if t, err := parseTimeWithLocation(params.EndDate, loc); err == nil {
			endTime = t
		}
	}

	// 如果没有传时间参数，默认查询今天
	if startTime.IsZero() {
		now := time.Now().In(loc)
		startTime = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	}
	if endTime.IsZero() {
		now := time.Now().In(loc)
		endTime = time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, loc)
	}

	// 使用热池+数据库双源查询（与 HTTP API 一致）
	opts := &tracking.QueryOptions{
		StartDate:    &startTime,
		EndDate:      &endTime,
		ModelName:    params.Model,
		Channel:      params.Channel, // 渠道筛选
		EndpointName: params.Endpoint,
		GroupName:    params.Group,
		Status:       params.Status,
		Provider:     params.Provider, // 提供商筛选
		Limit:        pageSize,
		Offset:       offset,
	}

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

	for _, r := range requests {
		// 使用统一的时间格式（2025-12-04 17:18:48）
		// 数据库存储的就是配置时区的时间，直接格式化，不做时区转换
		chainItems := parseEndpointChain(r.EndpointChain)

		record := RequestRecord{
			RequestID:                     r.RequestID,
			Timestamp:                     r.StartTime.Format("2006-01-02 15:04:05"),
			Channel:                       r.Channel, // 渠道标签
			Endpoint:                      r.EndpointName,
			EndpointChain:                 chainItems,
			Group:                         r.GroupName,
			Model:                         r.ModelName,
			OriginalModelName:             r.OriginalModelName,
			ModelRewriteSource:            r.ModelRewriteSource,
			ModelRewriteTarget:            r.ModelRewriteTarget,
			Provider:                      r.Provider, // 提供商标识
			Status:                        r.Status,
			RetryCount:                    r.RetryCount,
			FailureReason:                 r.FailureReason,
			CancelReason:                  r.CancelReason,
			InputTokens:                   r.InputTokens,
			OutputTokens:                  r.OutputTokens,
			ReasoningTokens:               r.ReasoningTokens,
			BillableOutputTokens:          r.BillableOutputTokens,
			BillingOutputTokenSource:      r.BillingOutputTokenSource,
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

		// 处理指针字段
		if r.HTTPStatusCode != nil {
			record.HTTPStatus = *r.HTTPStatusCode
		}
		if r.DurationMs != nil {
			record.ResponseTime = *r.DurationMs
		}
		if r.FirstByteMs != nil {
			firstByteMs := *r.FirstByteMs
			record.FirstByteMs = &firstByteMs
		}

		result.Requests = append(result.Requests, record)
	}

	return result, nil
}

// chainNode 链路节点（API 响应用）
type chainNode struct {
	Hop      int    `json:"hop"`
	Channel  string `json:"channel"`
	Endpoint string `json:"endpoint"`
}

// parseEndpointChain 解析端点链路字符串（兼容三种历史格式）
//   - 新格式: [{"channel":"A","endpoint":"B"},...]
//   - 中间格式: ["A::B","C::D"]
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

// keyStringsToNodes 将 "channel::endpoint" 格式字符串转为结构化节点
func keyStringsToNodes(items []string) []chainNode {
	nodes := make([]chainNode, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		hop := len(nodes) + 1
		if idx := strings.Index(trimmed, "::"); idx > 0 {
			nodes = append(nodes, chainNode{Hop: hop, Channel: trimmed[:idx], Endpoint: trimmed[idx+2:]})
		} else {
			nodes = append(nodes, chainNode{Hop: hop, Endpoint: trimmed})
		}
	}
	return nodes
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
		if err == nil && totals != nil {
			result.TotalRequests = int(totals.TotalRequests)
			result.FailedCount = int(totals.FailedRequests)
			result.TotalTokens = totals.TotalTokens
			result.TotalCostUSD = totals.TotalCostUSD
			result.AvgDurationMs = totals.AvgDurationMs()

			if totals.TotalRequests > 0 {
				result.SuccessRate = float64(totals.SuccessRequests) / float64(totals.TotalRequests) * 100
			}

			return result, nil
		}
		// 查询失败时，继续使用运行时统计（降级方案）
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

// GetTokenUsage 获取今日 Token 使用统计（从数据库查询，覆盖 Claude 和 Codex）
// provider 参数过滤：空字符串 = 全部，"claude" / "codex" = 按 provider 过滤
func (a *App) GetTokenUsage(provider string) TokenUsageData {
	a.mu.RLock()
	usageTracker := a.usageTracker
	cfg := a.config
	a.mu.RUnlock()

	if usageTracker == nil {
		return TokenUsageData{}
	}

	db := usageTracker.GetDB()
	if db == nil {
		return TokenUsageData{}
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

	query := `SELECT
		COALESCE(SUM(` + tracking.DistributionInputTokensSQLExpression() + `), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(cache_creation_tokens), 0),
		COALESCE(SUM(cache_read_tokens), 0),
		COALESCE(SUM(` + tracking.TotalTokensSQLExpression() + `), 0)
		FROM request_logs
		WHERE start_time >= ? AND start_time < ?`
	args := []any{
		todayStart.Format("2006-01-02 15:04:05"),
		tomorrowStart.Format("2006-01-02 15:04:05"),
	}
	if provider != "" {
		query += " AND provider = ?"
		args = append(args, provider)
	}

	ctx, cancel := context.WithTimeout(context.Background(), usageDBQueryTimeout)
	defer cancel()

	var input, output, cacheCreation, cacheRead, total int64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&input, &output, &cacheCreation, &cacheRead, &total); err != nil {
		// 失败时返回零值（前端会显示"暂无数据"，但不阻塞）
		return TokenUsageData{}
	}

	return TokenUsageData{
		InputTokens:         input,
		OutputTokens:        output,
		CacheCreationTokens: cacheCreation,
		CacheReadTokens:     cacheRead,
		TotalTokens:         total,
	}
}
