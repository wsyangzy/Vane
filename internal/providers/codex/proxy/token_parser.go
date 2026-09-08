// token_parser.go - OpenAI 格式 Token 解析器
// 同时支持两种上游响应格式：
//   1. Chat Completions API: 顶层 model + usage{prompt_tokens, completion_tokens, total_tokens}
//   2. Responses API（Codex CLI 使用）: 顶层 model + usage{input_tokens, output_tokens, total_tokens}
//      流式响应将 usage 嵌套在 response.completed 事件的 response 字段中：
//      data: {"type":"response.completed","response":{"model":"...","usage":{...}}}

package codexproxy

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// TokenResult 解析结果
type TokenResult struct {
	InputTokens      int64
	OutputTokens     int64
	ReasoningTokens  int64
	TotalTokens      int64
	CacheReadTokens  int64 // Responses API 中的 cached_tokens（输入命中缓存的部分）
	CacheWriteTokens int64 // GPT-5.6+ cache_write_tokens（写入缓存的输入部分）
	Model            string
	ServiceTier      string
}

// UpstreamStreamError 表示 OpenAI/Codex 以 HTTP 200 SSE 流返回的错误事件。
// 这类错误不应被记录为 completed，否则会出现 token 全 0 的假成功记录。
//
// 刻意不带 StatusCode：这条响应上游给出的真实 HTTP 状态码就是 200，
// 凭 message 里的关键字反推出 429/401/402 写进转移链路，
// 与提供商真正返回过的状态码在追踪里无法区分。重试判断改用下面的 type/code 文本。
type UpstreamStreamError struct {
	Type    string
	Code    string
	Message string
}

func (e *UpstreamStreamError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// TokenParser 解析 OpenAI 格式响应中的 token 用量
type TokenParser struct{}

// NewTokenParser 创建 Token 解析器
func NewTokenParser() *TokenParser {
	return &TokenParser{}
}

// flexibleTokenCount accepts token counters returned either as JSON numbers or
// numeric strings. Some OpenAI-compatible providers use strings in usage details.
type flexibleTokenCount int64

func (c *flexibleTokenCount) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*c = 0
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		*c = flexibleTokenCount(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		*c = 0
		return nil
	}
	parsed, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*c = flexibleTokenCount(parsed)
	return nil
}

func tokenCountValue(v flexibleTokenCount) int64 {
	return int64(v)
}

// openAIInputTokensDetails Responses API 输入 token 详情
type openAIInputTokensDetails struct {
	CachedTokens     flexibleTokenCount `json:"cached_tokens"`
	CacheWriteTokens flexibleTokenCount `json:"cache_write_tokens"`
}

// openAIPromptTokensDetails Chat Completions API 中的提示词 token 详情（部分实现提供）
type openAIPromptTokensDetails struct {
	CachedTokens     flexibleTokenCount `json:"cached_tokens"`
	CacheWriteTokens flexibleTokenCount `json:"cache_write_tokens"`
}

// openAIOutputTokensDetails Responses API 输出 token 详情
type openAIOutputTokensDetails struct {
	ReasoningTokens flexibleTokenCount `json:"reasoning_tokens"`
}

// openAICompletionTokensDetails Chat Completions API 输出 token 详情
type openAICompletionTokensDetails struct {
	ReasoningTokens flexibleTokenCount `json:"reasoning_tokens"`
}

