package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ai-relay-forwarder/config"
	claudesub "ai-relay-forwarder/internal/providers/claude/proxy/subscription"
	claudestore "ai-relay-forwarder/internal/providers/claude/store"
	codexsub "ai-relay-forwarder/internal/providers/codex/proxy/subscription"
	codexstore "ai-relay-forwarder/internal/providers/codex/store"
)

// The embedded interfaces expose only the operations this API should use.
type connectionClaudeGroups struct {
	claudestore.ClaudeCredentialGroupStore
	group *claudestore.ClaudeCredentialGroupRecord
}

func (s connectionClaudeGroups) GetByID(context.Context, int64) (*claudestore.ClaudeCredentialGroupRecord, error) {
	return s.group, nil
}

type connectionClaudeEndpoints struct{ claudestore.EndpointStore }

func (connectionClaudeEndpoints) ListByCredentialGroupID(context.Context, int64) ([]*claudestore.EndpointRecord, error) {
	return []*claudestore.EndpointRecord{{URL: claudesub.OfficialOrigin, FailoverEnabled: true}}, nil
}

type connectionCodexGroups struct {
	codexstore.CodexCredentialGroupStore
	group *codexstore.CodexCredentialGroupRecord
}

func (s connectionCodexGroups) GetByID(context.Context, int64) (*codexstore.CodexCredentialGroupRecord, error) {
	return s.group, nil
}

type connectionCodexEndpoints struct{ codexstore.CodexEndpointStore }

func (connectionCodexEndpoints) ListByChannelID(context.Context, int64) ([]*codexstore.CodexEndpointRecord, error) {
	return []*codexstore.CodexEndpointRecord{{CredentialGroupID: 7, BaseURL: codexsub.OfficialBaseURL, FailoverEnabled: true}}, nil
}

func TestSubscriptionConnectionPreviewApplyRestoreBothProviders(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows DPAPI")
	}
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			clientDir := t.TempDir()
			t.Setenv("CODEX_HOME", clientDir)
			t.Setenv("CLAUDE_CONFIG_DIR", clientDir)
			path, err := subscriptionConfigPath(provider)
			if err != nil {
				t.Fatal(err)
			}
			original := "{\"env\":{\"KEEP\":\"private-existing-value\"}}"
			if provider == "codex" {
				original = "model_reasoning_effort = 'high'\n[model_providers.other]\nexperimental_bearer_token = 'private-existing-value'\n"
			}
			os.WriteFile(path, []byte(original), 0600)
			authPath := filepath.Join(clientDir, "auth.json")
			os.WriteFile(authPath, []byte("original-login-file"), 0600)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			key := "relay_" + strings.Repeat("a", 64)
			digest := sha256.Sum256([]byte(key))
			keyHash := hex.EncodeToString(digest[:])
			app := &App{config: &config.Config{Server: config.ServerConfig{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port}}, isRunning: true, proxyServer: &http.Server{},
				claudeCredentialGroupStore: connectionClaudeGroups{group: &claudestore.ClaudeCredentialGroupRecord{ID: 7, Channel: "official", AuthMode: claudesub.ClientPassthrough, Subscription: &claudesub.Binding{KeyHash: keyHash, Version: 3, Enabled: true}}}, endpointStore: connectionClaudeEndpoints{},
				codexCredentialGroupStore: connectionCodexGroups{group: &codexstore.CodexCredentialGroupRecord{ID: 7, ChannelID: 2, AuthMode: codexsub.ClientPassthrough, Subscription: &codexsub.Binding{KeyHash: keyHash, Version: 3, Enabled: true}}}, codexEndpointStore: connectionCodexEndpoints{}}
			input := SubscriptionConnectionCommand{Provider: provider, Action: "status", GroupID: 7}
			status, err := app.ManageSubscriptionConnection(input)
			if err != nil || !status.EndpointReady || !status.Running || status.Snippet != "" || status.ConfigPath != path {
				t.Fatalf("bad status: %+v %v", status, err)
			}
			input.Action, input.LocalKey = "preview", "wrong-key"
			if _, err = app.ManageSubscriptionConnection(input); err == nil {
				t.Fatal("wrong key accepted")
			}
			input.LocalKey = key
			preview, err := app.ManageSubscriptionConnection(input)
			if err != nil || !strings.Contains(preview.Snippet, key) || strings.Contains(preview.Snippet, "private-existing-value") {
				t.Fatalf("bad preview: %v", err)
			}
			current, _ := os.ReadFile(path)
			if string(current) != original {
				t.Fatal("preview wrote live file")
			}
			input.Action, input.Revision, input.BindingVersion, input.ContentRevision = "apply", preview.Revision, preview.BindingVersion, preview.ContentRevision
			input.ContentRevision = "stale-preview"
			if _, err = app.ManageSubscriptionConnection(input); err == nil {
				t.Fatal("changed preview applied")
			}
			input.ContentRevision = preview.ContentRevision
			applied, err := app.ManageSubscriptionConnection(input)
			if err != nil || !applied.BackupAvailable {
				t.Fatalf("apply failed: %v", err)
			}
			auth, _ := os.ReadFile(authPath)
			if string(auth) != "original-login-file" {
				t.Fatal("official login overwritten")
			}
			// No group/key is needed to restore; it must work after deleting a caller.
			input.Action, input.GroupID, input.LocalKey, input.Revision = "restore", 0, "", applied.Revision
			if _, err = app.ManageSubscriptionConnection(input); err != nil {
				t.Fatal(err)
			}
			current, _ = os.ReadFile(path)
			if string(current) != original {
				t.Fatal("original not restored")
			}
		})
	}
}

func TestCodexGlobalConfigKeepsSubscriptionRouteAndIPv6(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	path, _ := subscriptionConfigPath("codex")
	os.WriteFile(path, []byte("model_provider = \"relay_subscription\"\n[model_providers.relay_subscription]\nbase_url = \"http://[::1]:9000/backend-api/codex\"\nrequires_openai_auth = true\n[model_providers.relay_subscription.http_headers]\nX-Relay-Key = 'keep-local-key'\n"), 0600)
	a := &App{config: &config.Config{Server: config.ServerConfig{Host: "::1", Port: 9000}}}
	if err := a.UpdateCodexGlobalConfig(CodexGlobalConfigInput{Model: "keep-model"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "http://[::1]:9000/backend-api/codex") || !strings.Contains(string(got), "keep-local-key") {
		t.Fatal("model settings replaced subscription connection")
	}
}
