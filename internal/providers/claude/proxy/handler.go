package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/events"
	"ai-relay-forwarder/internal/middleware"
	"ai-relay-forwarder/internal/privacy"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
	"ai-relay-forwarder/internal/providers/claude/proxy/handlers"
	"ai-relay-forwarder/internal/providers/claude/proxy/response"
	"ai-relay-forwarder/internal/tracking"
)

// Context key for endpoint information
type contextKey string

const EndpointContextKey = contextKey("endpoint")

// Handler handles HTTP proxy requests
type Handler struct {
	endpointManager      *endpoint.Manager
	config               *config.Config
	retryHandler         *RetryHandler
	usageTracker         *tracking.UsageTracker
	monitoringMiddleware *middleware.MonitoringMiddleware
	responseProcessor    *response.Processor
	tokenAnalyzer        *response.TokenAnalyzer
	forwarder            *handlers.Forwarder
	regularHandler       *handlers.RegularHandler
	streamingHandler     *handlers.StreamingHandler
	privacyService       *privacy.Service
	eventBus             events.EventBus // EventBus事件总线
	// 🔧 [Critical修复] 保存共享的SuspensionManager实例的引用
	// 确保在SetUsageTracker中重建Handler时保持共享状态
	sharedSuspensionManager handlers.SuspensionManager
	// 🚀 [端点自愈] 端点恢复信号管理器
	recoverySignalManager *EndpointRecoverySignalManager
}

// NewHandler creates a new proxy handler
func NewHandler(endpointManager *endpoint.Manager, cfg *config.Config) *Handler {
	retryHandler := NewRetryHandler(cfg)
	retryHandler.SetEndpointManager(endpointManager)

	// 创建forwarder
	forwarder := handlers.NewForwarder(cfg, endpointManager)

	// 🚀 [端点自愈] 创建端点恢复信号管理器
	recoverySignalManager := NewEndpointRecoverySignalManager()

	h := &Handler{
		endpointManager:       endpointManager,
		config:                cfg,
		retryHandler:          retryHandler,
		responseProcessor:     response.NewProcessor(),
		forwarder:             forwarder,
		recoverySignalManager: recoverySignalManager, // 🚀 [端点自愈] 保存恢复信号管理器引用
	}

	// 初始化 token analyzer
	provider := &TokenParserProviderImpl{}
	h.tokenAnalyzer = response.NewTokenAnalyzer(nil, nil, provider)

	// 创建工厂实例
	tokenParserFactory := &TokenParserFactoryImpl{}
	streamProcessorFactory := &StreamProcessorFactoryImpl{}
	errorRecovery := NewErrorRecoveryManager(nil)
	retryManagerFactory := &RetryManagerFactoryImpl{
		config:          cfg,
		errorRecovery:   NewErrorRecoveryManager(nil), // 临时创建，后续会在工厂中重新创建
		endpointManager: endpointManager,
	}
	suspensionManagerFactory := &SuspensionManagerFactoryImpl{
		config:                cfg,
		endpointManager:       endpointManager,
		recoverySignalManager: recoverySignalManager, // 🚀 [端点自愈] 传递恢复信号管理器
	}

	// 🔧 [Critical修复] 创建单一共享的SuspensionManager实例
	// 确保常规请求和流式请求共享同一个挂起计数器，真正实现全局限制
	sharedSuspensionManager := suspensionManagerFactory.NewSuspensionManager()

	// 🔧 [Critical修复] 保存共享SuspensionManager的引用到Handler结构体
	// 确保在SetUsageTracker中能重用相同的实例
	h.sharedSuspensionManager = sharedSuspensionManager

	// 创建RetryHandler适配器
	retryHandlerAdapter := &RetryHandlerAdapter{innerHandler: retryHandler}

	// 创建TokenAnalyzer适配器
	tokenAnalyzerAdapter := &TokenAnalyzerAdapter{innerAnalyzer: h.tokenAnalyzer}

	// 创建regularHandler - 传入正确初始化的组件
	h.regularHandler = handlers.NewRegularHandler(
		cfg,
		endpointManager,
		forwarder,
		nil,                  // usageTracker will be set later
		h.responseProcessor,  // 传入已创建的responseProcessor
		tokenAnalyzerAdapter, // 传入TokenAnalyzer适配器
		retryHandlerAdapter,  // 传入RetryHandler适配器
		errorRecovery,
		retryManagerFactory,
		suspensionManagerFactory,
		// 🔧 [Critical修复] 传入共享的SuspensionManager实例
		sharedSuspensionManager,
	)

	// 创建streamingHandler
	h.streamingHandler = handlers.NewStreamingHandler(
		cfg,
		endpointManager,
		forwarder,
		nil, // usageTracker will be set later
		tokenParserFactory,
		streamProcessorFactory,
		errorRecovery,
		retryManagerFactory, // 传递retryManagerFactory
		suspensionManagerFactory,
		// 🔧 [Critical修复] 传入相同的共享SuspensionManager实例
		sharedSuspensionManager,
	)

	// 初始化 token analyzer，暂时不设置 usageTracker 和 monitoringMiddleware
	// 这些将在 SetUsageTracker 和 SetMonitoringMiddleware 方法中设置
	// provider已经在上面定义过了，这里删除重复定义

	return h
}

