package proxy

import (
	"context"
	"net/http"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/endpoint"
	"ai-relay-forwarder/internal/monitor"
	"ai-relay-forwarder/internal/providers/claude/proxy/handlers"
	"ai-relay-forwarder/internal/providers/claude/proxy/response"
	"ai-relay-forwarder/internal/tracking"
)

// TokenParserProviderImpl 实现 TokenAnalyzer 所需的 TokenParserProvider 接口
type TokenParserProviderImpl struct{}

func (p *TokenParserProviderImpl) NewTokenParser() response.TokenParser {
	return NewTokenParser()
}

func (p *TokenParserProviderImpl) NewTokenParserWithUsageTracker(requestID string, usageTracker *tracking.UsageTracker) response.TokenParser {
	return NewTokenParserWithUsageTracker(requestID, usageTracker)
}

// =========================
// Adapters
// =========================

// TokenParserAdapter 适配 proxy.TokenParser → handlers.TokenParser
type TokenParserAdapter struct {
	innerParser *TokenParser
}

func (ta *TokenParserAdapter) ParseSSELine(line string) *monitor.TokenUsage {
	return ta.innerParser.ParseSSELine(line)
}

func (ta *TokenParserAdapter) SetModelName(model string) {
	ta.innerParser.SetModelName(model)
}

// StreamProcessorAdapter 适配 proxy.StreamProcessor → handlers.StreamProcessor
type StreamProcessorAdapter struct {
	innerProcessor *StreamProcessor
}

func (spa *StreamProcessorAdapter) ProcessStreamWithRetry(ctx context.Context, resp *http.Response) (*tracking.TokenUsage, string, error) {
	return spa.innerProcessor.ProcessStreamWithRetry(ctx, resp)
}

func (spa *StreamProcessorAdapter) HasSentContent() bool {
	return spa.innerProcessor.HasSentContent()
}

// ErrorRecoveryManagerAdapter 适配 *ErrorRecoveryManager → handlers.ErrorRecoveryManager
type ErrorRecoveryManagerAdapter struct {
	innerManager *ErrorRecoveryManager
}

func (era *ErrorRecoveryManagerAdapter) ClassifyError(err error, connID, endpointName, groupName string, attemptCount int) handlers.ErrorContext {
	ctx := era.innerManager.ClassifyError(err, connID, endpointName, groupName, attemptCount)
	return handlers.ErrorContext{
		RequestID:      ctx.RequestID,
		EndpointName:   ctx.EndpointName,
		GroupName:      ctx.GroupName,
		AttemptCount:   ctx.AttemptCount,
		ErrorType:      handlers.ErrorType(ctx.ErrorType),
		OriginalError:  ctx.OriginalError,
		RetryableAfter: ctx.RetryableAfter,
		MaxRetries:     ctx.MaxRetries,
	}
}

func (era *ErrorRecoveryManagerAdapter) HandleFinalFailure(errorCtx handlers.ErrorContext) {
	innerCtx := ErrorContext{
		RequestID:      errorCtx.RequestID,
		EndpointName:   errorCtx.EndpointName,
		GroupName:      errorCtx.GroupName,
		AttemptCount:   errorCtx.AttemptCount,
		ErrorType:      ErrorType(errorCtx.ErrorType),
		OriginalError:  errorCtx.OriginalError,
		RetryableAfter: errorCtx.RetryableAfter,
		MaxRetries:     errorCtx.MaxRetries,
	}
	era.innerManager.HandleFinalFailure(&innerCtx)
}

func (era *ErrorRecoveryManagerAdapter) GetErrorTypeName(errorType handlers.ErrorType) string {
	return era.innerManager.getErrorTypeName(ErrorType(errorType))
}

// TokenAnalyzerAdapter 适配 *response.TokenAnalyzer → handlers.TokenAnalyzer
type TokenAnalyzerAdapter struct {
	innerAnalyzer *response.TokenAnalyzer
}

func (taa *TokenAnalyzerAdapter) AnalyzeResponseForTokens(ctx context.Context, responseBody, endpointName string, r *http.Request) {
	taa.innerAnalyzer.AnalyzeResponseForTokens(ctx, responseBody, endpointName, r)
}

