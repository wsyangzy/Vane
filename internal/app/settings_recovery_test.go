package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestCodexHistoryRecoverySettingReachesNewAndRunningHandlers(t *testing.T) {
	a, _ := newDispatchSettingsTestApp(t)
	resources, members := dispatchTestApp(t, "codex")
	ctx := context.Background()
	var sends atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		raw, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(raw), "encrypted_content") {
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"code":"invalid_encrypted_content","message":"rejected history"}}`)
			return
		}
		io.WriteString(w, `{"id":"resp-recovered","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()
	_, err := resources.storeDB.ExecContext(ctx, "UPDATE codex_endpoints SET base_url=?", upstream.URL+"/v1")
	require.NoError(t, err)
	require.NoError(t, resources.codexEndpointMgr.Reload())
	profile := routing.DefaultProfile("codex")
	profile.Name, profile.IsDefault = "history recovery", true
	profile.Members = []routing.RoutingMember{{ChannelID: members[0].ChannelID, GroupID: members[0].GroupID, Enabled: true}}
	_, err = resources.SaveRoutingProfile(profile)
	require.NoError(t, err)
	a.codexEndpointMgr = resources.codexEndpointMgr
	require.NoError(t, a.settingsService.InitDefaults(ctx))
	require.NoError(t, a.UpdateSetting(UpdateSettingInput{Category: "request", Key: "codex_encrypted_history_recovery", Value: "true"}))
	a.registerCodexRoutes(http.NewServeMux())
	handler := a.codexHTTPHandler
	for i, enabled := range []string{"true", "false", "true"} {
		if i > 0 {
			require.NoError(t, a.UpdateSetting(UpdateSettingInput{Category: "request", Key: "codex_encrypted_history_recovery", Value: enabled}))
		}
		require.Same(t, handler, a.codexHTTPHandler)
		before := sends.Load()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "/responses", strings.NewReader(`{"model":"test","input":[{"type":"reasoning","encrypted_content":"sealed"},{"role":"user","content":"continue"}]}`)))
		if enabled == "true" {
			require.Equal(t, 200, w.Code)
			require.Equal(t, int32(3), sends.Load()-before)
		} else {
			require.Equal(t, 400, w.Code)
			require.Equal(t, int32(2), sends.Load()-before)
		}
	}
	// A saved but now unavailable directory must not suppress fallback on restart.
	bad := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(bad, []byte("keep"), 0600))
	a.codexHTTPHandler = nil
	require.NoError(t, a.BatchUpdateSettings(BatchUpdateSettingsInput{Settings: []UpdateSettingInput{
		{Category: "request", Key: "codex_history_directory", Value: bad},
		{Category: "request", Key: "codex_history_persistence", Value: "true"},
	}}))
	a.registerCodexRoutes(http.NewServeMux())
	before := sends.Load()
	w := httptest.NewRecorder()
	a.codexHTTPHandler.ServeHTTP(w, httptest.NewRequest("POST", "/responses", strings.NewReader(`{"model":"test","input":[{"type":"reasoning","encrypted_content":"sealed"},{"role":"user","content":"continue"}]}`)))
	require.Equal(t, 200, w.Code)
	require.Equal(t, int32(3), sends.Load()-before)
}

func TestCodexHistoryRecoverySettingPersistsAndRemainsOptIn(t *testing.T) {
	a, _ := newDispatchSettingsTestApp(t)
	ctx := context.Background()
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	read := func(want string) {
		t.Helper()
		setting, err := a.GetSetting("request", "codex_encrypted_history_recovery")
		if err != nil || setting.Value != want || setting.RequiresRestart {
			t.Fatalf("unexpected recovery setting: %+v %v", setting, err)
		}
	}
	read("false")
	visible, err := a.GetSettingsByCategory("request")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, setting := range visible {
		found = found || setting.Key == "codex_encrypted_history_recovery"
	}
	if !found {
		t.Fatal("recovery setting is hidden from the settings page")
	}
	if err := a.UpdateSetting(UpdateSettingInput{Category: "request", Key: "codex_encrypted_history_recovery", Value: "true"}); err != nil {
		t.Fatal(err)
	}
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	read("true")
	for _, input := range []UpdateSettingInput{
		{Category: "request", Key: "codex_encrypted_history_recovery", Value: "invalid"},
		{Provider: "claude", Category: "request", Key: "codex_encrypted_history_recovery", Value: "true"},
		{Provider: "codex", Category: "request", Key: "codex_encrypted_history_recovery", Value: "true"},
	} {
		if err := a.UpdateSetting(input); err == nil {
			t.Fatalf("invalid update accepted: %+v", input)
		}
	}
	read("true")
	if err := a.ResetCategorySettings("request"); err != nil {
		t.Fatal(err)
	}
	read("false")
}

