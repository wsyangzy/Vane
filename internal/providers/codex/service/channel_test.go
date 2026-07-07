package service

import (
	"strings"
	"testing"

	"ai-relay-forwarder/internal/providers/codex/store"
)

func TestNormalizeAndValidateCodexChannelAcceptsUnicodeNames(t *testing.T) {
	cases := []string{
		"主渠道",
		"主 渠道",
		"渠道-1",
		"渠道_1",
		"Channel_1",
		"ark api",
	}

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			record := &store.CodexChannelRecord{
				Name:   name,
				ApiKey: "test-key",
			}

			if err := normalizeAndValidateCodexChannel(record); err != nil {
				t.Fatalf("expected %q to be valid, got error: %v", name, err)
			}
		})
	}
}

func TestNormalizeAndValidateCodexChannelAllowsEmptyApiKey(t *testing.T) {
	record := &store.CodexChannelRecord{Name: "主渠道"}

	if err := normalizeAndValidateCodexChannel(record); err != nil {
		t.Fatalf("expected channel without standalone API key to be valid, got error: %v", err)
	}
}

func TestNormalizeAndValidateCodexChannelRejectsInvalidNames(t *testing.T) {
	cases := []string{
		"-主渠道",
		"_主渠道",
		"主\t渠道",
		"主\n渠道",
		"主渠道!",
		strings.Repeat("中", 65),
	}

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			record := &store.CodexChannelRecord{
				Name:   name,
				ApiKey: "test-key",
			}

			if err := normalizeAndValidateCodexChannel(record); err == nil {
				t.Fatalf("expected %q to be invalid", name)
			}
		})
	}
}
