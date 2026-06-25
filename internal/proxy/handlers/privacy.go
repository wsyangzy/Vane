package handlers

import (
	"fmt"

	"ai-relay-forwarder/internal/privacy"
)

type PrivacyRedactor interface {
	Redact(body []byte) (redacted []byte, summary privacy.RedactionSummary, changed bool, err error)
}

func redactBodyBeforeForward(redactor PrivacyRedactor, body []byte, lifecycleManager RequestLifecycleManager) ([]byte, error) {
	if redactor == nil {
		return body, nil
	}
	redacted, summary, changed, err := redactor.Redact(body)
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