func TestCodexRecoveryStorageSettingsPathsAndHotApply(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows user vault")
	}
	a, _ := newDispatchSettingsTestApp(t)
	resources, _ := dispatchTestApp(t, "codex")
	a.codexEndpointMgr = resources.codexEndpointMgr
	ctx := context.Background()
	require.NoError(t, a.settingsService.InitDefaults(ctx))
	directory := filepath.Join(t.TempDir(), "recovery store")
	require.NoError(t, a.UpdateSetting(UpdateSettingInput{Category: "request", Key: "codex_history_directory", Value: directory}))
	a.registerCodexRoutes(http.NewServeMux())
	path := filepath.Join(directory, historyEnvironment(), "recovery.db")
	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err), "default must not create recovery storage")
	handler := a.codexHTTPHandler
	require.NoError(t, a.UpdateSetting(UpdateSettingInput{Category: "request", Key: "codex_history_persistence", Value: "true"}))
	_, err = os.Stat(path)
	require.NoError(t, err)
	require.Same(t, handler, a.codexHTTPHandler)
	setting, err := a.GetSetting("request", "codex_history_directory")
	require.NoError(t, err)
	require.Contains(t, setting.Description, path)
	require.False(t, setting.RequiresRestart)
	for _, input := range []UpdateSettingInput{
		{Category: "request", Key: "codex_history_directory", Value: "relative"},
		{Category: "request", Key: "codex_history_retention_days", Value: "0"},
		{Category: "request", Key: "codex_history_retention_days", Value: "91"},
		{Category: "request", Key: "codex_history_max_mib", Value: "7"},
		{Category: "request", Key: "codex_history_max_mib", Value: "1025"},
		{Category: "request", Key: "codex_history_client_lookup", Value: "maybe"},
		{Provider: "codex", Category: "request", Key: "codex_history_persistence", Value: "true"},
	} {
		require.Error(t, a.UpdateSetting(input))
	}
	bad := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(bad, []byte("keep"), 0600))
	require.Error(t, a.UpdateSetting(UpdateSettingInput{Category: "request", Key: "codex_history_directory", Value: bad}))
	setting, err = a.GetSetting("request", "codex_history_directory")
	require.NoError(t, err)
	require.Equal(t, directory, setting.Value)
	require.NoError(t, a.UpdateSetting(UpdateSettingInput{Category: "request", Key: "codex_history_persistence", Value: "false"}))
	_, err = os.Stat(path)
	require.NoError(t, err, "disable must retain existing protected file")
	require.NoError(t, a.settingsService.InitDefaults(ctx))
	setting, err = a.GetSetting("request", "codex_history_directory")
	require.NoError(t, err)
	require.Equal(t, directory, setting.Value)
}

func TestGlobalRecoveryWaitDefaultsPersistsAndResets(t *testing.T) {
	a, _ := newDispatchSettingsTestApp(t)
	ctx := context.Background()
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.applySettingsToConfig(false); err != nil {
		t.Fatal(err)
	}
	setting, err := a.GetSetting("retry", "wait_for_recovery")
	if err != nil || setting.Provider != "global" || setting.Value != "true" || a.config.Retry.DisableRecoveryWait {
		t.Fatalf("recovery waiting must default on globally: setting=%+v runtime=%+v err=%v", setting, a.config.Retry, err)
	}
	if err := a.BatchUpdateSettings(BatchUpdateSettingsInput{Settings: []UpdateSettingInput{{Provider: "global", Category: "retry", Key: "wait_for_recovery", Value: "false"}}}); err != nil {
		t.Fatal(err)
	}
	if !a.config.Retry.DisableRecoveryWait {
		t.Fatal("disabling recovery waiting did not reach the runtime")
	}
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	a.config.Retry = config.NewDefaultConfig().Retry
	if err := a.applySettingsToConfig(false); err != nil {
		t.Fatal(err)
	}
	if !a.config.Retry.DisableRecoveryWait {
		t.Fatal("reinitialization lost the saved disabled policy")
	}
	for _, input := range []UpdateSettingInput{
		{Provider: "claude", Category: "retry", Key: "wait_for_recovery", Value: "true"},
		{Provider: "codex", Category: "retry", Key: "wait_for_recovery", Value: "true"},
		{Provider: "global", Category: "retry", Key: "wait_for_recovery", Value: "invalid"},
	} {
		if err := a.UpdateSetting(input); err == nil {
			t.Fatalf("invalid global recovery policy accepted: %+v", input)
		}
	}
	if err := a.ResetCategorySettings("retry"); err != nil {
		t.Fatal(err)
	}
	if a.config.Retry.DisableRecoveryWait {
		t.Fatal("reset must restore recovery waiting")
	}
}