func (h *Handler) SetPrivacyService(service *privacy.Service) {
	h.privacyService = service
	if h.regularHandler != nil {
		h.regularHandler.SetPrivacyFilter(service)
	}
	if h.streamingHandler != nil {
		h.streamingHandler.SetPrivacyFilter(service)
	}
}

// SetMonitoringMiddleware 设置监控中间件用于重试跟踪
func (h *Handler) SetMonitoringMiddleware(mm *middleware.MonitoringMiddleware) {
	h.monitoringMiddleware = mm
	h.retryHandler.SetMonitoringMiddleware(mm)

	// 同时更新tokenAnalyzer的monitoringMiddleware
	if h.tokenAnalyzer != nil {
		provider := &TokenParserProviderImpl{}
		h.tokenAnalyzer = response.NewTokenAnalyzer(h.usageTracker, mm, provider)
	}
}

// SetUsageTracker sets the usage tracker for request tracking
func (h *Handler) SetUsageTracker(ut *tracking.UsageTracker) {
	h.usageTracker = ut

	// ⚠️ 重要：先更新tokenAnalyzer，再创建适配器
	provider := &TokenParserProviderImpl{}
	h.tokenAnalyzer = response.NewTokenAnalyzer(ut, h.retryHandler.monitoringMiddleware, provider)

	// 创建共用的工厂实例
	errorRecovery := NewErrorRecoveryManager(ut)
	retryManagerFactory := &RetryManagerFactoryImpl{
		config:          h.config,
		errorRecovery:   NewErrorRecoveryManager(nil), // 临时创建，后续会在工厂中重新创建
		endpointManager: h.endpointManager,
	}
	suspensionManagerFactory := &SuspensionManagerFactoryImpl{
		config:                h.config,
		endpointManager:       h.endpointManager,
		recoverySignalManager: h.recoverySignalManager, // 🚀 [端点自愈] 修复：确保恢复信号能力不丢失
	}

	// 重新创建regularHandler以包含usageTracker
	if h.regularHandler != nil {
		// 创建适配器 - 使用更新后的tokenAnalyzer
		retryHandlerAdapter := &RetryHandlerAdapter{innerHandler: h.retryHandler}
		tokenAnalyzerAdapter := &TokenAnalyzerAdapter{innerAnalyzer: h.tokenAnalyzer}

		h.regularHandler = handlers.NewRegularHandler(
			h.config,
			h.endpointManager,
			h.forwarder,
			ut,
			h.responseProcessor,  // responseProcessor
			tokenAnalyzerAdapter, // tokenAnalyzer适配器
			retryHandlerAdapter,  // retryHandler适配器
			errorRecovery,
			retryManagerFactory,
			suspensionManagerFactory,
			// 🔧 [Critical修复] 使用保存的共享SuspensionManager实例
			h.sharedSuspensionManager,
		)
		h.regularHandler.SetPrivacyFilter(h.privacyService)
	}

	// 重新创建streamingHandler以包含usageTracker
	if h.streamingHandler != nil {
		tokenParserFactory := &TokenParserFactoryImpl{}
		streamProcessorFactory := &StreamProcessorFactoryImpl{}

		h.streamingHandler = handlers.NewStreamingHandler(
			h.config,
			h.endpointManager,
			h.forwarder,
			ut, // 设置usageTracker
			tokenParserFactory,
			streamProcessorFactory,
			errorRecovery,
			retryManagerFactory,
			suspensionManagerFactory,
			// 🔧 [Critical修复] 使用保存的共享SuspensionManager实例
			h.sharedSuspensionManager,
		)
		h.streamingHandler.SetPrivacyFilter(h.privacyService)
	}

	// 注意：h.tokenAnalyzer 已经在方法开头更新
}

