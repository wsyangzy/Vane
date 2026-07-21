package handlers

import (
	"context"
	"fmt"

	"ai-relay-forwarder/internal/privacy"
)

type PrivacyRedactor interface {
	Redact(body []byte) (redacted []byte, summary privacy.RedactionSummary, changed bool, err error)
}

func redactBodyBeforeForward(redactor PrivacyRedactor, body []byte, lifecycleManager RequestLifecycleManager) ([]byte, error) {
	return redactBodyBeforeForwardContext(context.Background(), redactor, body, lifecycleManager, "", "")
}

func redactBodyBeforeForwardContext(ctx context.Context, redactor PrivacyRedactor, body []byte, lifecycleManager RequestLifecycleManager, contentType, contentEncoding string) ([]byte, error) {
	if redactor == nil {
		return body, nil
	}
	redacted, summary, changed, err := redactWithContext(ctx, redactor, body, contentType, contentEncoding)
	if err != nil {
		return nil, fmt.Errorf("privacy redaction failed: %w", err)
	}
	if changed && lifecycleManager != nil {
		lifecycleManager.RecordPrivacyRedaction(
			privacy.SummaryToJSON(summary),
			redacted,
			privacy.DebugMatchesToJSON(summary),
		)
	}
	return redacted, nil
}

func redactWithContext(ctx context.Context, redactor PrivacyRedactor, body []byte, contentType, contentEncoding string) ([]byte, privacy.RedactionSummary, bool, error) {
	if requestAware, ok := redactor.(interface {
		RedactRequestContext(context.Context, []byte, string, string) ([]byte, privacy.RedactionSummary, bool, error)
	}); ok {
		return requestAware.RedactRequestContext(ctx, body, contentType, contentEncoding)
	}
	if contextual, ok := redactor.(interface {
		RedactContext(context.Context, []byte) ([]byte, privacy.RedactionSummary, bool, error)
	}); ok {
		return contextual.RedactContext(ctx, body)
	}
	return redactor.Redact(body)
}