func TestRoutingPolicySettingsPersistApplyToBothProvidersAndRejectInvalidBatch(t *testing.T) {
	a, _ := newDispatchSettingsTestApp(t)
	ctx := context.Background()
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	visible, err := a.GetSettingsByCategory("retry")
	if err != nil || len(visible) != 7 {
		t.Fatalf("routing policy must be exposed by the settings API: %d %v", len(visible), err)
	}
	a.poolRuntimes = map[string]*routing.PoolRuntime{}
	a.profileRuntimes = map[string]*routing.ProfileRuntime{}
	for _, provider := range []string{"claude", "codex"} {
		a.poolRuntimes[provider] = routing.NewPoolRuntime(routing.PoolConfig{}, nil, nil)
		profile := routing.DefaultProfile(provider)
		profile.ID, profile.Name, profile.IsDefault = 1, "test", true
		profile.Rules = []routing.RoutingRule{{ID: 1, Enabled: true, Kind: "model", MatchModel: "*", Targets: []routing.RouteTarget{{ChannelID: 2}}}}
		a.profileRuntimes[provider] = routing.NewProfileRuntime(routing.RuntimeConfig{Provider: provider, Profiles: []routing.RoutingProfile{profile}})
	}
	updates := []UpdateSettingInput{
		{Provider: "global", Category: "retry", Key: "cooldown_initial", Value: "4s"},
		{Provider: "global", Category: "retry", Key: "cooldown_max", Value: "20s"},
		{Provider: "global", Category: "retry", Key: "cooldown_multiplier", Value: "1.5"},
		{Provider: "global", Category: "retry", Key: "rule_fallback_to_default", Value: "false"},
	}
	if err := a.BatchUpdateSettings(BatchUpdateSettingsInput{Settings: updates}); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"claude", "codex"} {
		policy := a.poolRuntimes[provider].Config().CooldownPolicy
		if policy.Initial != 4*time.Second || policy.Maximum != 20*time.Second || policy.Multiplier != 1.5 {
			t.Fatalf("%s did not apply saved policy: %+v", provider, policy)
		}
		plan := a.profileRuntimes[provider].Preview(ctx, 1, "test-model", true, routing.TurnInput{})
		if plan.Decision.FallbackToDefault {
			t.Fatalf("%s did not apply inherited model fallback", provider)
		}
	}
	if err := a.settingsService.InitDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	for _, update := range updates {
		saved, err := a.GetSetting(update.Category, update.Key)
		if err != nil || saved.Value != update.Value {
			t.Fatalf("initialization overwrote saved policy: %+v %v", saved, err)
		}
	}
	for _, bad := range []UpdateSettingInput{
		{Provider: "global", Category: "retry", Key: "cooldown_initial", Value: "21s"},
		{Provider: "global", Category: "retry", Key: "cooldown_max", Value: "1ms"},
		{Provider: "global", Category: "retry", Key: "cooldown_multiplier", Value: "NaN"},
		{Provider: "global", Category: "retry", Key: "cooldown_multiplier", Value: "0.5"},
		{Provider: "codex", Category: "retry", Key: "rule_fallback_to_default", Value: "true"},
		{Provider: "global", Category: "retry", Key: "rule_fallback_to_default", Value: "bad"},
	} {
		if err := a.BatchUpdateSettings(BatchUpdateSettingsInput{Settings: []UpdateSettingInput{{Provider: "global", Category: "retry", Key: "rule_fallback_to_default", Value: "true"}, bad}}); err == nil {
			t.Fatalf("invalid batch accepted: %+v", bad)
		}
		saved, _ := a.GetSetting("retry", "rule_fallback_to_default")
		if saved.Value != "false" {
			t.Fatal("invalid batch partially updated settings")
		}
	}
	if err := a.ResetCategorySettings("retry"); err != nil {
		t.Fatal(err)
	}
	policy, rules := a.settingsService.RoutingPolicy(ctx)
	if policy != routing.DefaultCooldownPolicy() || rules != routing.DefaultRuleDefaults() {
		t.Fatal("reset failed to restore routing policy defaults")
	}
	for _, provider := range []string{"claude", "codex"} {
		plan := a.profileRuntimes[provider].Preview(ctx, 1, "test-model", true, routing.TurnInput{})
		if !plan.Decision.FallbackToDefault || a.poolRuntimes[provider].Config().CooldownPolicy != routing.DefaultCooldownPolicy() {
			t.Fatalf("reset did not reach %s runtime", provider)
		}
	}
}
