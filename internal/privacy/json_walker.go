package privacy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// textSegment body 中一个可扫描的文本字段。
// Start/End 是原始 JSON 字符串 token（含双引号）的字节区间，Value 是解码后的字符串。
// 对非 JSON 文本，Start/End 覆盖整个 body，IsJSON 置 false。
type textSegment struct {
	Start  int // 含
	End    int // 不含
	Value  string
	IsJSON bool // true 表示区间是 JSON 字符串 token，替换时需重新 escape
}

// pathElem JSON 路径元素：Key 为对象键；Index >= 0 时为数组下标。
type pathElem struct {
	Key   string
	Index int
}

// segmentMatcher 判断给定 JSON 路径上的字符串值是否属于可扫描文本字段
type segmentMatcher func(path []pathElem) bool

// walkerForPath 返回请求路径对应的字段匹配器；不支持的路径返回 nil。
//
// 扫描范围是「全部字符串值，排除协议字段」：早期的文本字段白名单漏掉了
// tool_use.input、function_call.arguments、thinking、文档正文等——在编程场景里
// 这些恰恰是客户数据的主要载体，走三方中转时等于裸奔。
// 协议字段（模型名、类型标记、ID、签名、密文、JSON Schema 等）由 isProtocolStringPath
// 排除：改写它们会破坏请求语义，或被上游当作篡改（签名校验、指纹检测）。
//
// 未覆盖路径仍由 Settings.UnsupportedPathAction 显式暴露给用户，不做静默跳过。
func walkerForPath(requestPath string) segmentMatcher {
	switch normalizeScanPath(requestPath) {
	case "/internal/routing/classify":
		return func(path []pathElem) bool {
			if len(path) != 1 || path[0].Index >= 0 {
				return false
			}
			switch path[0].Key {
			case "current_user_message", "previous_user_message", "previous_assistant_text":
				return true
			default:
				return false
			}
		}
	case "/v1/messages", "/v1/messages/count_tokens":
		return matchClaudeMessagesTextPath
	case "/v1/responses", "/v1/responses/compact":
		return matchCodexResponsesTextPath
	case "/v1/chat/completions":
		return matchChatCompletionsTextPath
	default:
		return nil
	}
}

// normalizeScanPath 把客户端的等价路径写法归一：Codex CLI 直连时不带 /v1 前缀，
// 查询串不参与路径匹配。
func normalizeScanPath(requestPath string) string {
	if idx := strings.IndexByte(requestPath, '?'); idx >= 0 {
		requestPath = requestPath[:idx]
	}
	requestPath = strings.TrimRight(requestPath, "/")
	switch requestPath {
	case "/responses", "/responses/compact", "/chat/completions":
		return "/v1" + requestPath
	}
	return requestPath
}

// IsScannablePath 报告该请求路径是否有对应的字段白名单
func IsScannablePath(requestPath string) bool {
	return walkerForPath(requestPath) != nil
}

// 顶层协议字段：整棵子树不扫描。
//   - model/stream/max_tokens 等是请求参数；
//   - tools / tool_choice 是工具定义（名称、描述、JSON Schema），改写会让模型调用错工具；
//   - metadata 含客户端标识（user_id 等），部分上游据此做会话关联与风控，改写等于伪造身份；
//   - reasoning / thinking / text / response_format 等是生成配置；
//   - mcp_servers / container 含连接配置；
//   - previous_response_id / prompt_cache_key 等是会话引用。
var protocolTopLevelKeys = map[string]bool{
	"model": true, "stream": true, "stream_options": true, "max_tokens": true, "max_output_tokens": true,
	"max_completion_tokens": true, "temperature": true, "top_p": true, "top_k": true, "stop": true,
	"stop_sequences": true, "n": true, "seed": true, "logit_bias": true, "logprobs": true, "top_logprobs": true,
	"presence_penalty": true, "frequency_penalty": true, "user": true,
	"tools": true, "tool_choice": true, "parallel_tool_calls": true, "functions": true, "function_call": true,
	"metadata": true, "reasoning": true, "reasoning_effort": true, "thinking": true, "text": true,
	"response_format": true, "modalities": true, "audio": true, "service_tier": true, "store": true,
	"include": true, "truncation": true, "background": true, "previous_response_id": true,
	"prompt_cache_key": true, "safety_identifier": true, "container": true, "mcp_servers": true,
	"betas": true, "context_management": true, "prompt": true,
}

