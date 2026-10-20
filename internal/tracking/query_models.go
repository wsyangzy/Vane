package tracking

import (
	"time"
)

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
	RouteSnapshot           string `json:"route_snapshot"`
	ReasoningEffort         string `json:"reasoning_effort"`
	ServiceTier             string `json:"service_tier"`
	PrivacyRedacted         bool   `json:"privacy_redacted"`
	PrivacyRedactionSummary string `json:"privacy_redaction_summary"`
	IsStreaming             bool   `json:"is_streaming"` // 是否为流式请求
	Provider                string `json:"provider"`     // 提供商标识: claude/codex

	Status               string `json:"status"`
	StreamOutcome        string `json:"stream_outcome"` // 终态枚举；历史行与活跃请求为空
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
	// CacheHitNumerator / CacheHitDenominator 缓存命中率的分子分母（token 加权）。
	// 分开累加而不是存比率：跨数据源（活动库 + 月度归档 + 热池）合并时比率不可相加
	CacheHitNumerator   int64
	CacheHitDenominator int64
}

func (t *UsageStatsTotals) add(other UsageStatsTotals) {
	t.TotalRequests += other.TotalRequests
	t.SuccessRequests += other.SuccessRequests
	t.FailedRequests += other.FailedRequests
	t.TotalTokens += other.TotalTokens
	t.TotalCostUSD += other.TotalCostUSD
	t.DurationSumMs += other.DurationSumMs
	t.DurationCount += other.DurationCount
	t.CacheHitNumerator += other.CacheHitNumerator
	t.CacheHitDenominator += other.CacheHitDenominator
}

func (t UsageStatsTotals) AvgDurationMs() float64 {
	if t.DurationCount <= 0 {
		return 0
	}
	return float64(t.DurationSumMs) / float64(t.DurationCount)
}

// CacheHitRate 缓存命中率（0~1）；没有任何输入用量时返回 nil，由展示层显示 "-"
func (t UsageStatsTotals) CacheHitRate() *float64 {
	return cacheHitRate(t.CacheHitNumerator, t.CacheHitDenominator)
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

	// CacheHitRate token 加权缓存命中率（0~1），按 provider 口径计算；没有输入用量时为 nil
	CacheHitRate        *float64 `json:"cache_hit_rate"`
	cacheHitNumerator   int64
	cacheHitDenominator int64
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
