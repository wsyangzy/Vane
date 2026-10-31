package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/appdir"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

type QuotaCredentialResolver interface {
	Resolve(context.Context, string, string) (accounts.Credential, error)
}

// CredentialMaterializer writes only into the supplied private, disposable CLI
// directory. It never reads or updates the user's everyday Claude configuration.
type CredentialMaterializer interface {
	Materialize(context.Context, routing.CredentialRef, string) error
}

type ManagedCredentialMaterializer struct{ Resolver QuotaCredentialResolver }

func (m ManagedCredentialMaterializer) Materialize(ctx context.Context, ref routing.CredentialRef, directory string) error {
	if m.Resolver == nil || ref.Provider != "claude" || ref.AccountID == "" {
		return routing.ErrQuotaCredentials
	}
	credential, err := m.Resolver.Resolve(ctx, "claude", ref.AccountID)
	if err != nil || !accounts.ValidToken(credential.AccessToken) {
		return routing.ErrQuotaCredentials
	}
	var expires int64
	if !credential.ExpiresAt.IsZero() {
		expires = credential.ExpiresAt.UnixMilli()
	}
	oauth := map[string]any{"accessToken": credential.AccessToken, "refreshToken": credential.RefreshToken, "expiresAt": expires, "scopes": credential.Scopes, "subscriptionType": credential.Plan}
	data, err := json.Marshal(map[string]any{"claudeAiOauth": oauth})
	if err != nil {
		return routing.ErrQuotaCredentials
	}
	defer clear(data)
	if os.WriteFile(filepath.Join(directory, ".credentials.json"), data, 0600) != nil {
		return routing.ErrQuotaIsolation
	}
	profile, _ := json.Marshal(map[string]any{"hasCompletedOnboarding": true, "oauthAccount": map[string]string{"accountUuid": credential.Subject, "emailAddress": credential.Email}})
	defer clear(profile)
	if os.WriteFile(filepath.Join(directory, ".claude.json"), profile, 0600) != nil {
		return routing.ErrQuotaIsolation
	}
	return nil
}

type QuotaCLIRunner interface {
	Run(context.Context, string, string, []string) ([]byte, error)
}

type RoutingQuotaReader struct {
	Materializer CredentialMaterializer
	Runner       QuotaCLIRunner
	DataDir      string
	Command      string
	Proxy        string
	Timeout      time.Duration
	locks        sync.Map
}

func NewRoutingQuotaReader(resolver QuotaCredentialResolver, dataDir string) *RoutingQuotaReader {
	if dataDir == "" {
		dataDir = appdir.GetDataDir()
	}
	return &RoutingQuotaReader{Materializer: ManagedCredentialMaterializer{resolver}, Runner: isolatedQuotaCLI{}, DataDir: dataDir, Command: "claude", Timeout: 30 * time.Second}
}

func (*RoutingQuotaReader) Provider() string { return "claude" }

func (r *RoutingQuotaReader) Read(ctx context.Context, ref routing.CredentialRef, model string) ([]routing.QuotaSnapshot, error) {
	if r.Materializer == nil || r.Runner == nil || ref.Provider != "claude" || ref.AccountID == "" {
		return nil, routing.ErrQuotaCredentials
	}
	timeout := r.Timeout
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	lock, _ := r.locks.LoadOrStore(ref.IdentityHash(), make(chan struct{}, 1))
	gate := lock.(chan struct{})
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return nil, routing.SafeQuotaError(ctx.Err())
	}
	accountDirectory := sha256.Sum256([]byte(ref.IdentityHash()))
	base, err := filepath.Abs(filepath.Join(r.DataDir, "routing-quota", "claude", hex.EncodeToString(accountDirectory[:])))
	if err != nil || os.MkdirAll(base, 0700) != nil || secureRoutingQuotaDirectory(base) != nil {
		return nil, routing.ErrQuotaIsolation
	}
	directory, err := os.MkdirTemp(base, "refresh-")
	if err != nil {
		return nil, routing.ErrQuotaIsolation
	}
	// The only recursive cleanup target is an os.MkdirTemp child of this account
	// directory. Check the boundary before removal, including on Windows.
	defer func() {
		relative, pathErr := filepath.Rel(base, directory)
		if pathErr == nil && relative != "." && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			_ = os.RemoveAll(directory)
		}
	}()
	if secureRoutingQuotaDirectory(directory) != nil {
		return nil, routing.ErrQuotaIsolation
	}
	if err = r.Materializer.Materialize(ctx, ref, directory); err != nil {
		return nil, routing.SafeQuotaError(err)
	}
	body, err := r.Runner.Run(ctx, r.Command, directory, quotaCLIEnvironment(os.Environ(), directory, r.Proxy))
	defer clear(body)
	if err != nil {
		return nil, routing.SafeQuotaError(err)
	}
	if ctx.Err() != nil {
		return nil, routing.SafeQuotaError(ctx.Err())
	}
	snapshots := parseClaudeCLIQuota(body, ref, model, time.Now().UTC())
	if len(snapshots) == 0 {
		// /usage is not supported in print mode by every CLI version. Never
		// interpret arbitrary assistant prose or login messages as quota facts.
		return nil, routing.ErrQuotaCLIUnsupported
	}
	return snapshots, nil
}

