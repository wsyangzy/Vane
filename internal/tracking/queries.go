package tracking

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	appcache "ai-relay-forwarder/internal/cache"
)

const totalTokensSQLExpression = `CASE
	WHEN LOWER(COALESCE(provider, 'claude')) IN ('codex', 'openai') THEN
		COALESCE(input_tokens, 0) + COALESCE(output_tokens, 0) +
		CASE
			WHEN COALESCE(cache_read_tokens, 0) > COALESCE(input_tokens, 0) THEN COALESCE(cache_read_tokens, 0)
			ELSE 0
		END
	ELSE
		COALESCE(input_tokens, 0) + COALESCE(output_tokens, 0) +
		COALESCE(cache_creation_tokens, 0) + COALESCE(cache_read_tokens, 0)
END`

const distributionInputTokensSQLExpression = `CASE
	WHEN LOWER(COALESCE(provider, 'claude')) IN ('codex', 'openai') THEN
		CASE
			WHEN COALESCE(cache_read_tokens, 0) > COALESCE(input_tokens, 0) THEN COALESCE(input_tokens, 0)
			ELSE COALESCE(input_tokens, 0) - COALESCE(cache_read_tokens, 0)
		END
	ELSE
		COALESCE(input_tokens, 0)
END`

// TotalTokensSQLExpression returns the request_logs expression for display/usage total tokens.
// Codex/OpenAI input_tokens already include cached_tokens. Very old Codex rows stored only
// billable input, detected by cache_read_tokens > input_tokens and expanded for display totals.
func TotalTokensSQLExpression() string {
	return totalTokensSQLExpression
}

// DistributionInputTokensSQLExpression returns input-token buckets for token distribution charts.
// Codex/OpenAI cache reads are rendered as a separate bucket, so cached input is removed here.
func DistributionInputTokensSQLExpression() string {
	return distributionInputTokensSQLExpression
}

// DisplayInputTokens returns provider-facing input tokens for UI/statistics display.
func DisplayInputTokens(provider string, inputTokens, cacheReadTokens int64) int64 {
	if isInputCacheInclusiveProvider(provider) && cacheReadTokens > inputTokens {
		return inputTokens + cacheReadTokens
	}
	return inputTokens
}

// DisplayTotalTokens returns provider-facing total tokens for request display/statistics.
func DisplayTotalTokens(provider string, inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens int64) int64 {
	if isInputCacheInclusiveProvider(provider) {
		return DisplayInputTokens(provider, inputTokens, cacheReadTokens) + outputTokens
	}
	return inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens
}

func isInputCacheInclusiveProvider(provider string) bool {
	switch strings.ToLower(provider) {
	case "codex", "openai":
		return true
	default:
		return false
	}
}

func (ut *UsageTracker) formatStartTimeQueryBound(t time.Time) string {
	if ut != nil && ut.location != nil {
		t = t.In(ut.location)
	}
	return t.Format("2006-01-02 15:04:05")
}

func (ut *UsageTracker) formatEndTimeQueryBound(t time.Time) string {
	if ut != nil && ut.location != nil {
		t = t.In(ut.location)
	}
	// DB 中 start_time 可能被写为：
	// - "YYYY-MM-DD HH:MM:SS"
	// - "YYYY-MM-DD HH:MM:SS.ffffff"
	// - "YYYY-MM-DD HH:MM:SS.fffffffff"
	// 这里统一用一个带小数的“上界”字符串，保证 <= 查询不漏掉带小数的记录。
	return t.Format("2006-01-02 15:04:05") + ".999999999"
}

// QueryOptions represents options for querying usage data
type QueryOptions struct {
	StartDate    *time.Time
	EndDate      *time.Time
	ModelName    string
	Channel      string
	EndpointName string
	GroupName    string
	Status       string
	Provider     string // 提供商过滤: claude/codex，空字符串表示不过滤
	Limit        int
	Offset       int
}

