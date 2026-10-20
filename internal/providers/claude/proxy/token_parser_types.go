package proxy

import (
	"github.com/wsyangzy/Vane/internal/tracking"
)

// ParseResult 解析结果结构体
// 用于将Token解析与状态记录分离，支持职责纯化
type ParseResult struct {
	TokenUsage  *tracking.TokenUsage
	ModelName   string
	ErrorInfo   *ErrorInfo
	RawError    string // error SSE data exactly as received from the provider
	IsCompleted bool
	Status      string
}

// ErrorInfo 错误信息结构体
type ErrorInfo struct {
	Type    string
	Message string
}

// TokenParserInterface 统一的Token解析接口
// 根据STREAMING_REFACTOR_PROPOSAL.md方案设计
type TokenParserInterface interface {
	ParseMessageStart(line string) *ModelInfo
	ParseMessageDelta(line string) *tracking.TokenUsage
	SetModel(modelName string)
	GetFinalUsage() *tracking.TokenUsage
	Reset()

	// ParseSSEEvent 解析 SSE 事件行并返回结构化结果
	ParseSSEEvent(line string) *ParseResult
}

// ModelInfo 模型信息结构体
type ModelInfo struct {
	Model string `json:"model"`
}

// UsageData 表示Claude API SSE事件中的usage字段
type UsageData struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"` // 总数（向后兼容）
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	// 分开的缓存创建详情
	CacheCreation *CacheCreationDetail `json:"cache_creation,omitempty"`
}

// CacheCreationDetail 表示缓存创建详情（分开的 5m/1h）
type CacheCreationDetail struct {
	Ephemeral5mInputTokens int64 `json:"ephemeral_5m_input_tokens"` // 5分钟缓存 tokens
	Ephemeral1hInputTokens int64 `json:"ephemeral_1h_input_tokens"` // 1小时缓存 tokens
}

// MessageStartData 表示message_start事件中的message对象
type MessageStartData struct {
	ID      string        `json:"id"`
	Type    string        `json:"type"`
	Role    string        `json:"role"`
	Model   string        `json:"model"`
	Content []interface{} `json:"content"`
	Usage   *UsageData    `json:"usage,omitempty"`
}

// MessageStart 表示message_start事件的结构
type MessageStart struct {
	Type    string            `json:"type"`
	Message *MessageStartData `json:"message"`
}

// MessageDelta 表示message_delta事件的结构
type MessageDelta struct {
	Type  string      `json:"type"`
	Delta interface{} `json:"delta"`
	Usage *UsageData  `json:"usage,omitempty"`
}

// SSEErrorData 表示SSE流中error事件的结构
type SSEErrorData struct {
	Type  string `json:"type"`
	Error struct {
		Type      string `json:"type"`
		Message   string `json:"message"`
		RequestID string `json:"request_id,omitempty"`
	} `json:"error"`
}

// 请求处理状态常量
const (
	StatusCompleted    = "completed"     // 真正成功（有Token或正常响应）
	StatusErrorAPI     = "error_api"     // API层错误（overloaded等）
	StatusErrorNetwork = "error_network" // 网络层错误（超时等）
	StatusProcessing   = "processing"    // 处理中
)
