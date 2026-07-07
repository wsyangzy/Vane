package codexproxy

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestHandlerRecordPrivacyRedactionLogsDebugSummary(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler := &Handler{logger: logger.With("provider", "codex")}

	summary := `{"changed":true,"hits":[{"rule_name":"api key rule","category":"custom","count":1}]}`
	matches := `[{"rule_name":"api key rule","category":"custom","raw":"sk-body-api-key","replacement":"fake_api_key_0001"}]`
	handler.recordPrivacyRedaction("test-codex-privacy-log", summary, []byte(`{"api_key":"fake_api_key_0001"}`), matches)

	output := logs.String()
	for _, expected := range []string{
		"[隐私防护] 请求正文已脱敏",
		"test-codex-privacy-log",
		"privacy_redaction_summary",
		"privacy_redaction_matches",
		"privacy_redacted_body",
		"sk-body-api-key",
		"fake_api_key_0001",
		"api key rule",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected debug log to contain %q, got %s", expected, output)
		}
	}
}

func TestHTTPHandlerUpdateLoggerSuppressesPrivacyDebugAtInfoLevel(t *testing.T) {
	var debugLogs bytes.Buffer
	debugLogger := slog.New(slog.NewTextHandler(&debugLogs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler := NewHTTPHandler(nil, ServerConfig{}, debugLogger, nil)

	summary := `{"changed":true,"hits":[{"rule_name":"api key rule","category":"custom","count":1}]}`
	matches := `[{"rule_name":"api key rule","category":"custom","raw":"sk-body-api-key","replacement":"fake_api_key_0001"}]`
	handler.handler.recordPrivacyRedaction("before-switch", summary, []byte(`{"api_key":"fake_api_key_0001"}`), matches)
	if !strings.Contains(debugLogs.String(), "before-switch") {
		t.Fatalf("expected debug logger to capture privacy redaction before switch, got %s", debugLogs.String())
	}

	var infoLogs bytes.Buffer
	infoLogger := slog.New(slog.NewTextHandler(&infoLogs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	handler.UpdateLogger(infoLogger)
	handler.handler.recordPrivacyRedaction("after-switch", summary, []byte(`{"api_key":"fake_api_key_0001"}`), matches)

	if strings.Contains(infoLogs.String(), "after-switch") {
		t.Fatalf("info logger should suppress privacy debug after switch, got %s", infoLogs.String())
	}
	if strings.Contains(debugLogs.String(), "after-switch") {
		t.Fatalf("old debug logger should not receive privacy debug after switch, got %s", debugLogs.String())
	}
}