// GetRetryHandler returns the retry handler for accessing suspended request counts
func (h *Handler) GetRetryHandler() *RetryHandler {
	return h.retryHandler
}

// SetEventBus 设置EventBus事件总线
func (h *Handler) SetEventBus(eventBus events.EventBus) {
	h.eventBus = eventBus
}

// extractModelFromRequestBody 从请求体中提取模型名称
// 仅对 /v1/messages 相关路径进行解析，避免不必要的JSON解析开销
func (h *Handler) extractModelFromRequestBody(bodyBytes []byte, path string) string {
	// 仅对包含 messages 的路径尝试解析模型
	if !strings.Contains(path, "/v1/messages") {
		return ""
	}

	// 避免解析空请求体
	if len(bodyBytes) == 0 {
		return ""
	}

	var requestBody struct {
		Model string `json:"model"`
	}

	if err := json.Unmarshal(bodyBytes, &requestBody); err == nil && requestBody.Model != "" {
		return requestBody.Model
	}

	return ""
}

// extractReasoningConfig 从 Claude 请求体提取推理相关配置并合并序列化为 JSON
//
// 这里是两个语义不同的参数，Anthropic 放在请求体的不同位置：
//   - thinking.type        扩展思考的开关模式：adaptive / enabled / disabled
//   - output_config.effort 推理强度：low / medium / high / xhigh / max
//
// 合并成一个对象存入 model_reasoning_effort，供前端按 effort 优先展示。
// 只有 thinking 而没有 effort 时不能把 adaptive 当成推理强度——它只是「让模型
// 自行决定思考量」，与强度档位无关。
//
// 返回 "" 表示两者都未设置（模型默认行为）
func (h *Handler) extractReasoningConfig(bodyBytes []byte) string {
	if len(bodyBytes) == 0 {
		return ""
	}
	var partial struct {
		Thinking     json.RawMessage `json:"thinking"`
		OutputConfig struct {
			Effort string `json:"effort"`
		} `json:"output_config"`
	}
	if err := json.Unmarshal(bodyBytes, &partial); err != nil {
		return ""
	}

	merged := make(map[string]any, 3)
	if len(partial.Thinking) > 0 {
		var thinking map[string]any
		if err := json.Unmarshal(partial.Thinking, &thinking); err == nil {
			for key, value := range thinking {
				merged[key] = value
			}
		}
	}
	if effort := strings.TrimSpace(partial.OutputConfig.Effort); effort != "" {
		merged["effort"] = effort
	}
	if len(merged) == 0 {
		return ""
	}

	encoded, err := json.Marshal(merged)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// ServeHTTP implements the http.Handler interface
// 统一请求分发逻辑 - 整合流式处理、错误恢复和生命周期管理
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 客户端可能始终发起 count_tokens。无论是否开启上游转发，都必须在这里拦截，
	// 避免关闭开关后落入普通处理器并把该路径原样发送给服务商。
	if r.URL.Path == "/v1/messages/count_tokens" {
		ctx := r.Context()
		connID, _ := r.Context().Value("conn_id").(string)
		lifecycleManager := NewRequestLifecycleManagerWithRecoverySignal(h.usageTracker, h.monitoringMiddleware, connID, h.eventBus, h.recoverySignalManager)

		// 读取请求体
		var bodyBytes []byte
		if r.Body != nil {
			var err error
			bodyBytes, err = io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "请求读取失败: 无法读取请求体", http.StatusBadRequest)
				return
			}
			r.Body.Close()
		}

		lifecycleManager.StartRequest(r.RemoteAddr, r.Header.Get("User-Agent"), r.Method, r.URL.Path, false)
		// count_tokens 与主流程共用同一套请求元数据（模型/推理配置/档位），
		// 否则请求追踪里这些列会全空。
		h.trackRequestMetadata(lifecycleManager, bodyBytes, r.URL.Path)

		// 使用CountTokensHandler处理
		countTokensHandler := handlers.NewCountTokensHandler(h.config, h.endpointManager, h.forwarder, h.privacyService)
		countTokensHandler.Handle(ctx, w, r, bodyBytes, connID, lifecycleManager)
		return
	}

	// 创建请求上下文
	ctx := r.Context()

	// 获取连接ID
	connID := ""
	if connIDValue, ok := r.Context().Value("conn_id").(string); ok {
		connID = connIDValue
	}

	// 创建统一的请求生命周期管理器
	lifecycleManager := NewRequestLifecycleManagerWithRecoverySignal(h.usageTracker, h.monitoringMiddleware, connID, h.eventBus, h.recoverySignalManager)

	// 克隆请求体用于重试
	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			lifecycleManager.HandleError(err)
			http.Error(w, "请求读取失败: 无法读取请求体", http.StatusBadRequest)
			return
		}
		r.Body.Close()
	}

	// 检测是否为SSE流式请求
	isSSE := h.detectSSERequest(r, bodyBytes)

	// 开始请求跟踪（传递流式标记）
	clientIP := r.RemoteAddr
	userAgent := r.Header.Get("User-Agent")
	lifecycleManager.StartRequest(clientIP, userAgent, r.Method, r.URL.Path, isSSE)

	h.trackRequestMetadata(lifecycleManager, bodyBytes, r.URL.Path)

	// 统一请求处理
	if isSSE {
		// 流式请求处理 - 使用StreamingHandler
		if h.streamingHandler != nil {
			h.streamingHandler.HandleStreamingRequest(ctx, w, r, bodyBytes, lifecycleManager)
			// h.regularHandler.HandleRegularRequestUnified(ctx, w, r, bodyBytes, lifecycleManager)
		} else {
			// 备用方案：如果streamingHandler不可用，使用regularHandler
			h.regularHandler.HandleRegularRequestUnified(ctx, w, r, bodyBytes, lifecycleManager)
		}
	} else {
		// 常规请求处理 - 使用RegularHandler
		h.regularHandler.HandleRegularRequestUnified(ctx, w, r, bodyBytes, lifecycleManager)
	}
}