// openAIUsage 同时兼容 Chat Completions / Responses 两种格式
// JSON 中不存在的字段会保持零值，因此可以安全地共存。
type openAIUsage struct {
	// Chat Completions API 字段
	PromptTokens     flexibleTokenCount `json:"prompt_tokens"`
	CompletionTokens flexibleTokenCount `json:"completion_tokens"`
	// Responses API 字段
	InputTokens  flexibleTokenCount `json:"input_tokens"`
	OutputTokens flexibleTokenCount `json:"output_tokens"`
	// 公共字段
	TotalTokens flexibleTokenCount `json:"total_tokens"`
	// 部分兼容服务商会直接把 reasoning_tokens 放在 usage 顶层
	ReasoningTokens flexibleTokenCount `json:"reasoning_tokens"`
	// 缓存命中（Responses API）
	InputTokensDetails *openAIInputTokensDetails `json:"input_tokens_details,omitempty"`
	// 兼容少数服务商的单数拼写
	InputTokenDetails *openAIInputTokensDetails `json:"input_token_details,omitempty"`
	// 输出详情（Responses API）
	OutputTokensDetails *openAIOutputTokensDetails `json:"output_tokens_details,omitempty"`
	// 兼容少数服务商的单数拼写
	OutputTokenDetails *openAIOutputTokensDetails `json:"output_token_details,omitempty"`
	// 缓存命中（部分 Chat Completions API 实现，如 OpenAI 官方）
	PromptTokensDetails *openAIPromptTokensDetails `json:"prompt_tokens_details,omitempty"`
	// 输出详情（部分 Chat Completions API 实现）
	CompletionTokensDetails *openAICompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

// openAIResponseWrapper Responses API SSE 事件中嵌套的响应对象
// 例如：response.completed 事件 → data: {"type":"response.completed","response":{...,"usage":{...}}}
type openAIResponseWrapper struct {
	Model       string       `json:"model"`
	ServiceTier string       `json:"service_tier"`
	Usage       *openAIUsage `json:"usage,omitempty"`
}

// openAIResponse 统一响应结构（同时支持顶层 usage 和嵌套在 response 中的 usage）
type openAIResponse struct {
	Model       string                 `json:"model"`
	ServiceTier string                 `json:"service_tier"`
	Usage       *openAIUsage           `json:"usage,omitempty"`    // Chat Completions API / Responses 非流式
	Response    *openAIResponseWrapper `json:"response,omitempty"` // Responses API SSE 事件包装
}

type openAIStreamErrorEnvelope struct {
	Type     string                 `json:"type"`
	Error    json.RawMessage        `json:"error,omitempty"`
	Response *openAIStreamErrorBody `json:"response,omitempty"`
}

type openAIStreamErrorBody struct {
	Status string          `json:"status"`
	Error  json.RawMessage `json:"error,omitempty"`
}

type openAIStreamErrorDetail struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func parseStreamErrorDetail(raw json.RawMessage) *openAIStreamErrorDetail {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var message string
	if err := json.Unmarshal(raw, &message); err == nil {
		message = strings.TrimSpace(message)
		if message == "" {
			return nil
		}
		return &openAIStreamErrorDetail{Message: message}
	}
	var detail openAIStreamErrorDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return nil
	}
	detail.Type = strings.TrimSpace(detail.Type)
	detail.Code = strings.TrimSpace(detail.Code)
	detail.Message = strings.TrimSpace(detail.Message)
	if detail.Type == "" && detail.Code == "" && detail.Message == "" {
		return nil
	}
	return &detail
}

// resolveUsage 从 usage 中提取统一的 token 字段
// Responses API 字段优先；如果都为 0 则回退到 Chat Completions 字段
func resolveUsage(u *openAIUsage) (input, output, cacheRead, cacheWrite, reasoning int64) {
	if u == nil {
		return 0, 0, 0, 0, 0
	}
	// 优先使用 Responses API 字段
	if u.InputTokens > 0 || u.OutputTokens > 0 {
		input = tokenCountValue(u.InputTokens)
		output = tokenCountValue(u.OutputTokens)
	} else {
		input = tokenCountValue(u.PromptTokens)
		output = tokenCountValue(u.CompletionTokens)
	}
	// 缓存命中：优先 Responses API 字段
	if u.InputTokensDetails != nil && u.InputTokensDetails.CachedTokens > 0 {
		cacheRead = tokenCountValue(u.InputTokensDetails.CachedTokens)
	} else if u.InputTokenDetails != nil && u.InputTokenDetails.CachedTokens > 0 {
		cacheRead = tokenCountValue(u.InputTokenDetails.CachedTokens)
	} else if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		cacheRead = tokenCountValue(u.PromptTokensDetails.CachedTokens)
	}
	// 缓存写入：与缓存读取相同，优先 Responses API 字段。
	if u.InputTokensDetails != nil && u.InputTokensDetails.CacheWriteTokens > 0 {
		cacheWrite = tokenCountValue(u.InputTokensDetails.CacheWriteTokens)
	} else if u.InputTokenDetails != nil && u.InputTokenDetails.CacheWriteTokens > 0 {
		cacheWrite = tokenCountValue(u.InputTokenDetails.CacheWriteTokens)
	} else if u.PromptTokensDetails != nil && u.PromptTokensDetails.CacheWriteTokens > 0 {
		cacheWrite = tokenCountValue(u.PromptTokensDetails.CacheWriteTokens)
	}
	if u.OutputTokensDetails != nil && u.OutputTokensDetails.ReasoningTokens > 0 {
		reasoning = tokenCountValue(u.OutputTokensDetails.ReasoningTokens)
	} else if u.OutputTokenDetails != nil && u.OutputTokenDetails.ReasoningTokens > 0 {
		reasoning = tokenCountValue(u.OutputTokenDetails.ReasoningTokens)
	} else if u.CompletionTokensDetails != nil && u.CompletionTokensDetails.ReasoningTokens > 0 {
		reasoning = tokenCountValue(u.CompletionTokensDetails.ReasoningTokens)
	} else if u.ReasoningTokens > 0 {
		reasoning = tokenCountValue(u.ReasoningTokens)
	}
	return
}

