package proxy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// 非流式请求正文摘要：只在 debug 级别输出，用来辨认 Claude Code 后台调用的用途。
//
// 反代不落库请求正文，追踪库只能看到「非流式、thinking disabled、主模型」这种形态，
// 无法区分是标题生成、摘要还是探测。这里把结构（max_tokens、消息数、system 与
// 末条 user 消息的开头）写进本地日志，看一轮就能对上号。只进 slog，不进追踪库、
// 不经 Wails 回传，与隐私脱敏的红线一致。

const (
	nonStreamSummaryTextLimit = 300
)

// summarizeNonStreamRequest 生成一行可读摘要；正文不是 JSON 对象时返回长度信息。
func summarizeNonStreamRequest(body []byte) string {
	var req struct {
		Model     string          `json:"model"`
		MaxTokens int             `json:"max_tokens"`
		System    json.RawMessage `json:"system"`
		Messages  []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools    []json.RawMessage `json:"tools"`
		Metadata json.RawMessage   `json:"metadata"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return fmt.Sprintf("body_len=%d (not a JSON object)", len(body))
	}

	lastUser := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			lastUser = textPreview(req.Messages[i].Content)
			break
		}
	}

	return fmt.Sprintf("model=%s max_tokens=%d messages=%d tools=%d metadata=%s system=%q last_user=%q",
		req.Model, req.MaxTokens, len(req.Messages), len(req.Tools),
		compactRaw(req.Metadata), textPreview(req.System), lastUser)
}

// textPreview 从 string 或 content block 数组里抽出文本开头。
func textPreview(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return clip(text)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return clip(string(raw))
	}
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			if sb.Len() > 0 {
				sb.WriteString(" | ")
			}
			sb.WriteString(b.Text)
		} else if b.Type != "" && b.Type != "text" {
			sb.WriteString("<" + b.Type + ">")
		}
		if sb.Len() > nonStreamSummaryTextLimit {
			break
		}
	}
	return clip(sb.String())
}

func compactRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "-"
	}
	return clip(string(raw))
}

func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= nonStreamSummaryTextLimit {
		return s
	}
	return string(runes[:nonStreamSummaryTextLimit]) + "…"
}

func logNonStreamRequestSummary(connID string, body []byte) {
	if !slog.Default().Enabled(nil, slog.LevelDebug) {
		return
	}
	slog.Debug(fmt.Sprintf("🔎 [非流请求摘要] [%s] %s", connID, summarizeNonStreamRequest(body)))
}
