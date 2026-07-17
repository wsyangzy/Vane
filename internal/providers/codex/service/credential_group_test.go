package service

import (
	"context"
	"testing"

	"ai-relay-forwarder/internal/providers/codex/store"
)

func TestEnsureDefaultGroupSkipsChannelWithoutAPIKey(t *testing.T) {
	service := &CodexCredentialGroupService{}
	group, err := service.EnsureDefaultGroup(context.Background(), &store.CodexChannelRecord{ID: 1, Name: "empty"})
	if err != nil {
		t.Fatalf("EnsureDefaultGroup returned error: %v", err)
	}
	if group != nil {
		t.Fatalf("group = %#v, want nil", group)
	}
}
