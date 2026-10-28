package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

type quotaResolverFunc func(context.Context, string, string) (accounts.Credential, error)

func (f quotaResolverFunc) Resolve(ctx context.Context, p, id string) (accounts.Credential, error) {
	return f(ctx, p, id)
}

type quotaRunnerFunc func(context.Context, string, string, []string) ([]byte, error)

func (f quotaRunnerFunc) Run(ctx context.Context, command, dir string, env []string) ([]byte, error) {
	return f(ctx, command, dir, env)
}

func newQuotaReaderTest(t *testing.T) *RoutingQuotaReader {
	t.Helper()
	return NewRoutingQuotaReader(quotaResolverFunc(func(_ context.Context, p, id string) (accounts.Credential, error) {
		if p != "claude" || id != "managed" {
			t.Error("wrong account")
		}
		return accounts.Credential{AccessToken: "access-secret", RefreshToken: "refresh-secret", Subject: "uuid", Email: "private@example.invalid", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}), t.TempDir())
}

func TestRoutingQuotaClaudeFamilyProjectionIsModelIsolated(t *testing.T) {
	full := 100.0
	windows := []routing.QuotaSnapshot{{Provider: "claude", ModelScope: "family:opus", UsedPercent: &full, ExpiresAt: time.Now().Add(time.Hour)}}
	profile := routing.DefaultProfile("claude")
	profile.ID = 1
	profile.Name = "family quota"
	profile.Members = []routing.RoutingMember{{ChannelID: 1, GroupID: 1, Enabled: true}}
	for _, model := range []string{"claude-opus-4-5", "claude-sonnet-4-5"} {
		projected := ProjectRoutingQuotaForModel(windows, model)
		candidate := routing.DispatchCandidate{Provider: "claude", ChannelID: 1, GroupID: 1, EndpointID: "test", Model: model, Protocol: "messages", Enabled: true, Quota: routing.QuotaView{Windows: projected}}
		result := routing.NewScheduler().Order(profile, []routing.DispatchCandidate{candidate}, nil, time.Now())
		if len(result) != 1 || result[0].Eligible == strings.Contains(model, "opus") {
			t.Fatalf("family quota was ignored or crossed models: model=%s result=%+v", model, result)
		}
		if windows[0].ModelScope != "family:opus" {
			t.Fatal("persisted quota scope mutated")
		}
	}
}

func TestRoutingQuotaClaudeMaterializesOnlyInsideDisposableAccountDirectory(t *testing.T) {
	reader := newQuotaReaderTest(t)
	var directory string
	reader.Runner = quotaRunnerFunc(func(_ context.Context, _ string, dir string, env []string) ([]byte, error) {
		directory = dir
		rel, err := filepath.Rel(reader.DataDir, dir)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Fatal("not isolated in app data")
		}
		data, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
		if err != nil {
			t.Fatal(err)
		}
		var credential struct {
			OAuth struct {
				Access string `json:"accessToken"`
			} `json:"claudeAiOauth"`
		}
		if json.Unmarshal(data, &credential) != nil || credential.OAuth.Access != "access-secret" {
			t.Fatal("native credential not materialized")
		}
		if !strings.Contains(strings.Join(env, "\n"), "CLAUDE_CONFIG_DIR="+dir) {
			t.Fatal("CLI directory not scoped")
		}
		return []byte(`{"five_hour":{"utilization":25,"resets_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"},"seven_day_opus":{"utilization":80}}`), nil
	})
	got, err := reader.Read(context.Background(), routing.CredentialRef{Provider: "claude", ChannelID: 1, GroupID: 2, AccountID: "managed"}, "claude-sonnet-test")
	if err != nil || len(got) != 2 || *got[0].UsedPercent != 25 || got[1].ModelScope != "family:opus" {
		t.Fatalf("quota=%+v %v", got, err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credential directory retained: %v", err)
	}
}

func TestRoutingQuotaClaudeTimeoutCleansDirectoryAndDoesNotExposeOutput(t *testing.T) {
	reader := newQuotaReaderTest(t)
	reader.Timeout = 10 * time.Millisecond
	var directory string
	reader.Runner = quotaRunnerFunc(func(ctx context.Context, _ string, dir string, _ []string) ([]byte, error) {
		directory = dir
		<-ctx.Done()
		return []byte("raw-secret-output"), ctx.Err()
	})
	_, err := reader.Read(context.Background(), routing.CredentialRef{Provider: "claude", ChannelID: 1, GroupID: 2, AccountID: "managed"}, "model")
	if !errors.Is(err, routing.ErrQuotaTimeout) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("timeout=%v", err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("timeout retained credentials")
	}
}

func TestRoutingQuotaClaudeUnsupportedCLIStaysResponseLearningOnly(t *testing.T) {
	reader := newQuotaReaderTest(t)
	reader.Runner = quotaRunnerFunc(func(context.Context, string, string, []string) ([]byte, error) {
		return []byte("You have 10% remaining; token-secret login failed"), nil
	})
	_, err := reader.Read(context.Background(), routing.CredentialRef{Provider: "claude", ChannelID: 1, GroupID: 2, AccountID: "managed"}, "")
	if !errors.Is(err, routing.ErrQuotaCLIUnsupported) {
		t.Fatalf("guessed quota from prose: %v", err)
	}
	env := strings.Join(quotaCLIEnvironment([]string{"PATH=kept", "ANTHROPIC_API_KEY=secret", "CLAUDE_CONFIG_DIR=daily", "CLAUDECODE=1", "BROWSER=secret", "HTTP_PROXY=old"}, "isolated", ""), "\n")
	if strings.Contains(env, "secret") || strings.Contains(env, "daily") || strings.Contains(env, "old") || !strings.Contains(env, "PATH=kept") {
		t.Fatalf("daily credentials leaked: %s", env)
	}
}

func TestRoutingQuotaClaudeResponseLearningUsesHeaderFractionsAndModelScopes(t *testing.T) {
	now := time.Now().UTC()
	ref := routing.CredentialRef{Provider: "claude", ChannelID: 1, GroupID: 2, AccountID: "managed"}
	h := http.Header{"Anthropic-Ratelimit-Unified-5h-Utilization": []string{"0.25"}, "Anthropic-Ratelimit-Unified-5h-Reset": []string{strconv.FormatInt(now.Add(time.Hour).Unix(), 10)}, "Anthropic-Ratelimit-Unified-7d-Opus-Utilization": []string{"1"}}
	got := ParseResponseQuota(200, h, nil, ref, "claude-sonnet-test", now)
	if len(got) != 2 || *got[0].UsedPercent != 25 || got[1].ModelScope != "family:opus" || *got[1].UsedPercent != 100 {
		t.Fatalf("header learning=%+v", got)
	}
}