// 任意层级的协议键：值是类型标记、ID、签名、密文或二进制载荷。
//   - signature / encrypted_content / redacted_thinking.data 由上游签发，改写会导致签名校验失败甚至风控；
//   - data 是图片、PDF 的 base64，不是可读文本，扫描只会误伤；
//   - *_id / id / call_id 是会话内引用，改写会断开工具调用与结果的配对；
//   - media_type / mime_type / type / role / status / name 是结构标记。
var protocolAnyLevelKeys = map[string]bool{
	"type": true, "role": true, "id": true, "call_id": true, "tool_use_id": true, "tool_call_id": true,
	"name": true, "status": true, "signature": true, "encrypted_content": true, "data": true,
	"namespace": true, "server_label": true, "server_name": true, "thoughtSignature": true, "thought_signature": true,
	"media_type": true, "mime_type": true, "cache_control": true, "file_id": true, "image_url": true,
	"url": true, "detail": true, "format": true, "citations": true, "phase": true,
}

// isProtocolStringPath 判断字符串值是否位于协议字段内（不扫描）。
//
// 判定只看键名：同名键在 Claude 与 Codex 两种协议里语义一致。
// 数组下标不参与判定，因此 messages[3].content[0].id 与 input[0].id 同样被排除。
func isProtocolStringPath(path []pathElem) bool {
	if len(path) == 0 {
		return true
	}
	if path[0].Index < 0 && protocolTopLevelKeys[path[0].Key] {
		return true
	}
	if isToolArgumentPath(path) {
		return false
	}
	for _, elem := range path {
		if elem.Index < 0 && protocolAnyLevelKeys[elem.Key] {
			return true
		}
	}
	return false
}

// Tool input keys belong to the user's data, even when named id/name/url.
func isToolArgumentPath(path []pathElem) bool {
	for i, elem := range path {
		if i > 1 && elem.Index < 0 && (elem.Key == "input" || elem.Key == "args" || elem.Key == "arguments") {
			return true
		}
	}
	return false
}

// matchClaudeMessagesTextPath 扫描 Claude /v1/messages 的 system 与 messages 下全部非协议字符串：
// 文本块、tool_use.input 的全部字符串叶子、tool_result（字符串或嵌套块）、
// 文档 source.type=text 的正文与 content[] 块、document 的 title / context、搜索结果正文等。
// 顶层其它字段（tools、metadata 等）一律不扫。
//
// thinking 块的正文不扫：上游用 signature 校验 thinking 原文，改写一个字节都会被拒，
// 订阅透传场景下更可能被判为篡改。thinking 是模型自己的输出，隐私数据的源头仍在
// 用户消息与工具结果里，那里已经被脱敏。
func matchClaudeMessagesTextPath(path []pathElem) bool {
	if len(path) == 0 || path[0].Index >= 0 {
		return false
	}
	if path[0].Key != "system" && path[0].Key != "messages" {
		return false
	}
	if last := path[len(path)-1]; last.Index < 0 && last.Key == "thinking" && !isToolArgumentPath(path) {
		return false
	}
	return !isProtocolStringPath(path)
}

// matchCodexResponsesTextPath 扫描 Codex /v1/responses 的 instructions 与 input 下全部非协议字符串：
// 消息文本、function_call.arguments（JSON 字符串，按文本整体扫描）、custom_tool_call.input、
// function_call_output 的字符串或数组输出、推理摘要 summary[].text、local_shell_call 命令等。
func matchCodexResponsesTextPath(path []pathElem) bool {
	if len(path) == 0 || path[0].Index >= 0 {
		return false
	}
	if path[0].Key != "instructions" && path[0].Key != "input" {
		return false
	}
	return !isProtocolStringPath(path)
}

// matchChatCompletionsTextPath OpenAI 兼容 /v1/chat/completions：扫描 messages 下全部非协议字符串，
// 含 content（字符串或数组）、tool_calls[].function.arguments、tool 消息结果。
func matchChatCompletionsTextPath(path []pathElem) bool {
	if len(path) == 0 || path[0].Index >= 0 || path[0].Key != "messages" {
		return false
	}
	// function.name 已由 name 排除；arguments 是 JSON 字符串，按文本扫描
	return !isProtocolStringPath(path)
}

// collectJSONTextSegments 以 offset-aware 方式收集 body 中可扫描文本字段。
//
// 只记录字符串 token 的原始字节区间，绝不构建 map[string]any 再 Marshal——
// 后者会重排对象键、把尖括号与和号转义成 Unicode 形式并改变数字格式，
// 等于伪造了客户端的原始请求，可能触发上游的客户端指纹检测。
func collectJSONTextSegments(body []byte, match segmentMatcher) ([]textSegment, error) {
	segments, _, err := walkJSONTextSegments(body, match, false)
	return segments, err
}

// collectJSONTextSegmentsWithPaths 同 collectJSONTextSegments，另返回每段的 JSON 路径（测试面板展示用）
func collectJSONTextSegmentsWithPaths(body []byte, match segmentMatcher) ([]textSegment, []string, error) {
	return walkJSONTextSegments(body, match, true)
}

func formatJSONPath(path []pathElem) string {
	var b strings.Builder
	for i, elem := range path {
		if elem.Index >= 0 {
			b.WriteString(elem.String())
			continue
		}
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(elem.Key)
	}
	return b.String()
}

