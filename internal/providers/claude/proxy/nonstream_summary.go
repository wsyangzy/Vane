package proxy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// 非流式请求结构摘要：只在 debug 级别输出，用来辨认 Claude Code 后台调用的用途。
//
// 反代不落库请求正文，追踪库只能看到「非流式、thinking disabled、主模型」这种形态，
// 无法区分是标题生成、摘要还是探测。这里只写结构（max_tokens、消息数与角色序列、
// 各内容块的类型与字符数、system 长度、metadata 的键名），足以对上号。
//
// 刻意不输出任何文本内容与 metadata 的值：本函数在隐私脱敏之前执行，
// 而日志会广播到前端日志查看器并写入日志文件——哪怕截断的 300 字原文，
// 也等于把客户对话绕过隐私防护又落了一份盘。
func summarizeNonStreamRequest(body []byte) string {
	var req struct {
		Model     string          `json:"model"`
		MaxTokens int             `json:"max_tokens"`
		System    json.RawMessage `json:"system"`
		Messages  []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools    []json.RawMessage          `json:"tools"`
		Metadata map[string]json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return fmt.Sprintf("body_len=%d (not a JSON object)", len(body))
	}

	roles := make([]string, 0, len(req.Messages))
	for _, msg := range req.Messages {
		roles = append(roles, msg.Role)
	}
	lastUser := "-"
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			lastUser = contentShape(req.Messages[i].Content)
			break
		}
	}

	return fmt.Sprintf("model=%s max_tokens=%d messages=%d roles=%s tools=%d metadata_keys=%s system=%s last_user=%s",
		req.Model, req.MaxTokens, len(req.Messages), strings.Join(roles, ","), len(req.Tools),
		metadataKeys(req.Metadata), contentShape(req.System), lastUser)
}

// contentShape 描述 string 或 content block 数组的结构：字符串给出字符数，
// 数组给出每个块的类型与其文本字符数，如 "[text:120,tool_result]"。不含任何文本内容。
func contentShape(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "-"
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return fmt.Sprintf("text:%d", len([]rune(text)))
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return fmt.Sprintf("raw_len=%d", len(raw))
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		kind := block.Type
		if kind == "" {
			kind = "?"
		}
		if block.Text != "" {
			parts = append(parts, fmt.Sprintf("%s:%d", kind, len([]rune(block.Text))))
		} else {
			parts = append(parts, kind)
		}
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// metadataKeys 只列键名：metadata.user_id 等值是客户端标识，不进日志
func metadataKeys(metadata map[string]json.RawMessage) string {
	if len(metadata) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys) // 固定顺序，便于日志比对
	return strings.Join(keys, ",")
}

func logNonStreamRequestSummary(connID string, body []byte) {
	if !slog.Default().Enabled(nil, slog.LevelDebug) {
		return
	}
	slog.Debug(fmt.Sprintf("🔎 [非流请求摘要] [%s] %s", connID, summarizeNonStreamRequest(body)))
}