// UsageSummary represents a summary of usage data
type UsageSummary struct {
	Date         string `json:"date"`
	ModelName    string `json:"model_name"`
	EndpointName string `json:"endpoint_name"`
	GroupName    string `json:"group_name"`
	RequestCount int    `json:"request_count"`
	SuccessCount int    `json:"success_count"`
	ErrorCount   int    `json:"error_count"`

	TotalInputTokens         int64   `json:"total_input_tokens"`
	TotalOutputTokens        int64   `json:"total_output_tokens"`
	TotalCacheCreationTokens int64   `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens     int64   `json:"total_cache_read_tokens"`
	TotalCostUSD             float64 `json:"total_cost_usd"`

	AvgDurationMs float64   `json:"avg_duration_ms"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// RequestDetail represents a detailed request record
type RequestDetail struct {
	ID        int64  `json:"id"`
	RequestID string `json:"request_id"`
	ClientIP  string `json:"client_ip"`
	UserAgent string `json:"user_agent"`
	Method    string `json:"method"`
	Path      string `json:"path"`

	StartTime    time.Time  `json:"start_time"`
	EndTime      *time.Time `json:"end_time"`
	DurationMs   *int64     `json:"duration_ms"`
	OutputSentAt *time.Time `json:"output_sent_at"`
	FirstByteMs  *int64     `json:"first_byte_ms"`

	Channel                 string `json:"channel"` // 渠道标签
	EndpointName            string `json:"endpoint_name"`
	EndpointChain           string `json:"endpoint_chain"`
	GroupName               string `json:"group_name"`
	ModelName               string `json:"model_name"`
	OriginalModelName       string `json:"original_model_name"`
	ModelRewriteSource      string `json:"model_rewrite_source"`
	ModelRewriteTarget      string `json:"model_rewrite_target"`
	PrivacyRedacted         bool   `json:"privacy_redacted"`
	PrivacyRedactionSummary string `json:"privacy_redaction_summary"`
	PrivacyRedactionMatches string `json:"privacy_redaction_matches"`
	IsStreaming             bool   `json:"is_streaming"` // 是否为流式请求
	Provider                string `json:"provider"`     // 提供商标识: claude/codex

	Status         string `json:"status"`
	HTTPStatusCode *int   `json:"http_status_code"`
	RetryCount     int    `json:"retry_count"`

	// 状态机重构新增字段 - 错误原因分离
	FailureReason     string `json:"failure_reason"`      // 失败原因类型
	LastFailureReason string `json:"last_failure_reason"` // 最后一次失败的详细信息
	CancelReason      string `json:"cancel_reason"`       // 取消原因

	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	ReasoningTokens          int64  `json:"reasoning_tokens"`
	BillableOutputTokens     int64  `json:"billable_output_tokens"`
	BillingOutputTokenSource string `json:"billing_output_token_source"`
	CacheCreationTokens      int64  `json:"cache_creation_tokens"`    // 总缓存创建（向后兼容）
	CacheCreation5mTokens    int64  `json:"cache_creation_5m_tokens"` // 5分钟缓存
	CacheCreation1hTokens    int64  `json:"cache_creation_1h_tokens"` // 1小时缓存
	CacheReadTokens          int64  `json:"cache_read_tokens"`

	InputCostUSD           float64 `json:"input_cost_usd"`
	OutputCostUSD          float64 `json:"output_cost_usd"`
	CacheCreationCostUSD   float64 `json:"cache_creation_cost_usd"`    // 总缓存创建成本（向后兼容）
	CacheCreation5mCostUSD float64 `json:"cache_creation_5m_cost_usd"` // 5分钟缓存创建成本
	CacheCreation1hCostUSD float64 `json:"cache_creation_1h_cost_usd"` // 1小时缓存创建成本
	CacheReadCostUSD       float64 `json:"cache_read_cost_usd"`
	TotalCostUSD           float64 `json:"total_cost_usd"`

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

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (d *RequestDetail) ComputeFirstByteMs() {
	if d == nil || d.OutputSentAt == nil {
		return
	}
	ms := d.OutputSentAt.Sub(d.StartTime).Milliseconds()
	if ms < 0 {
		ms = 0
	}
	d.FirstByteMs = &ms
}

// UsageStats represents aggregated usage statistics
type UsageStats struct {
	Period        string  `json:"period"`
	TotalRequests int     `json:"total_requests"`
	SuccessRate   float64 `json:"success_rate"`
	AvgDuration   float64 `json:"avg_duration_ms"`
	TotalCost     float64 `json:"total_cost_usd"`
}

// UsageStatsTotals 表示用于快速统计展示的聚合数据（总览用）。
// 与 UsageStats 不同：该结构提供“计数/累加值”，由调用方决定如何计算成功率、平均耗时等派生指标。
type UsageStatsTotals struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	TotalTokens     int64
	TotalCostUSD    float64
	DurationSumMs   int64
	DurationCount   int64
}

func (t *UsageStatsTotals) add(other UsageStatsTotals) {
	t.TotalRequests += other.TotalRequests
	t.SuccessRequests += other.SuccessRequests
	t.FailedRequests += other.FailedRequests
	t.TotalTokens += other.TotalTokens
	t.TotalCostUSD += other.TotalCostUSD
	t.DurationSumMs += other.DurationSumMs
	t.DurationCount += other.DurationCount
}

func (t UsageStatsTotals) AvgDurationMs() float64 {
	if t.DurationCount <= 0 {
		return 0
	}
	return float64(t.DurationSumMs) / float64(t.DurationCount)
}

// EndpointCostSummary represents cost summary data for an endpoint
type EndpointCostSummary struct {
	Channel      string  `json:"channel"`
	EndpointName string  `json:"endpoint_name"`
	GroupName    string  `json:"group_name"`
	Provider     string  `json:"provider"`
	TotalTokens  int64   `json:"total_tokens"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	RequestCount int     `json:"request_count"`
	SuccessCount int     `json:"success_count"`

	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`

	InputCostUSD         float64 `json:"input_cost_usd"`
	OutputCostUSD        float64 `json:"output_cost_usd"`
	CacheCreationCostUSD float64 `json:"cache_creation_cost_usd"`
	CacheReadCostUSD     float64 `json:"cache_read_cost_usd"`
}

// GetDB returns the read database connection for external queries (读写分离：返回读连接)
func (ut *UsageTracker) GetDB() *sql.DB {
	return ut.readDB
}

// GetReadDB returns the read database connection
func (ut *UsageTracker) GetReadDB() *sql.DB {
	return ut.readDB
}

// GetWriteDB returns the write database connection (仅用于特殊情况)
func (ut *UsageTracker) GetWriteDB() *sql.DB {
	return ut.writeDB
}

// QueryUsageSummary queries usage summary data
func (ut *UsageTracker) QueryUsageSummary(ctx context.Context, opts *QueryOptions) ([]UsageSummary, error) {
	if ut.readDB == nil {
		return nil, fmt.Errorf("read database not initialized")
	}

	query := `SELECT date, model_name, endpoint_name, 
		COALESCE(group_name, '') as group_name,
		request_count, success_count, error_count,
		total_input_tokens, total_output_tokens, 
		total_cache_creation_tokens, total_cache_read_tokens,
		total_cost_usd, COALESCE(avg_duration_ms, 0.0) as avg_duration_ms, created_at, updated_at
		FROM usage_summary WHERE 1=1`

	var args []interface{}

	if opts.StartDate != nil {
		query += " AND date >= ?"
		args = append(args, opts.StartDate.Format("2006-01-02"))
	}
	if opts.EndDate != nil {
		query += " AND date <= ?"
		args = append(args, opts.EndDate.Format("2006-01-02"))
	}
	if opts.ModelName != "" {
		query += " AND model_name = ?"
		args = append(args, opts.ModelName)
	}
	if opts.EndpointName != "" {
		query += " AND endpoint_name = ?"
		args = append(args, opts.EndpointName)
	}
	if opts.GroupName != "" {
		query += " AND group_name = ?"
		args = append(args, opts.GroupName)
	}
	if opts.Provider != "" {
		query += " AND provider = ?"
		args = append(args, opts.Provider)
	}

	query += " ORDER BY date DESC, total_cost_usd DESC"

	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}
	if opts.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	rows, err := ut.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query usage summary: %w", err)
	}
	defer rows.Close()

	var summaries []UsageSummary
	for rows.Next() {
		var summary UsageSummary
		err := rows.Scan(
			&summary.Date, &summary.ModelName, &summary.EndpointName, &summary.GroupName,
			&summary.RequestCount, &summary.SuccessCount, &summary.ErrorCount,
			&summary.TotalInputTokens, &summary.TotalOutputTokens,
			&summary.TotalCacheCreationTokens, &summary.TotalCacheReadTokens,
			&summary.TotalCostUSD, &summary.AvgDurationMs,
			&summary.CreatedAt, &summary.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan usage summary: %w", err)
		}
		summaries = append(summaries, summary)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating usage summary rows: %w", err)
	}

	return summaries, nil
}