// trackRequestMetadata 把请求体里的模型 / 推理配置 / 档位写入生命周期管理器。
// count_tokens 与常规转发共用，两条路径的请求追踪列才不会一边有值一边全空。
// 在请求可能终态前同步写入，避免后台解析晚于归档而丢失元数据。
func (h *Handler) trackRequestMetadata(lifecycleManager *RequestLifecycleManager, bodyBytes []byte, path string) {
	if modelName := h.extractModelFromRequestBody(bodyBytes, path); modelName != "" {
		lifecycleManager.SetModel(modelName)
	}

	// 提取并追踪推理配置（thinking 开关模式 + output_config.effort 推理强度）
	if reasoningJSON := h.extractReasoningConfig(bodyBytes); reasoningJSON != "" {
		lifecycleManager.SetReasoningConfig(reasoningJSON)
	}

	// 提取并追踪 service_tier（auto / standard_only）
	if serviceTier := h.extractServiceTier(bodyBytes); serviceTier != "" {
		lifecycleManager.SetServiceTier(serviceTier)
	}
}

// detectSSERequest 统一SSE请求检测逻辑
func (h *Handler) detectSSERequest(r *http.Request, bodyBytes []byte) bool {
	// 请求体中的 stream 是 Anthropic API 的明确语义，优先于 Accept/Cache-Control。
	// CLI 的非流式请求也可能带 Cache-Control: no-cache；先看 header 会把它误判成
	// 流式请求，从而绕过非流式端点（stream_only）过滤。
	if len(bodyBytes) > 0 {
		var requestBody struct {
			Stream *bool `json:"stream"`
		}
		if err := json.Unmarshal(bodyBytes, &requestBody); err == nil && requestBody.Stream != nil {
			return *requestBody.Stream
		}
	}

	// 请求体没有明确 stream 字段时，再使用传输层 header 作为兼容兜底。
	acceptHeader := r.Header.Get("Accept")
	cacheControlHeader := r.Header.Get("Cache-Control")
	streamHeader := strings.ToLower(strings.TrimSpace(r.Header.Get("stream")))
	if strings.Contains(strings.ToLower(acceptHeader), "text/event-stream") ||
		strings.Contains(strings.ToLower(cacheControlHeader), "no-cache") ||
		streamHeader == "true" {
		return true
	}

	return false
}