func (taa *TokenAnalyzerAdapter) AnalyzeResponseForTokensUnified(responseBytes []byte, connID, endpointName string) (*tracking.TokenUsage, string) {
	return taa.innerAnalyzer.AnalyzeResponseForTokensUnified(responseBytes, connID, endpointName)
}

// RetryHandlerAdapter 适配 *RetryHandler → handlers.RetryHandler
type RetryHandlerAdapter struct {
	innerHandler *RetryHandler
}

func (rha *RetryHandlerAdapter) ShouldSuspendRequest(ctx context.Context) bool {
	return rha.innerHandler.shouldSuspendRequest(ctx)
}

func (rha *RetryHandlerAdapter) WaitForGroupSwitch(ctx context.Context, connID string) bool {
	return rha.innerHandler.waitForGroupSwitch(ctx, connID)
}

func (rha *RetryHandlerAdapter) SetEndpointManager(manager any) {
	if em, ok := manager.(*endpoint.Manager); ok {
		rha.innerHandler.SetEndpointManager(em)
	}
}

func (rha *RetryHandlerAdapter) SetUsageTracker(tracker *tracking.UsageTracker) {
	rha.innerHandler.SetUsageTracker(tracker)
}

func (rha *RetryHandlerAdapter) ExecuteWithContext(ctx context.Context, operation func(*endpoint.Endpoint, string) (*http.Response, error), connID string) (*http.Response, error) {
	return rha.innerHandler.ExecuteWithContext(ctx, Operation(operation), connID)
}

// =========================
// Factories
// =========================

type TokenParserFactoryImpl struct{}

func (f *TokenParserFactoryImpl) NewTokenParserWithUsageTracker(connID string, usageTracker *tracking.UsageTracker) handlers.TokenParser {
	innerParser := NewTokenParserWithUsageTracker(connID, usageTracker)
	return &TokenParserAdapter{innerParser: innerParser}
}

type StreamProcessorFactoryImpl struct{}

func (f *StreamProcessorFactoryImpl) NewStreamProcessor(
	tokenParser handlers.TokenParser,
	usageTracker *tracking.UsageTracker,
	w http.ResponseWriter,
	flusher http.Flusher,
	requestID, endpoint string,
) handlers.StreamProcessor {
	// 获取内部的 TokenParser 实例
	var concreteTokenParser *TokenParser
	if adapter, ok := tokenParser.(*TokenParserAdapter); ok {
		concreteTokenParser = adapter.innerParser
	} else {
		// 如果不是适配器类型，创建新的
		concreteTokenParser = NewTokenParserWithUsageTracker(requestID, usageTracker)
	}

	innerProcessor := NewStreamProcessor(concreteTokenParser, usageTracker, w, flusher, requestID, endpoint)
	return &StreamProcessorAdapter{innerProcessor: innerProcessor}
}

type ErrorRecoveryFactoryImpl struct{}

func (f *ErrorRecoveryFactoryImpl) NewErrorRecoveryManager(usageTracker *tracking.UsageTracker) handlers.ErrorRecoveryManager {
	innerManager := NewErrorRecoveryManager(usageTracker)
	return &ErrorRecoveryManagerAdapter{innerManager: innerManager}
}

type RetryManagerFactoryImpl struct {
	config          *config.Config
	errorRecovery   *ErrorRecoveryManager
	endpointManager *endpoint.Manager
}

func (f *RetryManagerFactoryImpl) NewRetryManager() handlers.RetryManager {
	return NewRetryManager(f.config, f.errorRecovery, f.endpointManager)
}

type SuspensionManagerFactoryImpl struct {
	config                *config.Config
	endpointManager       *endpoint.Manager
	recoverySignalManager *EndpointRecoverySignalManager // 🚀 [端点自愈] 恢复信号管理器
}

func (f *SuspensionManagerFactoryImpl) NewSuspensionManager() handlers.SuspensionManager {
	return NewSuspensionManagerWithRecoverySignal(
		f.config,
		f.endpointManager,
		f.endpointManager.GetGroupManager(),
		f.recoverySignalManager,
	)
}