// QueryRequestDetails queries detailed request records
func (ut *UsageTracker) QueryRequestDetails(ctx context.Context, opts *QueryOptions) ([]RequestDetail, error) {
	if ut.readDB == nil {
		return nil, fmt.Errorf("read database not initialized")
	}

	query := `SELECT id, request_id,
		COALESCE(client_ip, '') as client_ip,
		COALESCE(user_agent, '') as user_agent,
		method, path, start_time, end_time, duration_ms, output_sent_at,
		COALESCE(channel, '') as channel,
		COALESCE(endpoint_name, '') as endpoint_name,
		COALESCE(endpoint_chain, '') as endpoint_chain,
		COALESCE(group_name, '') as group_name,
		COALESCE(model_name, '') as model_name,
		COALESCE(original_model_name, '') as original_model_name,
		COALESCE(model_rewrite_source, '') as model_rewrite_source,
		COALESCE(model_rewrite_target, '') as model_rewrite_target,
		COALESCE(privacy_redacted, false) as privacy_redacted,
		COALESCE(privacy_redaction_summary, '') as privacy_redaction_summary,
		COALESCE(privacy_redaction_matches, '') as privacy_redaction_matches,
		COALESCE(is_streaming, false) as is_streaming,
		COALESCE(provider, 'claude') as provider,
		status, http_status_code, retry_count,
		COALESCE(failure_reason, '') as failure_reason,
		COALESCE(last_failure_reason, '') as last_failure_reason,
		COALESCE(cancel_reason, '') as cancel_reason,
		input_tokens, output_tokens,
		COALESCE(reasoning_tokens, 0) as reasoning_tokens,
		CASE
			WHEN COALESCE(billable_output_tokens, 0) > 0
				OR COALESCE(billing_output_token_source, 'output_tokens') != 'output_tokens'
			THEN COALESCE(billable_output_tokens, 0)
			ELSE output_tokens
		END as billable_output_tokens,
		COALESCE(billing_output_token_source, 'output_tokens') as billing_output_token_source,
		cache_creation_tokens, COALESCE(cache_creation_5m_tokens, 0) as cache_creation_5m_tokens, COALESCE(cache_creation_1h_tokens, 0) as cache_creation_1h_tokens,
		cache_read_tokens,
		input_cost_usd, output_cost_usd, cache_creation_cost_usd,
		COALESCE(cache_creation_5m_cost_usd, 0) as cache_creation_5m_cost_usd,
		COALESCE(cache_creation_1h_cost_usd, 0) as cache_creation_1h_cost_usd,
		cache_read_cost_usd, total_cost_usd,
		COALESCE(cost_multiplier, 1.0) as cost_multiplier,
		COALESCE(input_cost_multiplier, 1.0) as input_cost_multiplier,
		COALESCE(output_cost_multiplier, 1.0) as output_cost_multiplier,
		COALESCE(cache_creation_cost_multiplier, 1.0) as cache_creation_cost_multiplier,
		COALESCE(cache_creation_cost_multiplier_1h, 1.0) as cache_creation_cost_multiplier_1h,
		COALESCE(cache_read_cost_multiplier, 1.0) as cache_read_cost_multiplier,
		COALESCE(model_cost_multiplier, 1.0) as model_cost_multiplier,
		COALESCE(model_input_cost_multiplier, 1.0) as model_input_cost_multiplier,
		COALESCE(model_output_cost_multiplier, 1.0) as model_output_cost_multiplier,
		COALESCE(model_cache_read_cost_multiplier, 1.0) as model_cache_read_cost_multiplier,
		created_at, updated_at
		FROM request_logs WHERE 1=1`

	var args []interface{}

	if opts.StartDate != nil {
		query += " AND start_time >= ?"
		args = append(args, ut.formatStartTimeQueryBound(*opts.StartDate))
	}
	if opts.EndDate != nil {
		query += " AND start_time <= ?"
		args = append(args, ut.formatEndTimeQueryBound(*opts.EndDate))
	}
	if opts.ModelName != "" {
		query += " AND model_name = ?"
		args = append(args, opts.ModelName)
	}
	if opts.Channel != "" {
		query += " AND channel = ?"
		args = append(args, opts.Channel)
	}
	if opts.EndpointName != "" {
		query += " AND endpoint_name = ?"
		args = append(args, opts.EndpointName)
	}
	if opts.GroupName != "" {
		query += " AND group_name = ?"
		args = append(args, opts.GroupName)
	}
	if opts.Status != "" {
		// 状态机重构 - 状态与错误分离的兼容查询
		switch opts.Status {
		case "completed":
			// 已完成状态：精确匹配
			query += " AND status = ?"
			args = append(args, "completed")
		case "failed":
			// 失败状态：包含新架构的failed状态 + 旧版本的各种错误状态
			query += " AND status IN ('failed', 'partial_failed', 'error', 'auth_error', 'rate_limited', 'server_error', 'network_error', 'stream_error', 'timeout')"
		case "processing":
			// 处理中状态：精确匹配
			query += " AND status = ?"
			args = append(args, "processing")
		case "cancelled":
			// 取消状态：精确匹配
			query += " AND status = ?"
			args = append(args, "cancelled")
		case "suspended":
			// 挂起状态：精确匹配
			query += " AND status = ?"
			args = append(args, "suspended")
		case "pending":
			// 等待状态：精确匹配
			query += " AND status = ?"
			args = append(args, "pending")
		case "forwarding":
			// 转发状态：精确匹配
			query += " AND status = ?"
			args = append(args, "forwarding")
		case "retry":
			// 重试状态：精确匹配
			query += " AND status = ?"
			args = append(args, "retry")
		default:
			// 精确匹配其他状态
			query += " AND status = ?"
			args = append(args, opts.Status)
		}
	}
	if opts.Provider != "" {
		query += " AND provider = ?"
		args = append(args, opts.Provider)
	}

	query += " ORDER BY start_time DESC"

	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}
	if opts.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	rows, err := ut.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query request details: %w", err)
	}
	defer rows.Close()

	var details []RequestDetail
	for rows.Next() {
		var detail RequestDetail
		err := rows.Scan(
			&detail.ID, &detail.RequestID,
			&detail.ClientIP, &detail.UserAgent, &detail.Method, &detail.Path,
			&detail.StartTime, &detail.EndTime, &detail.DurationMs, &detail.OutputSentAt,
			&detail.Channel, &detail.EndpointName, &detail.EndpointChain, &detail.GroupName, &detail.ModelName,
			&detail.OriginalModelName, &detail.ModelRewriteSource, &detail.ModelRewriteTarget,
			&detail.PrivacyRedacted, &detail.PrivacyRedactionSummary, &detail.PrivacyRedactionMatches,
			&detail.IsStreaming,
			&detail.Provider,
			&detail.Status, &detail.HTTPStatusCode, &detail.RetryCount,
			&detail.FailureReason, &detail.LastFailureReason, &detail.CancelReason,
			&detail.InputTokens, &detail.OutputTokens,
			&detail.ReasoningTokens, &detail.BillableOutputTokens, &detail.BillingOutputTokenSource,
			&detail.CacheCreationTokens, &detail.CacheCreation5mTokens, &detail.CacheCreation1hTokens, &detail.CacheReadTokens,
			&detail.InputCostUSD, &detail.OutputCostUSD,
			&detail.CacheCreationCostUSD,
			&detail.CacheCreation5mCostUSD, &detail.CacheCreation1hCostUSD,
			&detail.CacheReadCostUSD, &detail.TotalCostUSD,
			&detail.CostMultiplier,
			&detail.InputCostMultiplier,
			&detail.OutputCostMultiplier,
			&detail.CacheCreationCostMultiplier,
			&detail.CacheCreationCostMultiplier1h,
			&detail.CacheReadCostMultiplier,
			&detail.ModelCostMultiplier,
			&detail.ModelInputCostMultiplier,
			&detail.ModelOutputCostMultiplier,
			&detail.ModelCacheReadCostMultiplier,
			&detail.CreatedAt, &detail.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan request detail: %w", err)
		}
		detail.ComputeFirstByteMs()
		details = append(details, detail)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating request detail rows: %w", err)
	}

	return details, nil
}