// buildResult 根据 model + usage 构造 TokenResult；如果 usage 为空返回 nil
func buildResult(model, serviceTier string, usage *openAIUsage) *TokenResult {
	if usage == nil {
		return nil
	}
	input, output, cacheRead, cacheWrite, reasoning := resolveUsage(usage)
	// 容错：如果两种字段都未提供任何 token，视为无效（避免污染数据）
	if input == 0 && output == 0 && cacheRead == 0 && cacheWrite == 0 && reasoning == 0 && usage.TotalTokens == 0 {
		return nil
	}
	total := tokenCountValue(usage.TotalTokens)
	if total == 0 {
		total = input + output
	}
	return &TokenResult{
		InputTokens:      input,
		OutputTokens:     output,
		ReasoningTokens:  reasoning,
		TotalTokens:      total,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
		Model:            model,
		ServiceTier:      strings.ToLower(strings.TrimSpace(serviceTier)),
	}
}

// ParseNonStreaming 从非流式响应 body 中提取 token
// 同时支持 Chat Completions / Responses API（顶层 usage 或嵌套 response.usage）
func (p *TokenParser) ParseNonStreaming(body []byte) *TokenResult {
	var resp openAIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	usage := resp.Usage
	model := resp.Model
	serviceTier := resp.ServiceTier
	// 嵌套 response 也可能补充顶层未返回的 model/service_tier。
	if resp.Response != nil {
		if usage == nil {
			usage = resp.Response.Usage
		}
		if model == "" {
			model = resp.Response.Model
		}
		if serviceTier == "" {
			serviceTier = resp.Response.ServiceTier
		}
	}
	return buildResult(model, serviceTier, usage)
}

// requestModelOnly 仅提取请求/响应中的 model 字段（用于早期识别）
type requestModelOnly struct {
	Model string `json:"model"`
}

// ParseModelFromBody 从请求体或响应体顶层提取 model 字段
// 用于在响应未携带 model 时填充模型名；返回空字符串表示无法解析
func (p *TokenParser) ParseModelFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var m requestModelOnly
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	return m.Model
}

var (
	sseDataPrefix = []byte("data:")
	sseDoneMarker = []byte("[DONE]")
	usageKeyHint  = []byte(`"usage"`)
)

// ParseStreamingLine 从流式 SSE 行中提取 token（如果有）
// 流结束标记: data: [DONE]
// 同时支持：
//   - Chat Completions API: data: {"id":"...","model":"...","usage":{...}}
//   - Responses API: data: {"type":"response.completed","response":{"model":"...","usage":{...}}}
func (p *TokenParser) ParseStreamingLine(line []byte) (result *TokenResult, isDone bool) {
	trimmed := bytes.TrimSpace(line)

	// 仅处理 data: 开头的行
	if !bytes.HasPrefix(trimmed, sseDataPrefix) {
		return nil, false
	}

	payload := bytes.TrimSpace(trimmed[len(sseDataPrefix):])
	if bytes.Equal(payload, sseDoneMarker) {
		return nil, true
	}
	if len(payload) == 0 {
		return nil, false
	}

	// 快速路径：如果不包含 usage 字段，直接跳过 JSON 解析
	if !bytes.Contains(payload, usageKeyHint) {
		return nil, false
	}

	var resp openAIResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, false
	}

	// 优先使用顶层 usage（Chat Completions），其次尝试嵌套 response.usage（Responses API）
	usage := resp.Usage
	model := resp.Model
	serviceTier := resp.ServiceTier
	if resp.Response != nil {
		if usage == nil {
			usage = resp.Response.Usage
		}
		if model == "" {
			model = resp.Response.Model
		}
		if serviceTier == "" {
			serviceTier = resp.Response.ServiceTier
		}
	}

	return buildResult(model, serviceTier, usage), false
}

// ParseStreamingErrorLine 从 SSE data 行中提取 OpenAI/Codex 错误事件。
// 支持：
//   - data: {"type":"response.failed","response":{"error":{"message":"..."}}}
//   - data: {"type":"error","error":{"type":"server_error","message":"..."}}
//   - data: {"error":{"message":"..."}}
func (p *TokenParser) ParseStreamingErrorLine(line []byte) *UpstreamStreamError {
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, sseDataPrefix) {
		return nil
	}
	payload := bytes.TrimSpace(trimmed[len(sseDataPrefix):])
	if len(payload) == 0 || bytes.Equal(payload, sseDoneMarker) {
		return nil
	}
	if !bytes.Contains(payload, []byte(`"error"`)) &&
		!bytes.Contains(payload, []byte(`"response.failed"`)) {
		return nil
	}

	var envelope openAIStreamErrorEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil
	}

	detail := parseStreamErrorDetail(envelope.Error)
	if detail == nil && envelope.Response != nil {
		detail = parseStreamErrorDetail(envelope.Response.Error)
	}
	if detail == nil && !strings.Contains(strings.ToLower(envelope.Type), "failed") {
		return nil
	}
	if detail == nil {
		detail = &openAIStreamErrorDetail{Message: "upstream stream failed"}
	}

	return &UpstreamStreamError{
		Type:    firstNonEmpty(detail.Type, envelope.Type),
		Code:    detail.Code,
		Message: string(payload),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
