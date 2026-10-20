package app

import (
	"time"
)

const (
	usageDBQueryTimeout             = 8 * time.Second
	tokenUsageHeatmapHourlyMaxRange = 7 * 24 * time.Hour
)

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

type RequestListResult struct {
	Requests []RequestRecord `json:"requests"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

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

type chainNode struct {
	Hop             int    `json:"hop"`
	Channel         string `json:"channel"`
	CredentialGroup string `json:"credential_group,omitempty"`
	Endpoint        string `json:"endpoint"`
	Attempts        int    `json:"attempts,omitempty"`    // 本跳实际发出的请求次数；旧记录为 0
	StartMs         int64  `json:"start_ms,omitempty"`    // 本跳开始时距请求开始的毫秒数
	DurationMs      *int64 `json:"duration_ms,omitempty"` // 本跳耗时；旧记录与进行中的最后一跳为空
	StatusCode      int    `json:"status_code,omitempty"`
	Error           string `json:"error,omitempty"`
	// Headers 失败响应的诊断头白名单（server、cf-ray、x-request-id 等）
	Headers map[string]string `json:"headers,omitempty"`
	// Local 本跳没有拿到上游响应，Error 是本地（网络/超时）错误
	Local bool `json:"local,omitempty"`
}

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

type TokenUsageData struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

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