// QueryUsageStats queries aggregated usage statistics
func (ut *UsageTracker) QueryUsageStats(ctx context.Context, period string) (*UsageStats, error) {
	if ut.readDB == nil {
		return nil, fmt.Errorf("read database not initialized")
	}

	// Calculate date range based on period
	endDate := time.Now()
	var startDate time.Time

	switch period {
	case "1d":
		startDate = endDate.AddDate(0, 0, -1)
	case "7d":
		startDate = endDate.AddDate(0, 0, -7)
	case "30d":
		startDate = endDate.AddDate(0, 0, -30)
	case "90d":
		startDate = endDate.AddDate(0, 0, -90)
	default:
		startDate = endDate.AddDate(0, 0, -7) // default to 7 days
	}

	query := `SELECT 
		COUNT(*) as total_requests,
		CAST(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*) * 100 as success_rate,
		AVG(CASE WHEN duration_ms IS NOT NULL THEN duration_ms ELSE 0 END) as avg_duration,
		SUM(total_cost_usd) as total_cost
		FROM request_logs 
		WHERE start_time >= ? AND start_time <= ?`

	var stats UsageStats
	stats.Period = period

	err := ut.readDB.QueryRowContext(ctx, query, ut.formatStartTimeQueryBound(startDate), ut.formatEndTimeQueryBound(endDate)).Scan(
		&stats.TotalRequests,
		&stats.SuccessRate,
		&stats.AvgDuration,
		&stats.TotalCost,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query usage stats: %w", err)
	}

	// 添加调试日志
	slog.Debug("Usage stats query result",
		"total_requests", stats.TotalRequests,
		"success_rate", stats.SuccessRate,
		"period", period,
		"start_date", startDate,
		"end_date", endDate)

	return &stats, nil
}

