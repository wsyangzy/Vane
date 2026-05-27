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
)

// TokenResult 解析结果
type TokenResult struct {
	InputTokens     int64
	OutputTokens    int64
	TotalTokens     int64
	CacheReadTokens int64 // Responses API 中的 cached_tokens（输入命中缓存的部分）
	Model           string
}

// TokenParser 解析 OpenAI 格式响应中的 token 用量
type TokenParser struct{}

// NewTokenParser 创建 Token 解析器
func NewTokenParser() *TokenParser {
	return &TokenParser{}
}

// openAIInputTokensDetails Responses API 输入 token 详情
type openAIInputTokensDetails struct {
	CachedTokens int64 `json:"cached_tokens"`
}

// openAIPromptTokensDetails Chat Completions API 中的提示词 token 详情（部分实现提供）
type openAIPromptTokensDetails struct {
	CachedTokens int64 `json:"cached_tokens"`
}

// openAIUsage 同时兼容 Chat Completions / Responses 两种格式
// JSON 中不存在的字段会保持零值，因此可以安全地共存。
type openAIUsage struct {
	// Chat Completions API 字段
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	// Responses API 字段
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	// 公共字段
	TotalTokens int64 `json:"total_tokens"`
	// 缓存命中（Responses API）
	InputTokensDetails *openAIInputTokensDetails `json:"input_tokens_details,omitempty"`
	// 缓存命中（部分 Chat Completions API 实现，如 OpenAI 官方）
	PromptTokensDetails *openAIPromptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

// openAIResponseWrapper Responses API SSE 事件中嵌套的响应对象
// 例如：response.completed 事件 → data: {"type":"response.completed","response":{...,"usage":{...}}}
type openAIResponseWrapper struct {
	Model string       `json:"model"`
	Usage *openAIUsage `json:"usage,omitempty"`
}

// openAIResponse 统一响应结构（同时支持顶层 usage 和嵌套在 response 中的 usage）
type openAIResponse struct {
	Model    string                 `json:"model"`
	Usage    *openAIUsage           `json:"usage,omitempty"`    // Chat Completions API / Responses 非流式
	Response *openAIResponseWrapper `json:"response,omitempty"` // Responses API SSE 事件包装
}

// resolveUsage 从 usage 中提取统一的 token 字段
// Responses API 字段优先；如果都为 0 则回退到 Chat Completions 字段
func resolveUsage(u *openAIUsage) (input, output, cacheRead int64) {
	if u == nil {
		return 0, 0, 0
	}
	// 优先使用 Responses API 字段
	if u.InputTokens > 0 || u.OutputTokens > 0 {
		input = u.InputTokens
		output = u.OutputTokens
	} else {
		input = u.PromptTokens
		output = u.CompletionTokens
	}
	// 缓存命中：优先 Responses API 字段
	if u.InputTokensDetails != nil && u.InputTokensDetails.CachedTokens > 0 {
		cacheRead = u.InputTokensDetails.CachedTokens
	} else if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		cacheRead = u.PromptTokensDetails.CachedTokens
	}
	return
}

// buildResult 根据 model + usage 构造 TokenResult；如果 usage 为空返回 nil
func buildResult(model string, usage *openAIUsage) *TokenResult {
	if usage == nil {
		return nil
	}
	input, output, cacheRead := resolveUsage(usage)
	// 容错：如果两种字段都未提供任何 token，视为无效（避免污染数据）
	if input == 0 && output == 0 && cacheRead == 0 && usage.TotalTokens == 0 {
		return nil
	}
	total := usage.TotalTokens
	if total == 0 {
		total = input + output
	}
	return &TokenResult{
		InputTokens:     input,
		OutputTokens:    output,
		TotalTokens:     total,
		CacheReadTokens: cacheRead,
		Model:           model,
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
	// 如果顶层 usage 缺失，尝试从嵌套 response 中读取（极少见，但保持一致性）
	if usage == nil && resp.Response != nil {
		usage = resp.Response.Usage
		if model == "" {
			model = resp.Response.Model
		}
	}
	return buildResult(model, usage)
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
	sseDataPrefix = []byte("data: ")
	sseDoneMarker = []byte("data: [DONE]")
	usageKeyHint  = []byte(`"usage"`)
)

// ParseStreamingLine 从流式 SSE 行中提取 token（如果有）
// 流结束标记: data: [DONE]
// 同时支持：
//   - Chat Completions API: data: {"id":"...","model":"...","usage":{...}}
//   - Responses API: data: {"type":"response.completed","response":{"model":"...","usage":{...}}}
func (p *TokenParser) ParseStreamingLine(line []byte) (result *TokenResult, isDone bool) {
	trimmed := bytes.TrimSpace(line)

	// 检查流结束标记
	if bytes.Equal(trimmed, sseDoneMarker) {
		return nil, true
	}

	// 仅处理 data: 开头的行
	if !bytes.HasPrefix(trimmed, sseDataPrefix) {
		return nil, false
	}

	payload := trimmed[len(sseDataPrefix):]
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
	if usage == nil && resp.Response != nil {
		usage = resp.Response.Usage
		if model == "" {
			model = resp.Response.Model
		}
	}

	return buildResult(model, usage), false
}