func quotaCLIEnvironment(environment []string, directory, proxy string) []string {
	result := []string{}
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_") || key == "CLAUDECODE" || strings.HasPrefix(key, "OPENAI_") || strings.HasPrefix(key, "CODEX_") || key == "BROWSER" || key == "HTTP_PROXY" || key == "HTTPS_PROXY" || key == "ALL_PROXY" || key == "NO_PROXY" {
			continue
		}
		result = append(result, entry)
	}
	result = append(result, "CLAUDE_CONFIG_DIR="+directory, "CLAUDE_SECURESTORAGE_CONFIG_DIR="+directory, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1", "NO_PROXY=localhost,127.0.0.1,::1")
	if proxy != "" {
		result = append(result, "HTTP_PROXY="+proxy, "HTTPS_PROXY="+proxy, "ALL_PROXY="+proxy)
	}
	return result
}

type isolatedQuotaCLI struct{}
type quotaOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *quotaOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > 1<<20 {
		b.overflow = true
		return n, nil
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

func (isolatedQuotaCLI) Run(ctx context.Context, command, directory string, environment []string) ([]byte, error) {
	path, err := exec.LookPath(command)
	if err != nil {
		path, err = exec.LookPath(command + ".cmd")
	}
	if err != nil {
		return nil, routing.ErrQuotaCLIUnavailable
	}
	cmd := routingQuotaCommand(ctx, path)
	cmd.Dir, cmd.Env = directory, environment
	cmd.WaitDelay = time.Second
	var output quotaOutput
	defer clear(output.Bytes())
	cmd.Stdout, cmd.Stderr, cmd.Stdin = &output, io.Discard, nil
	if cmd.Start() != nil {
		return nil, routing.ErrQuotaCLIUnavailable
	}
	stop, err := trackRoutingQuotaProcess(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, routing.ErrQuotaIsolation
	}
	stopCancellation := context.AfterFunc(ctx, stop)
	err = cmd.Wait()
	stopCancellation()
	stop()
	if ctx.Err() != nil {
		return nil, routing.SafeQuotaError(ctx.Err())
	}
	if err != nil || output.overflow {
		return nil, routing.ErrQuotaCLIUnsupported
	}
	return append([]byte(nil), output.Bytes()...), nil
}

func claudeQuotaSnapshot(ref routing.CredentialRef, scope, window, source, confidence string, used float64, reset *time.Time, now time.Time) routing.QuotaSnapshot {
	used = math.Min(used, 100)
	expires := now.Add(5 * time.Minute)
	if reset != nil && reset.Before(expires) {
		expires = *reset
	}
	return routing.QuotaSnapshot{Provider: "claude", ChannelID: ref.ChannelID, GroupID: ref.GroupID, AccountIDHash: ref.IdentityHash(), ModelScope: scope, WindowName: window, UsedPercent: &used, ResetAt: reset, ObservedAt: now, ExpiresAt: expires, Source: source, Confidence: confidence}
}

func claudeQuotaScope(family, model string) string {
	if family == "" {
		return "*"
	}
	if strings.Contains(strings.ToLower(model), family) {
		return model
	}
	return "family:" + family
}

// ProjectRoutingQuotaForModel resolves Claude's family windows at the adapter
// boundary. Persisted snapshots retain their original family scope for the UI.
func ProjectRoutingQuotaForModel(windows []routing.QuotaSnapshot, model string) []routing.QuotaSnapshot {
	projected := append([]routing.QuotaSnapshot(nil), windows...)
	for i := range projected {
		q := &projected[i]
		if q.Provider != "claude" || !strings.HasPrefix(q.ModelScope, "family:") {
			continue
		}
		family := strings.TrimPrefix(q.ModelScope, "family:")
		if (family == "opus" || family == "sonnet") && strings.Contains(strings.ToLower(model), family) {
			q.ModelScope = model
		}
	}
	return projected
}

func parseClaudeCLIQuota(body []byte, ref routing.CredentialRef, model string, now time.Time) []routing.QuotaSnapshot {
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	if raw := payload["result"]; raw != nil {
		var result string
		if json.Unmarshal(raw, &result) == nil {
			if json.Unmarshal([]byte(result), &payload) != nil {
				return nil
			}
		}
	}
	var result []routing.QuotaSnapshot
	for _, window := range []struct{ name, family string }{{"five_hour", ""}, {"seven_day", ""}, {"seven_day_opus", "opus"}, {"seven_day_sonnet", "sonnet"}} {
		var data struct {
			Used        *float64 `json:"used_percent"`
			Utilization *float64 `json:"utilization"`
			Reset       string   `json:"resets_at"`
		}
		if json.Unmarshal(payload[window.name], &data) != nil {
			continue
		}
		used := data.Used
		if used == nil {
			used = data.Utilization
		}
		if used == nil || math.IsNaN(*used) || math.IsInf(*used, 0) || *used < 0 {
			continue
		}
		var reset *time.Time
		if at, err := time.Parse(time.RFC3339, data.Reset); err == nil && at.After(now) && at.Before(now.Add(31*24*time.Hour)) {
			reset = &at
		}
		result = append(result, claudeQuotaSnapshot(ref, claudeQuotaScope(window.family, model), window.name, "cli", "exact", *used, reset, now))
	}
	return result
}

func ParseResponseQuota(status int, headers http.Header, body []byte, ref routing.CredentialRef, model string, now time.Time) []routing.QuotaSnapshot {
	var result []routing.QuotaSnapshot
	for _, window := range []struct{ name, family string }{{"5h", ""}, {"7d", ""}, {"7d-opus", "opus"}, {"7d-sonnet", "sonnet"}} {
		prefix := "anthropic-ratelimit-unified-" + window.name
		used, err := strconv.ParseFloat(headers.Get(prefix+"-utilization"), 64)
		if err != nil || math.IsNaN(used) || math.IsInf(used, 0) || used < 0 {
			if !strings.EqualFold(headers.Get(prefix+"-status"), "rejected") {
				continue
			}
			used = 1
		}
		at, known := subscription.ResetTime(headers.Get(prefix+"-reset"), now, false)
		if !known && used >= 1 {
			at, known = subscription.ResetTime(headers.Get("Retry-After"), now, true)
		}
		var reset *time.Time
		if known {
			reset = &at
		}
		result = append(result, claudeQuotaSnapshot(ref, claudeQuotaScope(window.family, model), window.name, "response_header", "learned", used*100, reset, now))
	}
	for _, pause := range subscription.QuotaPauses(status, headers, body, subscription.Binding{GroupID: ref.GroupID}, model, now) {
		if pause.Model == "" {
			continue
		}
		if len(result) > 0 && !strings.Contains(pause.Source, "structured") {
			continue
		}
		var reset *time.Time
		if pause.ResetKnown {
			at := pause.ResumeNotBefore
			reset = &at
		}
		q := claudeQuotaSnapshot(ref, pause.Model, "quota_error", "structured_error", "learned", 100, reset, now)
		if reset == nil {
			q.ExpiresAt = now.Add(time.Minute)
		}
		result = append(result, q)
	}
	return result
}