// QueryUsageStatsTotals 查询聚合统计（数据库部分）。
// 该方法只做聚合，不返回明细，适用于前端统计卡片/图表的快速刷新。
func (ut *UsageTracker) QueryUsageStatsTotals(ctx context.Context, opts *QueryOptions) (*UsageStatsTotals, error) {
	if ut.readDB == nil {
		return nil, fmt.Errorf("read database not initialized")
	}

	query := `SELECT
		COUNT(*) as total_requests,
		COALESCE(SUM(CASE WHEN status IN ('completed', 'processing') THEN 1 ELSE 0 END), 0) as success_requests,
		COALESCE(SUM(CASE WHEN status IN ('failed', 'partial_failed', 'error', 'auth_error', 'rate_limited', 'server_error', 'network_error', 'stream_error', 'timeout') THEN 1 ELSE 0 END), 0) as failed_requests,
		COALESCE(SUM(` + totalTokensSQLExpression + `), 0) as total_tokens,
		COALESCE(SUM(total_cost_usd), 0.0) as total_cost_usd,
		COALESCE(SUM(CASE WHEN duration_ms IS NOT NULL AND duration_ms > 0 THEN duration_ms ELSE 0 END), 0) as duration_sum_ms,
		COALESCE(SUM(CASE WHEN duration_ms IS NOT NULL AND duration_ms > 0 THEN 1 ELSE 0 END), 0) as duration_count
		FROM request_logs WHERE 1=1`

	var args []interface{}

	if opts != nil {
		if opts.StartDate != nil {
			query += " AND start_time >= ?"
			args = append(args, ut.formatStartTimeQueryBound(*opts.StartDate))
		}
		if opts.EndDate != nil {
			query += " AND start_time <= ?"
			args = append(args, ut.formatEndTimeQueryBound(*opts.EndDate))
		}
		if opts.ModelName != "" {
			query += " AND model_name = ?"
			args = append(args, opts.ModelName)
		}
		if opts.Channel != "" {
			query += " AND channel = ?"
			args = append(args, opts.Channel)
		}
		if opts.EndpointName != "" {
			query += " AND endpoint_name = ?"
			args = append(args, opts.EndpointName)
		}
		if opts.GroupName != "" {
			query += " AND group_name = ?"
			args = append(args, opts.GroupName)
		}
		if opts.Status != "" {
			// 与 QueryRequestDetails 一致：failed 为兼容集合查询，其余精确匹配。
			switch opts.Status {
			case "failed":
				query += " AND status IN ('failed', 'partial_failed', 'error', 'auth_error', 'rate_limited', 'server_error', 'network_error', 'stream_error', 'timeout')"
			default:
				query += " AND status = ?"
				args = append(args, opts.Status)
			}
		}
		if opts.Provider != "" {
			query += " AND provider = ?"
			args = append(args, opts.Provider)
		}
	}

	var totals UsageStatsTotals
	if err := ut.readDB.QueryRowContext(ctx, query, args...).Scan(
		&totals.TotalRequests,
		&totals.SuccessRequests,
		&totals.FailedRequests,
		&totals.TotalTokens,
		&totals.TotalCostUSD,
		&totals.DurationSumMs,
		&totals.DurationCount,
	); err != nil {
		return nil, fmt.Errorf("failed to query usage stats totals: %w", err)
	}

	return &totals, nil
}

