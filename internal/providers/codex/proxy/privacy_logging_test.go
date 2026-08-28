package codexproxy

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// 隐私日志只允许输出命中摘要。命中原文与请求正文都不得出现：
// 日志会广播到前端日志查看器并落盘，正文进来等于把整段客户对话又存了一份。
func TestHandlerRecordPrivacyRedactionLogsSummaryWithoutPlaintext(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler := &Handler{logger: logger.With("provider", "codex")}

	summary := `{"changed":true,"action":"redact","hit_count":1,"hits":[{"rule_name":"api key rule","count":1}]}`
	handler.recordPrivacyRedaction("test-codex-privacy-log", summary)

	output := logs.String()
	for _, expected := range []string{
		"[隐私防护] 请求正文已脱敏",
		"test-codex-privacy-log",
		"privacy_redaction_summary",
		"api key rule",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected debug log to contain %q, got %s", expected, output)
		}
	}
	for _, forbidden := range []string{
		"sk-body-api-key",           // 命中原文
		"privacy_redaction_matches", // 明细通道
		"privacy_redacted_body",     // 请求正文
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("privacy log must never contain %q, got %s", forbidden, output)
		}
	}
}

func TestHTTPHandlerUpdateLoggerSuppressesPrivacyDebugAtInfoLevel(t *testing.T) {
	var debugLogs bytes.Buffer
	debugLogger := slog.New(slog.NewTextHandler(&debugLogs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler := NewHTTPHandler(nil, ServerConfig{}, debugLogger, nil)

	summary := `{"changed":true,"action":"redact","hit_count":1,"hits":[{"rule_name":"api key rule","count":1}]}`
	handler.handler.recordPrivacyRedaction("before-switch", summary)
	if !strings.Contains(debugLogs.String(), "before-switch") {
		t.Fatalf("expected debug logger to capture privacy redaction before switch, got %s", debugLogs.String())
	}

	var infoLogs bytes.Buffer
	infoLogger := slog.New(slog.NewTextHandler(&infoLogs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	handler.UpdateLogger(infoLogger)
	handler.handler.recordPrivacyRedaction("after-switch", summary)

	if strings.Contains(infoLogs.String(), "after-switch") {
		t.Fatalf("info logger should suppress privacy debug after switch, got %s", infoLogs.String())
	}
	if strings.Contains(debugLogs.String(), "after-switch") {
		t.Fatalf("old debug logger should not receive privacy debug after switch, got %s", debugLogs.String())
	}
}
