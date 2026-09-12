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
	Environment  string // 运行环境过滤: dev/release；空字符串由 UsageTracker 自动填充
	Limit        int
	Offset       int
}

func (ut *UsageTracker) scopedQueryOptions(opts *QueryOptions) *QueryOptions {
	if opts == nil {
		opts = &QueryOptions{}
	}
	scoped := *opts
	if scoped.Environment == "" {
		scoped.Environment = normalizeTrackingEnvironment(ut.environment)
	}
	return &scoped
}

func queryEnvironment(opts *QueryOptions) string {
	if opts == nil {
		return "release"
	}
	return normalizeTrackingEnvironment(opts.Environment)
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
	ReasoningEffort         string `json:"reasoning_effort"`
	ServiceTier             string `json:"service_tier"`
	PrivacyRedacted         bool   `json:"privacy_redacted"`
	PrivacyRedactionSummary string `json:"privacy_redaction_summary"`
	IsStreaming             bool   `json:"is_streaming"` // 是否为流式请求
	Provider                string `json:"provider"`     // 提供商标识: claude/codex

	Status               string `json:"status"`
	HTTPStatusCode       *int   `json:"http_status_code"`
	HTTPStatusIsUpstream bool   `json:"http_status_is_upstream"`
	RetryCount           int    `json:"retry_count"`

	// 状态机重构新增字段 - 错误原因分离
	FailureReason     string `json:"failure_reason"`      // 失败原因类型
	LastFailureReason string `json:"last_failure_reason"` // 最后一次失败的详细信息
	CancelReason      string `json:"cancel_reason"`       // 取消原因

	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	ReasoningTokens          int64  `json:"reasoning_tokens"`
	BillableOutputTokens     int64  `json:"billable_output_tokens"`
	BillingOutputTokenSource string `json:"billing_output_token_source"`
	BillingRuleSnapshot      string `json:"billing_rule_snapshot"`
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

type TokenUsageTotals struct {
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	TotalTokens         int64
}

type TokenUsageBucket struct {
	Key                 string
	RequestCount        int64
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	TotalTokens         int64
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
	opts = ut.scopedQueryOptions(opts)
	sourceOpts := *opts
	sourceOpts.Offset = 0
	if opts.Limit > 0 {
		sourceOpts.Limit = max(opts.Offset, 0) + opts.Limit
	}

	var summaries []UsageSummary
	err := ut.forEachHistorySource(ctx, opts.StartDate, opts.EndDate, func(source historyQuerySource) error {
		sourceSummaries, err := queryUsageSummaryFromSource(ctx, source.db, &sourceOpts)
		if err != nil {
			return err
		}
		summaries = append(summaries, sourceSummaries...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Date != summaries[j].Date {
			return summaries[i].Date > summaries[j].Date
		}
		if summaries[i].TotalCostUSD != summaries[j].TotalCostUSD {
			return summaries[i].TotalCostUSD > summaries[j].TotalCostUSD
		}
		if summaries[i].ModelName != summaries[j].ModelName {
			return summaries[i].ModelName < summaries[j].ModelName
		}
		if summaries[i].EndpointName != summaries[j].EndpointName {
			return summaries[i].EndpointName < summaries[j].EndpointName
		}
		return summaries[i].GroupName < summaries[j].GroupName
	})

	start := max(opts.Offset, 0)
	if start >= len(summaries) {
		return []UsageSummary{}, nil
	}
	end := len(summaries)
	if opts.Limit > 0 && start+opts.Limit < end {
		end = start + opts.Limit
	}
	return summaries[start:end], nil
}

func queryUsageSummaryFromSource(ctx context.Context, db *sql.DB, opts *QueryOptions) ([]UsageSummary, error) {
	if db == nil {
		return nil, fmt.Errorf("read database not initialized")
	}

	query := `SELECT date, model_name, endpoint_name, 
		COALESCE(group_name, '') as group_name,
		request_count, success_count, error_count,
		total_input_tokens, total_output_tokens, 
		total_cache_creation_tokens, total_cache_read_tokens,
		total_cost_usd, COALESCE(avg_duration_ms, 0.0) as avg_duration_ms, created_at, updated_at
		FROM usage_summary WHERE 1=1
			AND COALESCE(environment, 'release') = ?`

	args := []interface{}{queryEnvironment(opts)}

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

	rows, err := db.QueryContext(ctx, query, args...)
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
	opts = ut.scopedQueryOptions(opts)
	sourceOpts := *opts
	sourceOpts.Offset = 0
	if opts.Limit > 0 {
		sourceOpts.Limit = max(opts.Offset, 0) + opts.Limit
	}

	capacity := max(sourceOpts.Limit, 0)
	details := make([]RequestDetail, 0, capacity)
	seen := make(map[string]struct{})
	err := ut.forEachHistorySource(ctx, opts.StartDate, opts.EndDate, func(source historyQuerySource) error {
		sourceDetails, err := ut.queryRequestDetailsFromSource(ctx, source.db, &sourceOpts)
		if err != nil {
			return err
		}
		for _, detail := range sourceDetails {
			if _, exists := seen[detail.RequestID]; exists {
				continue
			}
			seen[detail.RequestID] = struct{}{}
			details = append(details, detail)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(details, func(i, j int) bool {
		if !details[i].StartTime.Equal(details[j].StartTime) {
			return details[i].StartTime.After(details[j].StartTime)
		}
		return details[i].RequestID > details[j].RequestID
	})

	start := max(opts.Offset, 0)
	if start >= len(details) {
		return []RequestDetail{}, nil
	}
	end := len(details)
	if opts.Limit > 0 && start+opts.Limit < end {
		end = start + opts.Limit
	}
	return details[start:end], nil
}

func (ut *UsageTracker) queryRequestDetailsFromSource(ctx context.Context, db *sql.DB, opts *QueryOptions) ([]RequestDetail, error) {
	if db == nil {
		return nil, fmt.Errorf("read database not initialized")
	}
	optionalColumns := make(map[string]bool, 4)
	columnRows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info('request_logs')
		WHERE name IN ('model_reasoning_effort', 'service_tier', 'billing_rule_snapshot', 'http_status_is_upstream')`)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect request detail schema: %w", err)
	}
	for columnRows.Next() {
		var name string
		if err := columnRows.Scan(&name); err != nil {
			_ = columnRows.Close()
			return nil, fmt.Errorf("failed to inspect request detail schema: %w", err)
		}
		optionalColumns[name] = true
	}
	if err := columnRows.Err(); err != nil {
		_ = columnRows.Close()
		return nil, fmt.Errorf("failed to inspect request detail schema: %w", err)
	}
	if err := columnRows.Close(); err != nil {
		return nil, fmt.Errorf("failed to inspect request detail schema: %w", err)
	}
	optionalTextExpr := func(column string) string {
		if optionalColumns[column] {
			return "COALESCE(" + column + ", '')"
		}
		return "''"
	}
	optionalBoolExpr := func(column string) string {
		if optionalColumns[column] {
			return "COALESCE(" + column + ", 0)"
		}
		return "0"
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
		` + optionalTextExpr("model_reasoning_effort") + ` as model_reasoning_effort,
		` + optionalTextExpr("service_tier") + ` as service_tier,
		COALESCE(privacy_redacted, false) as privacy_redacted,
		COALESCE(privacy_redaction_summary, '') as privacy_redaction_summary,
		COALESCE(is_streaming, false) as is_streaming,
		COALESCE(provider, 'claude') as provider,
		status, CASE WHEN ` + optionalBoolExpr("http_status_is_upstream") + ` = 1 THEN http_status_code ELSE NULL END as http_status_code, ` + optionalBoolExpr("http_status_is_upstream") + ` as http_status_is_upstream, retry_count,
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
		` + optionalTextExpr("billing_rule_snapshot") + ` as billing_rule_snapshot,
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
		FROM request_logs WHERE 1=1
			AND COALESCE(environment, 'release') = ?`

	args := []interface{}{queryEnvironment(opts)}

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

	query += " ORDER BY start_time DESC, request_id DESC"

	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}
	if opts.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	rows, err := db.QueryContext(ctx, query, args...)
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
			&detail.ReasoningEffort,
			&detail.ServiceTier,
			&detail.PrivacyRedacted, &detail.PrivacyRedactionSummary,
			&detail.IsStreaming,
			&detail.Provider,
			&detail.Status, &detail.HTTPStatusCode, &detail.HTTPStatusIsUpstream, &detail.RetryCount,
			&detail.FailureReason, &detail.LastFailureReason, &detail.CancelReason,
			&detail.InputTokens, &detail.OutputTokens,
			&detail.ReasoningTokens, &detail.BillableOutputTokens, &detail.BillingOutputTokenSource, &detail.BillingRuleSnapshot,
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
	// Calculate date range based on period
	endDate := ut.now()
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

	totals, err := ut.QueryUsageStatsTotals(ctx, &QueryOptions{StartDate: &startDate, EndDate: &endDate})
	if err != nil {
		return nil, fmt.Errorf("failed to query usage stats: %w", err)
	}
	stats := UsageStats{
		Period:        period,
		TotalRequests: int(totals.TotalRequests),
		AvgDuration:   totals.AvgDurationMs(),
		TotalCost:     totals.TotalCostUSD,
	}
	if totals.TotalRequests > 0 {
		stats.SuccessRate = float64(totals.SuccessRequests) / float64(totals.TotalRequests) * 100
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
	opts = ut.scopedQueryOptions(opts)
	var combined UsageStatsTotals
	var startTime, endTime *time.Time
	if opts != nil {
		startTime = opts.StartDate
		endTime = opts.EndDate
	}
	err := ut.forEachHistorySource(ctx, startTime, endTime, func(source historyQuerySource) error {
		totals, err := ut.queryUsageStatsTotalsFromSource(ctx, source.db, opts)
		if err != nil {
			return err
		}
		combined.add(*totals)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &combined, nil
}

func (ut *UsageTracker) queryUsageStatsTotalsFromSource(ctx context.Context, db *sql.DB, opts *QueryOptions) (*UsageStatsTotals, error) {
	if db == nil {
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
		FROM request_logs WHERE 1=1
			AND COALESCE(environment, 'release') = ?`

	args := []interface{}{queryEnvironment(opts)}

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
	if err := db.QueryRowContext(ctx, query, args...).Scan(
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
	opts = ut.scopedQueryOptions(opts)
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
	opts = ut.scopedQueryOptions(opts)
	total := 0
	err := ut.forEachHistorySource(ctx, opts.StartDate, opts.EndDate, func(source historyQuerySource) error {
		count, err := ut.countRequestDetailsFromSource(ctx, source.db, opts)
		if err != nil {
			return err
		}
		total += count
		return nil
	})
	return total, err
}

func (ut *UsageTracker) countRequestDetailsFromSource(ctx context.Context, db *sql.DB, opts *QueryOptions) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("read database not initialized")
	}

	query := "SELECT COUNT(*) FROM request_logs WHERE 1=1 AND COALESCE(environment, 'release') = ?"
	args := []interface{}{queryEnvironment(opts)}

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
	err := db.QueryRowContext(ctx, query, args...).Scan(&count)
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
		WHERE start_time >= ? AND start_time <= ?
		  AND COALESCE(environment, 'release') = ?`

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
	err = ut.forEachHistorySource(ctx, &startOfDay, &endOfDay, func(source historyQuerySource) error {
		rows, err := source.db.QueryContext(ctx, query,
			ut.formatStartTimeQueryBound(startOfDay), ut.formatEndTimeQueryBound(endOfDay), normalizeTrackingEnvironment(ut.environment))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row endpointCostRow
			if err := rows.Scan(
				&row.Channel, &row.EndpointName, &row.GroupName, &row.EndpointChain, &row.Provider, &row.Status,
				&row.TotalTokens, &row.TotalCostUSD,
				&row.InputTokens, &row.OutputTokens,
				&row.CacheCreationTokens, &row.CacheReadTokens,
				&row.InputCostUSD, &row.OutputCostUSD,
				&row.CacheCreationCostUSD, &row.CacheReadCostUSD,
			); err != nil {
				return fmt.Errorf("failed to scan endpoint cost: %w", err)
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
		return rows.Err()
	})
	if err != nil {
		slog.Error("Failed to query endpoint costs", "error", err, "date", date, "start_time", startOfDay, "end_time", endOfDay)
		return nil, fmt.Errorf("failed to query endpoint costs for date %s: %w", date, err)
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

func (ut *UsageTracker) QueryTokenUsageTotals(ctx context.Context, startTime, endTime time.Time, provider string) (TokenUsageTotals, error) {
	query := `SELECT
		COALESCE(SUM(` + distributionInputTokensSQLExpression + `), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(cache_creation_tokens), 0),
		COALESCE(SUM(cache_read_tokens), 0),
		COALESCE(SUM(` + totalTokensSQLExpression + `), 0)
		FROM request_logs
		WHERE start_time >= ? AND start_time < ?
		  AND COALESCE(environment, 'release') = ?`
	args := []any{ut.formatStartTimeQueryBound(startTime), ut.formatStartTimeQueryBound(endTime), normalizeTrackingEnvironment(ut.environment)}
	if provider != "" {
		query += " AND provider = ?"
		args = append(args, provider)
	}

	var totals TokenUsageTotals
	err := ut.forEachHistorySource(ctx, &startTime, &endTime, func(source historyQuerySource) error {
		var sourceTotals TokenUsageTotals
		if err := source.db.QueryRowContext(ctx, query, args...).Scan(
			&sourceTotals.InputTokens,
			&sourceTotals.OutputTokens,
			&sourceTotals.CacheCreationTokens,
			&sourceTotals.CacheReadTokens,
			&sourceTotals.TotalTokens,
		); err != nil {
			return err
		}
		totals.InputTokens += sourceTotals.InputTokens
		totals.OutputTokens += sourceTotals.OutputTokens
		totals.CacheCreationTokens += sourceTotals.CacheCreationTokens
		totals.CacheReadTokens += sourceTotals.CacheReadTokens
		totals.TotalTokens += sourceTotals.TotalTokens
		return nil
	})
	return totals, err
}

func (ut *UsageTracker) QueryTokenUsageBuckets(ctx context.Context, opts *QueryOptions, bucket string) ([]TokenUsageBucket, error) {
	opts = ut.scopedQueryOptions(opts)
	if opts == nil || opts.StartDate == nil || opts.EndDate == nil {
		return []TokenUsageBucket{}, nil
	}
	bucketExpression := "substr(start_time, 1, 10)"
	if bucket == "hour" {
		bucketExpression = "substr(start_time, 1, 13)"
	} else if bucket != "day" {
		return nil, fmt.Errorf("unsupported token usage bucket %q", bucket)
	}

	query := `SELECT ` + bucketExpression + ` AS bucket,
		COUNT(*),
		COALESCE(SUM(` + distributionInputTokensSQLExpression + `), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(cache_creation_tokens), 0),
		COALESCE(SUM(cache_read_tokens), 0),
		COALESCE(SUM(` + totalTokensSQLExpression + `), 0)
		FROM request_logs WHERE 1=1`
	query, args := ut.appendRequestQueryFilters(query, nil, opts)
	query += " GROUP BY bucket ORDER BY bucket ASC"

	byKey := make(map[string]TokenUsageBucket)
	err := ut.forEachHistorySource(ctx, opts.StartDate, opts.EndDate, func(source historyQuerySource) error {
		rows, err := source.db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item TokenUsageBucket
			if err := rows.Scan(
				&item.Key,
				&item.RequestCount,
				&item.InputTokens,
				&item.OutputTokens,
				&item.CacheCreationTokens,
				&item.CacheReadTokens,
				&item.TotalTokens,
			); err != nil {
				return err
			}
			current := byKey[item.Key]
			current.Key = item.Key
			current.RequestCount += item.RequestCount
			current.InputTokens += item.InputTokens
			current.OutputTokens += item.OutputTokens
			current.CacheCreationTokens += item.CacheCreationTokens
			current.CacheReadTokens += item.CacheReadTokens
			current.TotalTokens += item.TotalTokens
			byKey[item.Key] = current
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	items := make([]TokenUsageBucket, 0, len(byKey))
	for _, item := range byKey {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, nil
}

// EndpointAvailabilityBucket 表示单个时间片内某端点的请求可用性聚合
type EndpointAvailabilityBucket struct {
	Key            string  `json:"key"`               // 分桶键：按小时为 "2006-01-02 15"，按天为 "2006-01-02"
	Total          int     `json:"total"`             // 有结论的请求数（success + failed）
	Success        int     `json:"success"`           // 成功完成数
	Failed         int     `json:"failed"`            // 失败数
	AvgFirstByteMs float64 `json:"avg_first_byte_ms"` // 平均首字节耗时
}

// 首字节耗时：优先用流式响应真正开始下发的时刻，非流式或未下发时回退为总耗时。
// start_time / output_sent_at 均以 "2006-01-02 15:04:05" 写入，julianday 可直接解析。
const endpointFirstByteMsSQLExpression = `COALESCE(
	CASE WHEN output_sent_at IS NOT NULL AND output_sent_at >= start_time
		THEN (julianday(output_sent_at) - julianday(start_time)) * 86400000.0
	END,
	duration_ms,
	0)`

// QueryEndpointAvailabilityBuckets 按时间片聚合端点的请求成功率与响应速度。
//
// 数据取自 request_logs 而非健康探测结果：探测通但实际转发失败的端点并不少见，
// 真实请求的成功率更能反映端点可用性。代价是没有流量的时间片没有记录，
// 由调用方补齐为空档 —— 不能省略，否则时间轴会被压缩而失真。
//
// 仅统计已有结论的请求：cancelled 是客户端主动放弃、进行中的请求尚无结果，
// 都不该计入可用率的分母。
func (ut *UsageTracker) QueryEndpointAvailabilityBuckets(ctx context.Context, opts *QueryOptions, bucket string) ([]EndpointAvailabilityBucket, error) {
	opts = ut.scopedQueryOptions(opts)
	if opts == nil || opts.StartDate == nil || opts.EndDate == nil {
		return []EndpointAvailabilityBucket{}, nil
	}
	bucketExpression := "substr(start_time, 1, 10)"
	if bucket == "hour" {
		bucketExpression = "substr(start_time, 1, 13)"
	} else if bucket != "day" {
		return nil, fmt.Errorf("unsupported endpoint availability bucket %q", bucket)
	}

	query := `SELECT ` + bucketExpression + ` AS bucket,
		SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END),
		SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),
		COALESCE(SUM(` + endpointFirstByteMsSQLExpression + `), 0)
		FROM request_logs WHERE 1=1`
	query, args := ut.appendRequestQueryFilters(query, nil, opts)
	query += " AND status IN ('completed', 'failed') GROUP BY bucket ORDER BY bucket ASC"

	type availabilityAcc struct {
		success      int
		failed       int
		latencySumMs float64
	}
	byKey := make(map[string]*availabilityAcc)

	err := ut.forEachHistorySource(ctx, opts.StartDate, opts.EndDate, func(source historyQuerySource) error {
		rows, err := source.db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			var success, failed int
			var latencySumMs float64
			if err := rows.Scan(&key, &success, &failed, &latencySumMs); err != nil {
				return err
			}
			acc := byKey[key]
			if acc == nil {
				acc = &availabilityAcc{}
				byKey[key] = acc
			}
			acc.success += success
			acc.failed += failed
			acc.latencySumMs += latencySumMs
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}

	items := make([]EndpointAvailabilityBucket, 0, len(byKey))
	for key, acc := range byKey {
		total := acc.success + acc.failed
		item := EndpointAvailabilityBucket{
			Key:     key,
			Total:   total,
			Success: acc.success,
			Failed:  acc.failed,
		}
		if total > 0 {
			item.AvgFirstByteMs = acc.latencySumMs / float64(total)
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, nil
}

func (ut *UsageTracker) appendRequestQueryFilters(query string, args []any, opts *QueryOptions) (string, []any) {
	if opts == nil {
		return query, args
	}
	query += " AND COALESCE(environment, 'release') = ?"
	args = append(args, queryEnvironment(opts))
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
	if opts.Status == "failed" {
		query += " AND status IN ('failed', 'partial_failed', 'error', 'auth_error', 'rate_limited', 'server_error', 'network_error', 'stream_error', 'timeout')"
	} else if opts.Status != "" {
		query += " AND status = ?"
		args = append(args, opts.Status)
	}
	if opts.Provider != "" {
		query += " AND provider = ?"
		args = append(args, opts.Provider)
	}
	return query, args
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