// ConfigureUsageStatsCache applies runtime cache options for DB usage-stat totals.
func (ut *UsageTracker) ConfigureUsageStatsCache(options appcache.Options) {
	if ut == nil {
		return
	}
	if options.Name == "" {
		options.Name = "usage:stats"
	}
	if ut.usageStatsCache == nil {
		ut.usageStatsCache = appcache.New[UsageStatsTotals](options)
		return
	}
	ut.usageStatsCache.SetOptions(options)
}

// UsageStatsCacheStats returns the cache status snapshot for DB usage-stat totals.
func (ut *UsageTracker) UsageStatsCacheStats() appcache.Stats {
	if ut == nil || ut.usageStatsCache == nil {
		return appcache.Stats{Name: "usage:stats"}
	}
	return ut.usageStatsCache.Stats()
}

// UsageStatsCache returns the managed cache instance for app-level registry wiring.
func (ut *UsageTracker) UsageStatsCache() appcache.ManagedCache {
	if ut == nil || ut.usageStatsCache == nil {
		return nil
	}
	return ut.usageStatsCache
}

// InvalidateUsageStatsCache clears cached DB usage-stat totals.
func (ut *UsageTracker) InvalidateUsageStatsCache() int {
	if ut == nil || ut.usageStatsCache == nil {
		return 0
	}
	return ut.usageStatsCache.Invalidate(appcache.InvalidateOptions{All: true})
}

// ConfigureDBCountFallbackCache applies runtime cache options for request count fallback.
func (ut *UsageTracker) ConfigureDBCountFallbackCache(options appcache.Options) {
	if ut == nil {
		return
	}
	if options.Name == "" {
		options.Name = "requests:count-fallback"
	}
	if ut.dbCountFallbackCache == nil {
		ut.dbCountFallbackCache = appcache.New[int](options)
		return
	}
	ut.dbCountFallbackCache.SetOptions(options)
}

// DBCountFallbackCache returns the managed cache instance for app-level registry wiring.
func (ut *UsageTracker) DBCountFallbackCache() appcache.ManagedCache {
	if ut == nil || ut.dbCountFallbackCache == nil {
		return nil
	}
	return ut.dbCountFallbackCache
}

// InvalidateDBCountFallbackCache clears cached DB count fallback values.
func (ut *UsageTracker) InvalidateDBCountFallbackCache() int {
	if ut == nil || ut.dbCountFallbackCache == nil {
		return 0
	}
	return ut.dbCountFallbackCache.Invalidate(appcache.InvalidateOptions{All: true})
}

func (ut *UsageTracker) queryUsageStatsTotalsCached(ctx context.Context, opts *QueryOptions) (*UsageStatsTotals, error) {
	if ut == nil || ut.usageStatsCache == nil {
		return ut.QueryUsageStatsTotals(ctx, opts)
	}

	key := "usage:stats:" + queryOptsCacheKey(opts)
	value, err := ut.usageStatsCache.GetOrLoad(key, func() (UsageStatsTotals, error) {
		totals, err := ut.QueryUsageStatsTotals(ctx, opts)
		if err != nil {
			return UsageStatsTotals{}, err
		}
		if totals == nil {
			return UsageStatsTotals{}, nil
		}
		return *totals, nil
	})
	if err != nil {
		return nil, err
	}
	totals := value
	return &totals, nil
}