// extractServiceTier 从 Claude 请求体 JSON 中提取 service_tier
// 取值: auto（默认）/ standard_only（仅标准容量）
func (h *Handler) extractServiceTier(bodyBytes []byte) string {
	if len(bodyBytes) == 0 {
		return ""
	}
	var partial struct {
		ServiceTier string `json:"service_tier"`
	}
	if err := json.Unmarshal(bodyBytes, &partial); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(partial.ServiceTier))
}

// UpdateConfig updates the handler configuration
func (h *Handler) UpdateConfig(cfg *config.Config) {
	h.config = cfg

	// Update retry handler with new config
	h.retryHandler.UpdateConfig(cfg)

	// 🔧 [热更新] Update suspension manager with new config
	if h.sharedSuspensionManager != nil {
		h.sharedSuspensionManager.UpdateConfig(cfg)
	}

	// 更新子处理器配置，确保设置变更能传播到流式和常规处理器
	if h.streamingHandler != nil {
		h.streamingHandler.UpdateConfig(cfg)
	}
	if h.regularHandler != nil {
		h.regularHandler.UpdateConfig(cfg)
	}
}

// GetSuspendedRequestsCount 获取当前挂起等待的请求数量
// 用于前端展示当前有多少请求正在等待端点恢复
func (h *Handler) GetSuspendedRequestsCount() int {
	if h.sharedSuspensionManager != nil {
		return h.sharedSuspensionManager.GetSuspendedRequestsCount()
	}
	return 0
}

// GetSuspendedEndpointCounts 获取当前挂起请求的端点计数
func (h *Handler) GetSuspendedEndpointCounts() map[string]int {
	if h.sharedSuspensionManager != nil {
		return h.sharedSuspensionManager.GetSuspendedEndpointCounts()
	}
	return map[string]int{}
}

// GetManualRetryRequestsCount 获取等待用户操作的挂起请求数量
func (h *Handler) GetManualRetryRequestsCount() int {
	if h.sharedSuspensionManager != nil {
		return h.sharedSuspensionManager.GetManualRetryRequestsCount()
	}
	return 0
}

// GetManualRetryChannelCounts 获取等待用户操作的渠道计数
func (h *Handler) GetManualRetryChannelCounts() map[string]int {
	if h.sharedSuspensionManager != nil {
		return h.sharedSuspensionManager.GetManualRetryChannelCounts()
	}
	return map[string]int{}
}

// noOpFlusher 是一个不执行实际flush操作的flusher实现
// 用于测试和不支持Flusher的环境
type noOpFlusher struct{}

func (f *noOpFlusher) Flush() {
	// 不执行任何操作，避免panic但保持流式处理逻辑
}