func walkJSONTextSegments(body []byte, match segmentMatcher, withPaths bool) ([]textSegment, []string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	type frame struct {
		isObject  bool
		expectKey bool
		key       string
		index     int
	}

	var (
		stack    []*frame
		segments []textSegment
		paths    []string
	)
	prevOffset := int64(0)

	currentPath := func() []pathElem {
		path := make([]pathElem, 0, len(stack))
		for _, f := range stack {
			if f.isObject {
				path = append(path, pathElem{Key: f.key, Index: -1})
			} else {
				path = append(path, pathElem{Index: f.index})
			}
		}
		return path
	}

	// 值结束后推进所在容器的游标
	advanceContainer := func() {
		if len(stack) == 0 {
			return
		}
		top := stack[len(stack)-1]
		if top.isObject {
			top.expectKey = true
		} else {
			top.index++
		}
	}

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			// 截断的 JSON（容器未闭合）Token() 也可能返回干净 EOF，显式校验
			if len(stack) != 0 {
				return nil, nil, fmt.Errorf("invalid json body: unexpected end of input")
			}
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("invalid json body: %w", err)
		}
		offset := decoder.InputOffset()

		switch tok := token.(type) {
		case json.Delim:
			switch tok {
			case '{':
				stack = append(stack, &frame{isObject: true, expectKey: true})
			case '[':
				stack = append(stack, &frame{isObject: false, index: 0})
			case '}', ']':
				stack = stack[:len(stack)-1]
				advanceContainer()
			}
		case string:
			var top *frame
			if len(stack) > 0 {
				top = stack[len(stack)-1]
			}
			if top != nil && top.isObject && top.expectKey {
				top.key = tok
				top.expectKey = false
				prevOffset = offset
				continue
			}
			// 字符串值：定位 token 原始字节区间。前一 token 末尾到当前末尾之间
			// 只可能有空白/逗号/冒号，因此首个双引号即 token 起点。
			if match != nil {
				path := currentPath()
				// Encoded media remains opaque even inside tool arguments.
				binary := strings.HasPrefix(tok, "data:") || len(path) > 1 && path[len(path)-1].Key == "data" && path[len(path)-2].Key == "source"
				if !binary && match(path) {
					gap := body[prevOffset:offset]
					quote := bytes.IndexByte(gap, '"')
					if quote < 0 {
						return nil, nil, fmt.Errorf("failed to locate string token at offset %d", prevOffset)
					}
					segments = append(segments, textSegment{
						Start:  int(prevOffset) + quote,
						End:    int(offset),
						Value:  tok,
						IsJSON: true,
					})
					if withPaths {
						paths = append(paths, formatJSONPath(path))
					}
				}
			}
			advanceContainer()
		default:
			// number/bool/null 值
			advanceContainer()
		}
		prevOffset = offset
	}

	return segments, paths, nil
}

// appendJSONStringBody 按 JSON string 规则 escape 并追加（不含首尾引号）。
// 刻意不对 < > & 做 HTML escape：encoding/json 的默认行为会改变上游收到的
// 字节形态，与「忠实透传」冲突。
func appendJSONStringBody(dst []byte, s string) []byte {
	for i := 0; i < len(s); {
		b := s[i]
		if b < 0x20 {
			switch b {
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = fmt.Appendf(dst, "\\u%04x", b)
			}
			i++
			continue
		}
		if b == '"' || b == '\\' {
			dst = append(dst, '\\', b)
			i++
			continue
		}
		if b < utf8.RuneSelf {
			dst = append(dst, b)
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// 无效 UTF-8 字节按 U+FFFD 输出，避免产生非法 JSON
			dst = append(dst, []byte("�")...)
			i++
			continue
		}
		dst = append(dst, s[i:i+size]...)
		i += size
	}
	return dst
}

// encodeJSONString 返回带引号的 JSON 字符串 token
func encodeJSONString(s string) []byte {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '"')
	out = appendJSONStringBody(out, s)
	out = append(out, '"')
	return out
}

// replaceSegments 按段替换重建 body。replacements 的 key 是段下标，
// value 是该段替换后的解码字符串；未替换的段保持原始字节。
func replaceSegments(body []byte, segments []textSegment, replacements map[int]string) []byte {
	if len(replacements) == 0 {
		return body
	}
	out := make([]byte, 0, len(body)+64)
	cursor := 0
	for i, seg := range segments {
		replaced, ok := replacements[i]
		if !ok {
			continue
		}
		out = append(out, body[cursor:seg.Start]...)
		if seg.IsJSON {
			out = append(out, encodeJSONString(replaced)...)
		} else {
			out = append(out, replaced...)
		}
		cursor = seg.End
	}
	out = append(out, body[cursor:]...)
	return out
}

func (p pathElem) String() string {
	if p.Index >= 0 {
		return "[" + strconv.Itoa(p.Index) + "]"
	}
	return p.Key
}