// QueryUsageStatsTotalsWithHotPool 双源聚合：热池 + 数据库。
// 相比 QueryRequestDetailsWithHotPool：不拉取大量明细，避免大数据量时拖慢 UI。
func (ut *UsageTracker) QueryUsageStatsTotalsWithHotPool(ctx context.Context, opts *QueryOptions) (*UsageStatsTotals, error) {
	dbTotals, err := ut.queryUsageStatsTotalsCached(ctx, opts)
	if err != nil {
		return nil, err
	}

	// 热池部分：仅遍历活跃请求（通常数量有限），成本可控。
	hotRequests := ut.getFilteredHotPoolRequests(opts)
	if len(hotRequests) == 0 {
		return dbTotals, nil
	}

	var hotTotals UsageStatsTotals
	for _, req := range hotRequests {
		hotTotals.TotalRequests++
		switch req.Status {
		case "completed", "processing":
			hotTotals.SuccessRequests++
		case "failed", "partial_failed", "error", "auth_error", "rate_limited", "server_error", "network_error", "stream_error", "timeout":
			hotTotals.FailedRequests++
		}

		hotTotals.TotalTokens += DisplayTotalTokens(req.Provider, req.InputTokens, req.OutputTokens, req.CacheCreationTokens, req.CacheReadTokens)
		hotTotals.TotalCostUSD += req.TotalCostUSD

		if req.DurationMs != nil && *req.DurationMs > 0 {
			hotTotals.DurationSumMs += *req.DurationMs
			hotTotals.DurationCount++
		}
	}

	dbTotals.add(hotTotals)
	return dbTotals, nil
}

// CountRequestDetails returns the total count of request details matching the query options
func (ut *UsageTracker) CountRequestDetails(ctx context.Context, opts *QueryOptions) (int, error) {
	if ut.readDB == nil {
		return 0, fmt.Errorf("read database not initialized")
	}

	query := "SELECT COUNT(*) FROM request_logs WHERE 1=1"
	var args []interface{}

	if opts.StartDate != nil {
		query += " AND start_time >= ?"
		args = append(args, ut.formatStartTimeQueryBound(*opts.StartDate))
	}
	if opts.EndDate != nil {
		query += " AND start_time <= ?"
		args = append(args, ut.formatEndTimeQueryBound(*opts.EndDate))
	}
	if opts.ModelName != "" {
		query += " AND model_name = ?"
		args = append(args, opts.ModelName)
	}
	if opts.Channel != "" {
		query += " AND channel = ?"
		args = append(args, opts.Channel)
	}
	if opts.EndpointName != "" {
		query += " AND endpoint_name = ?"
		args = append(args, opts.EndpointName)
	}
	if opts.GroupName != "" {
		query += " AND group_name = ?"
		args = append(args, opts.GroupName)
	}
	if opts.Status != "" {
		// 与 QueryRequestDetails 保持一致：failed 代表一组失败/错误状态
		switch opts.Status {
		case "failed":
			query += " AND status IN ('failed', 'partial_failed', 'error', 'auth_error', 'rate_limited', 'server_error', 'network_error', 'stream_error', 'timeout')"
		default:
			query += " AND status = ?"
			args = append(args, opts.Status)
		}
	}
	if opts.Provider != "" {
		query += " AND provider = ?"
		args = append(args, opts.Provider)
	}

	var count int
	err := ut.readDB.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count request details: %w", err)
	}

	return count, nil
}

