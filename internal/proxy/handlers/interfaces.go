package handlers

import (
	"context"
	"net/http"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/endpoint"
	"ai-relay-forwarder/internal/monitor"
	"ai-relay-forwarder/internal/tracking"
)

// SuspensionResult 挂起等待结果类型
// 用于区分WaitForEndpointRecovery的不同结果状态
type SuspensionResult int

const (
	SuspensionSuccess   SuspensionResult = iota // 成功恢复
	SuspensionTimeout                           // 等待超时
	SuspensionCancelled                         // 用户取消
)

// String 返回SuspensionResult的字符串表示
func (sr SuspensionResult) String() string {
	switch sr {
	case SuspensionSuccess:
		return "success"
	case SuspensionTimeout:
		return "timeout"
	case SuspensionCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// RequestLifecycleManager 请求生命周期管理器接口
// 修改版本：添加CompleteRequest和HandleNonTokenResponse方法以支持生命周期管理器架构
type RequestLifecycleManager interface {
	GetRequestID() string
	SetEndpoint(name, group, channel string)
	SetModel(modelName string)                              // 简单设置模型
	SetModelWithComparison(modelName, source string)        // 带对比的设置模型
	SetModelRewrite(originalModel, newModel, source string) // 设置模型改写信息
	HasModel() bool                                         // 检查是否已有模型
	GetModelName() string                                   // 获取当前模型名称
	UpdateStatus(status string, endpointIndex, statusCode int)
	HandleError(err error)
	PrepareErrorContext(errorCtx *ErrorContext)
	// 新增方法：统一的请求完成入口
	CompleteRequest(tokens *tracking.TokenUsage)
	// 🆕 [流完整性追踪] 2025-12-11: 带数据质量标记的请求完成
	CompleteRequestWithQuality(tokens *tracking.TokenUsage, failureReason string)
	HandleNonTokenResponse(responseContent string)
	RecordPrivacyRedaction(summaryJSON string, redactedBody []byte, matchDetailsJSON string)
	// 失败请求Token记录方法：只记录Token统计，不改变请求状态
	RecordTokensForFailedRequest(tokens *tracking.TokenUsage, failureReason string)
	// 🔢 [语义修复] 新增尝试计数管理方法
	IncrementAttempt() int // 线程安全地增加尝试计数，返回当前计数
	GetAttemptCount() int  // 线程安全地获取当前尝试次数
	// 🔧 [P5.3 新增] 全局重试预算管理
	InitRetryBudget(maxAttempts int, maxDuration time.Duration)
	CheckRetryBudget() (exhausted bool, reason string)
	// 🚀 [状态机重构] Phase 4: 新增状态管理方法
	MapErrorTypeToFailureReason(errorType ErrorType) string         // 映射ErrorType到failure_reason
	FailRequest(failureReason, errorDetail string, httpStatus int)  // 标记请求为最终失败
	CancelRequest(cancelReason string, tokens *tracking.TokenUsage) // 标记请求被取消
	// 🔥 [重复计费防护] 2025-12-26: 重复计费防护相关方法
	MarkOutputSent()        // 标记已输出内容
	HasOutputContent() bool // 检查是否已输出内容
	// 🔄 [递归保护] 重启计数相关方法
	CanRestart() bool      // 检查是否可以重启
	IncrementRestart() int // 增加重启次数并返回新值
	GetMaxRestarts() int   // 获取最大重启次数
	// 🚀 [Plan D实现] 2025-12-26: 部分Token记录
	RecordPartialTokens(tokens *tracking.TokenUsage, failureReason, endpointName string)
}

// ErrorRecoveryManager 错误恢复管理器接口
type ErrorRecoveryManager interface {
	ClassifyError(err error, connID, endpointName, groupName string, attemptCount int) ErrorContext
	HandleFinalFailure(errorCtx ErrorContext)
	GetErrorTypeName(errorType ErrorType) string
}

// ErrorContext 错误上下文信息
type ErrorContext struct {
	RequestID      string
	EndpointName   string
	GroupName      string
	AttemptCount   int
	ErrorType      ErrorType
	OriginalError  error
	RetryableAfter time.Duration
	MaxRetries     int
}

// ErrorType 错误类型枚举
// 注意：值顺序必须与 proxy/error_recovery.go 中的 ErrorType 完全一致
type ErrorType int

const (
	ErrorTypeUnknown            ErrorType = iota // 0: 未知错误
	ErrorTypeNetwork                             // 1: 网络错误（连接失败等，可重试）
	ErrorTypeEOF                                 // 2: EOF 错误（连接中断，不可重试，避免重复计费）
	ErrorTypeConnectionTimeout                   // 3: 连接超时（可重试，未开始处理）
	ErrorTypeResponseTimeout                     // 4: 响应超时（不可重试，可能已计费）
	ErrorTypeTimeout                             // 5: 超时错误（兼容旧代码）
	ErrorTypeHTTP                                // 6: HTTP错误
	ErrorTypeServerError                         // 7: 服务器错误（5xx）
	ErrorTypeStream                              // 8: 流式处理错误
	ErrorTypeAuth                                // 9: 认证错误
	ErrorTypeRateLimit                           // 10: 限流错误
	ErrorTypeParsing                             // 11: 解析错误
	ErrorTypeClientCancel                        // 12: 客户端取消错误
	ErrorTypeNoHealthyEndpoints                  // 13: 没有健康端点可用
)

// StreamIncompleteErrorInterface 流不完整错误接口
// 🆕 [流完整性追踪] 2025-12-11
// 用于跨包检查流不完整错误，避免循环导入
type StreamIncompleteErrorInterface interface {
	error
	GetFailureReason() string // 获取 failure_reason（incomplete_stream 或 stream_truncated）
	GetModelName() string     // 获取模型名称
	GetReason() string        // 获取不完整的原因（用于日志）
}

// TokenParser Token解析器接口
type TokenParser interface {
	ParseSSELine(line string) *monitor.TokenUsage // 返回TokenUsage类型
	SetModelName(model string)
}

// StreamProcessor 流式处理器接口
// 修改版本：返回Token使用信息和模型名称而非直接记录到usageTracker
type StreamProcessor interface {
	ProcessStreamWithRetry(ctx context.Context, resp *http.Response) (*tracking.TokenUsage, string, error)
	HasSentContent() bool // 🔥 [重复计费防护] 2025-12-26: 检查是否已发送实际模型输出
}

// RetryHandler 重试处理器接口
type RetryHandler interface {
	ExecuteWithContext(ctx context.Context, operation func(*endpoint.Endpoint, string) (*http.Response, error), connID string) (*http.Response, error)
	ShouldSuspendRequest(ctx context.Context) bool
	WaitForGroupSwitch(ctx context.Context, connID string) bool
	SetEndpointManager(manager interface{})
	SetUsageTracker(tracker *tracking.UsageTracker)
}

// TokenParserFactory Token解析器工厂接口
type TokenParserFactory interface {
	NewTokenParserWithUsageTracker(connID string, usageTracker *tracking.UsageTracker) TokenParser
}

// StreamProcessorFactory 流式处理器工厂接口
type StreamProcessorFactory interface {
	NewStreamProcessor(tokenParser TokenParser, usageTracker *tracking.UsageTracker,
		w http.ResponseWriter, flusher http.Flusher, requestID, endpoint string) StreamProcessor
}

// ErrorRecoveryFactory 错误恢复管理器工厂接口
type ErrorRecoveryFactory interface {
	NewErrorRecoveryManager(usageTracker *tracking.UsageTracker) ErrorRecoveryManager
}

// RetryHandlerFactory 重试处理器工厂接口
type RetryHandlerFactory interface {
	NewRetryHandler(config interface{}) RetryHandler
}

// TokenAnalyzer Token分析器接口
type TokenAnalyzer interface {
	AnalyzeResponseForTokens(ctx context.Context, responseBody, endpointName string, r *http.Request)
	AnalyzeResponseForTokensUnified(responseBytes []byte, connID, endpointName string) (*tracking.TokenUsage, string)
}

// ResponseProcessor 响应处理器接口
type ResponseProcessor interface {
	CopyResponseHeaders(resp *http.Response, w http.ResponseWriter)
	ProcessResponseBody(resp *http.Response) ([]byte, error)
	ReadAndDecompressResponse(ctx context.Context, resp *http.Response, endpointName string) ([]byte, error)
}

// TokenAnalyzerFactory Token分析器工厂接口
type TokenAnalyzerFactory interface {
	NewTokenAnalyzer(usageTracker *tracking.UsageTracker) TokenAnalyzer
}

// ResponseProcessorFactory 响应处理器工厂接口
type ResponseProcessorFactory interface {
	NewResponseProcessor() ResponseProcessor
}

// RetryManagerFactory 重试管理器工厂接口
type RetryManagerFactory interface {
	NewRetryManager() RetryManager
}

// SuspensionManagerFactory 挂起管理器工厂接口
type SuspensionManagerFactory interface {
	NewSuspensionManager() SuspensionManager
}

// RetryDecision 统一重试决策结果
// 包含重试策略的完整决策信息，用于替代原有的复杂RetryController机制
type RetryDecision struct {
	RetrySameEndpoint bool          // 是否继续在当前端点重试
	SwitchEndpoint    bool          // 是否切换到下一端点
	SuspendRequest    bool          // 是否尝试挂起请求
	Delay             time.Duration // 重试延迟时间
	FinalStatus       string        // 若终止，应记录的最终状态
	Reason            string        // 决策原因（用于日志）
}

// RetryManager 重试管理器接口
type RetryManager interface {
	ShouldRetry(errorCtx *ErrorContext, attempt int) (bool, time.Duration)
	GetHealthyEndpoints(ctx context.Context) []*endpoint.Endpoint
	// GetHealthyEndpointsExcluding 获取健康端点，排除已尝试的端点
	// 用于实现严格优先级转移逻辑，确保在同一请求链路内只能向下一优先级转移
	GetHealthyEndpointsExcluding(ctx context.Context, excludeKeys []string) []*endpoint.Endpoint
	// GetRecoveredEndpointsInChannel 获取当前渠道内已恢复的端点
	// 用于 auto_wait 策略下最后一个端点失败后的重试逻辑
	GetRecoveredEndpointsInChannel(triedKeys []string) []*endpoint.Endpoint
	GetMaxAttempts() int
	// ShouldRetryWithDecision 统一重试决策方法
	// 完全复制retry/policy.go的决策逻辑，确保行为一致
	// errorCtx: 错误上下文信息
	// localAttempt: 当前端点的尝试次数（从1开始，用于退避计算）
	// globalAttempt: 全局尝试次数（用于限流策略）
	// isStreaming: 是否为流式请求
	ShouldRetryWithDecision(errorCtx *ErrorContext, localAttempt int, globalAttempt int, isStreaming bool) RetryDecision
	// IsAutoWaitStrategy 检查是否为 auto_wait 策略
	IsAutoWaitStrategy() bool
}

// SuspensionManager 挂起管理器接口
type SuspensionManager interface {
	ShouldSuspend(ctx context.Context) bool
	WaitForGroupSwitch(ctx context.Context, connID string) bool
	WaitForEndpointRecovery(ctx context.Context, connID, failedEndpoint string) bool // 🚀 [端点自愈] 新增端点恢复等待方法
	// 🎯 [挂起取消区分] 新增带结果的端点恢复等待方法，能区分成功/超时/取消
	WaitForEndpointRecoveryWithResult(ctx context.Context, connID, failedEndpoint string) SuspensionResult
	// 🛑 [循环终止] 仅等待用户操作的挂起（不监听端点自愈）
	WaitForEndpointRecoveryWithResultManual(ctx context.Context, connID, failedEndpoint string) SuspensionResult
	// 🛑 [循环终止] 手动重试计数与渠道标记
	BeginManualRetry(requestID string, channels []string)
	EndManualRetry(requestID string)
	GetManualRetryRequestsCount() int
	GetManualRetryChannelCounts() map[string]int
	GetSuspendedRequestsCount() int
	GetSuspendedEndpointCounts() map[string]int
	// 🔧 [热更新] 更新配置
	UpdateConfig(cfg *config.Config)
}

// GetDefaultStatusCodeForFinalStatus 根据最终状态获取默认HTTP状态码
// 用于在RetryDecision中没有明确状态码时提供合理默认值
//
// 工具函数签名（应在具体实现中定义）:
// func GetDefaultStatusCodeForFinalStatus(finalStatus string) int