// GetEndpointCostsForDate queries endpoint cost summary data for a specific date
func (ut *UsageTracker) GetEndpointCostsForDate(ctx context.Context, date string) ([]EndpointCostSummary, error) {
	if ut.readDB == nil {
		return nil, fmt.Errorf("read database not initialized")
	}

	// Validate date format
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, fmt.Errorf("invalid date format, expected YYYY-MM-DD: %w", err)
	}

	slog.Debug("Querying endpoint costs for date", "date", date)

	// Convert date to time range for timezone-aware querying
	// Parse the date and create start/end times in local timezone
	startTime, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, fmt.Errorf("failed to parse date: %w", err)
	}

	// Use configured timezone for the date range
	location := time.Local
	if ut != nil && ut.location != nil {
		location = ut.location
	}
	startOfDay := time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 0, 0, 0, 0, location)
	endOfDay := time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 23, 59, 59, 999999999, location)

	query := `SELECT
		COALESCE(channel, '') as channel,
		COALESCE(endpoint_name, '') as endpoint_name,
		COALESCE(group_name, '') as group_name,
		COALESCE(endpoint_chain, '') as endpoint_chain,
		COALESCE(provider, 'claude') as provider,
		COALESCE(status, '') as status,
		COALESCE(` + totalTokensSQLExpression + `, 0) as total_tokens,
		COALESCE(total_cost_usd, 0.0) as total_cost_usd,
		COALESCE(input_tokens, 0) as input_tokens,
		COALESCE(output_tokens, 0) as output_tokens,
		COALESCE(cache_creation_tokens, 0) as cache_creation_tokens,
		COALESCE(cache_read_tokens, 0) as cache_read_tokens,
		COALESCE(input_cost_usd, 0.0) as input_cost_usd,
		COALESCE(output_cost_usd, 0.0) as output_cost_usd,
		COALESCE(cache_creation_cost_usd, 0.0) as cache_creation_cost_usd,
		COALESCE(cache_read_cost_usd, 0.0) as cache_read_cost_usd
		FROM request_logs
		WHERE start_time >= ? AND start_time <= ?`

	rows, err := ut.readDB.QueryContext(ctx, query, ut.formatStartTimeQueryBound(startOfDay), ut.formatEndTimeQueryBound(endOfDay))
	if err != nil {
		slog.Error("Failed to query endpoint costs", "error", err, "date", date, "start_time", startOfDay, "end_time", endOfDay)
		return nil, fmt.Errorf("failed to query endpoint costs for date %s: %w", date, err)
	}
	defer rows.Close()

	type endpointCostKey struct {
		Channel      string
		EndpointName string
		GroupName    string
		Provider     string
	}
	type endpointCostRow struct {
		Channel              string
		EndpointName         string
		GroupName            string
		EndpointChain        string
		Provider             string
		Status               string
		TotalTokens          int64
		TotalCostUSD         float64
		InputTokens          int64
		OutputTokens         int64
		CacheCreationTokens  int64
		CacheReadTokens      int64
		InputCostUSD         float64
		OutputCostUSD        float64
		CacheCreationCostUSD float64
		CacheReadCostUSD     float64
	}

	grouped := make(map[endpointCostKey]*EndpointCostSummary)
	for rows.Next() {
		var row endpointCostRow
		err := rows.Scan(
			&row.Channel, &row.EndpointName, &row.GroupName, &row.EndpointChain, &row.Provider, &row.Status,
			&row.TotalTokens, &row.TotalCostUSD,
			&row.InputTokens, &row.OutputTokens,
			&row.CacheCreationTokens, &row.CacheReadTokens,
			&row.InputCostUSD, &row.OutputCostUSD,
			&row.CacheCreationCostUSD, &row.CacheReadCostUSD,
		)
		if err != nil {
			slog.Error("Failed to scan endpoint cost row", "error", err)
			return nil, fmt.Errorf("failed to scan endpoint cost: %w", err)
		}
		groupName := row.GroupName
		if groupName == "" {
			groupName = endpointChainCredentialGroup(row.EndpointChain)
		}
		key := endpointCostKey{
			Channel:      row.Channel,
			EndpointName: row.EndpointName,
			GroupName:    groupName,
			Provider:     row.Provider,
		}
		cost := grouped[key]
		if cost == nil {
			cost = &EndpointCostSummary{
				Channel:      row.Channel,
				EndpointName: row.EndpointName,
				GroupName:    groupName,
				Provider:     row.Provider,
			}
			grouped[key] = cost
		}
		cost.TotalTokens += row.TotalTokens
		cost.TotalCostUSD += row.TotalCostUSD
		cost.RequestCount++
		if row.Status == "completed" {
			cost.SuccessCount++
		}
		cost.InputTokens += row.InputTokens
		cost.OutputTokens += row.OutputTokens
		cost.CacheCreationTokens += row.CacheCreationTokens
		cost.CacheReadTokens += row.CacheReadTokens
		cost.InputCostUSD += row.InputCostUSD
		cost.OutputCostUSD += row.OutputCostUSD
		cost.CacheCreationCostUSD += row.CacheCreationCostUSD
		cost.CacheReadCostUSD += row.CacheReadCostUSD
	}

	if err = rows.Err(); err != nil {
		slog.Error("Error iterating endpoint cost rows", "error", err)
		return nil, fmt.Errorf("error iterating endpoint cost rows: %w", err)
	}

	costs := make([]EndpointCostSummary, 0, len(grouped))
	for _, cost := range grouped {
		costs = append(costs, *cost)
	}
	sort.Slice(costs, func(i, j int) bool {
		if costs[i].TotalCostUSD != costs[j].TotalCostUSD {
			return costs[i].TotalCostUSD > costs[j].TotalCostUSD
		}
		if costs[i].Channel != costs[j].Channel {
			return costs[i].Channel < costs[j].Channel
		}
		if costs[i].EndpointName != costs[j].EndpointName {
			return costs[i].EndpointName < costs[j].EndpointName
		}
		return costs[i].GroupName < costs[j].GroupName
	})

	slog.Debug("Successfully queried endpoint costs",
		"date", date,
		"endpoint_count", len(costs),
		"total_endpoints", len(costs))

	return costs, nil
}

type endpointChainGroupNode struct {
	CredentialGroup string `json:"credential_group"`
}

func endpointChainCredentialGroup(chain string) string {
	chain = strings.TrimSpace(chain)
	if chain == "" {
		return ""
	}

	var nodes []endpointChainGroupNode
	if err := json.Unmarshal([]byte(chain), &nodes); err == nil {
		for i := len(nodes) - 1; i >= 0; i-- {
			if nodes[i].CredentialGroup != "" {
				return nodes[i].CredentialGroup
			}
		}
	}

	parts := strings.Split(chain, " -> ")
	for i := len(parts) - 1; i >= 0; i-- {
		fields := strings.Split(parts[i], "::")
		if len(fields) >= 3 && strings.TrimSpace(fields[1]) != "" {
			return strings.TrimSpace(fields[1])
		}
	}
	return ""
}
