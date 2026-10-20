package codexproxy

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestEndpointManagerActivatesNextEligibleChannel(t *testing.T) {
	manager, db, cleanup := newChannelFailoverTestManager(t)
	defer cleanup()

	ctx := context.Background()
	active := manager.GetActiveChannel()
	if active == nil {
		t.Fatal("expected active channel")
	}
	next, err := manager.ActivateNextChannelForFailover(ctx, map[int64]struct{}{active.ID: {}}, active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.Name != "backup" {
		t.Fatalf("next channel = %q, want backup", next.Name)
	}
	var activeName string
	if err := db.QueryRow(`SELECT name FROM codex_channels WHERE is_active = 1`).Scan(&activeName); err != nil {
		t.Fatal(err)
	}
	if activeName != "backup" {
		t.Fatalf("database active channel = %q", activeName)
	}
}

func TestEndpointManagerRestoresPreviousChannelWhenReloadFails(t *testing.T) {
	manager, db, cleanup := newChannelFailoverTestManager(t)
	defer cleanup()
	active := manager.GetActiveChannel()
	if _, err := db.Exec(`DROP TABLE codex_endpoints`); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ActivateNextChannelForFailover(context.Background(), map[int64]struct{}{active.ID: {}}, active.ID); err == nil {
		t.Fatal("expected reload failure")
	}
	var activeName string
	if err := db.QueryRow(`SELECT name FROM codex_channels WHERE is_active = 1`).Scan(&activeName); err != nil {
		t.Fatal(err)
	}
	if activeName != "primary" {
		t.Fatalf("active channel after rollback = %q, want primary", activeName)
	}
}

func TestEndpointManagerHealthScopeIncludesParticipatingBackupChannels(t *testing.T) {
	manager, _, cleanup := newChannelFailoverTestManager(t)
	defer cleanup()

	allParticipating, err := manager.GetHealthCheckEndpoints(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(allParticipating) != 2 {
		t.Fatalf("participating health endpoints = %d, want 2", len(allParticipating))
	}
	activeOnly, err := manager.GetHealthCheckEndpoints(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(activeOnly) != 1 || activeOnly[0].ChannelID != manager.GetActiveChannel().ID {
		t.Fatalf("active-only health endpoints = %#v", activeOnly)
	}
}

func TestEndpointManagerReloadPreservesBackupChannelHealthState(t *testing.T) {
	manager, _, cleanup := newChannelFailoverTestManager(t)
	defer cleanup()

	endpoints, err := manager.GetHealthCheckEndpoints(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	var backup *CodexEndpoint
	for _, endpoint := range endpoints {
		if endpoint.ChannelID != manager.GetActiveChannel().ID {
			backup = endpoint
			break
		}
	}
	if backup == nil {
		t.Fatal("expected a participating backup endpoint")
	}

	manager.MarkUnhealthyWithDetail(backup.ID, 0, "connection refused")
	if err := manager.Reload(); err != nil {
		t.Fatal(err)
	}
	if manager.IsHealthy(backup.ID) {
		t.Fatal("backup endpoint health state was lost after active-channel reload")
	}
	statusCode, reason, ok := manager.GetFailureDetail(backup.ID)
	if !ok || statusCode != 0 || reason != "connection refused" {
		t.Fatalf("backup failure detail = (%d, %q, %v)", statusCode, reason, ok)
	}
}

func TestHandlerStartsEveryRetryRoundFromHighestPriorityChannel(t *testing.T) {
	var mu sync.Mutex
	sequence := make([]string, 0, 6)
	newUpstream := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			sequence = append(sequence, name)
			mu.Unlock()
			http.Error(w, `{"error":{"message":"temporary failure"}}`, http.StatusServiceUnavailable)
		}))
	}
	primary := newUpstream("primary")
	defer primary.Close()
	backup := newUpstream("backup")
	defer backup.Close()

	manager, db, cleanup := newChannelFailoverTestManager(t)
	defer cleanup()
	if _, err := db.Exec(`UPDATE codex_endpoints SET base_url = CASE
		WHEN name = 'primary-endpoint' THEN ?
		WHEN name = 'backup-endpoint' THEN ?
		ELSE base_url END`, primary.URL, backup.URL); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reload(); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(
		manager,
		time.Second,
		config.RetryConfig{MaxAttempts: 1},
		config.FailoverConfig{Enabled: true, LoopRetry: true, MaxLoops: 3},
		nil,
		newTestLogger(),
		nil,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5","stream":false}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	mu.Lock()
	got := strings.Join(sequence, ",")
	mu.Unlock()
	if want := "primary,backup,primary,backup,primary,backup"; got != want {
		t.Fatalf("channel sequence = %q, want %q", got, want)
	}
}

func TestHandlerDisabledFailoverRetriesOnlyManuallyActiveChannel(t *testing.T) {
	var mu sync.Mutex
	primaryHits := 0
	backupHits := 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		primaryHits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		backupHits++
		mu.Unlock()
		http.Error(w, `{"error":{"message":"temporary failure"}}`, http.StatusServiceUnavailable)
	}))
	defer backup.Close()

	manager, db, cleanup := newChannelFailoverTestManager(t)
	defer cleanup()
	if _, err := db.Exec(`
		UPDATE codex_endpoints SET base_url = CASE
			WHEN name = 'primary-endpoint' THEN ?
			WHEN name = 'backup-endpoint' THEN ?
			ELSE base_url END;
		UPDATE codex_channels SET is_active = 0;
		UPDATE codex_channels SET is_active = 1 WHERE name = 'backup';
	`, primary.URL, backup.URL); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reload(); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(
		manager,
		time.Second,
		config.RetryConfig{MaxAttempts: 1},
		config.FailoverConfig{Enabled: false, LoopRetry: true, MaxLoops: 3},
		nil,
		newTestLogger(),
		nil,
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5","stream":false}`)))

	mu.Lock()
	gotPrimaryHits := primaryHits
	gotBackupHits := backupHits
	mu.Unlock()
	if gotPrimaryHits != 0 {
		t.Fatalf("disabled channel failover reached primary %d times", gotPrimaryHits)
	}
	if gotBackupHits != 3 {
		t.Fatalf("backup hits = %d, want 3 current-channel loop retries", gotBackupHits)
	}
	active := manager.GetActiveChannel()
	if active == nil || active.Name != "backup" {
		t.Fatalf("active channel = %#v, want backup", active)
	}
}

func TestHandlerHotDisableStopsInFlightCrossChannelFailover(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var enterOnce sync.Once
	var mu sync.Mutex
	primaryHits := 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		primaryHits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		enterOnce.Do(func() { close(entered) })
		<-release
		http.Error(w, `{"error":{"message":"temporary failure"}}`, http.StatusServiceUnavailable)
	}))
	defer backup.Close()

	manager, db, cleanup := newChannelFailoverTestManager(t)
	defer cleanup()
	if _, err := db.Exec(`
		UPDATE codex_endpoints SET base_url = CASE
			WHEN name = 'primary-endpoint' THEN ?
			WHEN name = 'backup-endpoint' THEN ?
			ELSE base_url END;
		UPDATE codex_channels SET is_active = 0;
		UPDATE codex_channels SET is_active = 1 WHERE name = 'backup';
	`, primary.URL, backup.URL); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reload(); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(
		manager,
		time.Second,
		config.RetryConfig{MaxAttempts: 1},
		config.FailoverConfig{Enabled: true},
		nil,
		newTestLogger(),
		nil,
	)
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5","stream":false}`)))
	}()
	<-entered

	updateDone := make(chan struct{})
	go func() {
		handler.UpdateConfig(config.RetryConfig{MaxAttempts: 1}, config.FailoverConfig{Enabled: false})
		close(updateDone)
	}()
	select {
	case <-updateDone:
	case <-time.After(250 * time.Millisecond):
		close(release)
		<-requestDone
		t.Fatal("UpdateConfig blocked behind an in-flight request")
	}
	close(release)
	<-requestDone

	mu.Lock()
	gotPrimaryHits := primaryHits
	mu.Unlock()
	if gotPrimaryHits != 0 {
		t.Fatalf("hot-disabled channel failover reached primary %d times", gotPrimaryHits)
	}
	active := manager.GetActiveChannel()
	if active == nil || active.Name != "backup" {
		t.Fatalf("active channel = %#v, want backup", active)
	}
}

func TestHandlerRebuildsRouteAfterManualChannelChange(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	primaryBody := make(chan string, 1)
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		primaryBody <- string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"primary-model","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		http.Error(w, `{"error":{"message":"temporary failure"}}`, http.StatusServiceUnavailable)
	}))
	defer backup.Close()

	manager, db, cleanup := newChannelFailoverTestManager(t)
	defer cleanup()
	if _, err := db.Exec(`
		UPDATE codex_endpoints SET base_url = CASE
			WHEN name = 'primary-endpoint' THEN ?
			WHEN name = 'backup-endpoint' THEN ?
			ELSE base_url END;
		UPDATE codex_channels SET model = CASE
			WHEN name = 'primary' THEN 'primary-model'
			WHEN name = 'backup' THEN 'backup-model'
			ELSE model END;
		UPDATE codex_channels SET is_active = 0;
		UPDATE codex_channels SET is_active = 1 WHERE name = 'backup';
	`, primary.URL, backup.URL); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reload(); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(
		manager,
		time.Second,
		config.RetryConfig{MaxAttempts: 1},
		config.FailoverConfig{Enabled: true},
		nil,
		newTestLogger(),
		nil,
	)
	recorder := httptest.NewRecorder()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"client-model","stream":false}`)))
	}()
	<-entered

	if _, err := db.Exec(`
		UPDATE codex_channels SET is_active = 0;
		UPDATE codex_channels SET is_active = 1 WHERE name = 'primary';
	`); err != nil {
		close(release)
		t.Fatal(err)
	}
	if err := manager.Reload(); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	<-requestDone

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", recorder.Code, recorder.Body.String())
	}
	select {
	case body := <-primaryBody:
		if !strings.Contains(body, `"model":"primary-model"`) {
			t.Fatalf("primary request body = %q, want rebuilt primary model", body)
		}
	case <-time.After(time.Second):
		t.Fatal("primary channel was not reached after manual activation")
	}
}

func TestEndpointManagerRejectsStaleChannelActivation(t *testing.T) {
	manager, db, cleanup := newChannelFailoverTestManager(t)
	defer cleanup()

	previous := manager.GetActiveChannel()
	if previous == nil {
		t.Fatal("expected active channel")
	}
	if _, err := db.Exec(`
		UPDATE codex_channels SET is_active = 0;
		UPDATE codex_channels SET is_active = 1 WHERE name = 'backup';
	`); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reload(); err != nil {
		t.Fatal(err)
	}

	_, err := manager.ActivateNextChannelForFailover(context.Background(), map[int64]struct{}{}, previous.ID)
	if !errors.Is(err, errCodexActiveChannelChanged) {
		t.Fatalf("expected stale activation error, got %v", err)
	}
	active := manager.GetActiveChannel()
	if active == nil || active.Name != "backup" {
		t.Fatalf("active channel = %#v, want backup", active)
	}
}

func newChannelFailoverTestManager(t *testing.T) (*EndpointManager, *sql.DB, func()) {
	t.Helper()
	adapter, err := tracking.NewSQLiteAdapter(tracking.DatabaseConfig{
		DatabasePath: filepath.Join(t.TempDir(), "failover.db"), Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Open(); err != nil {
		t.Fatal(err)
	}
	if err := adapter.InitSchema(); err != nil {
		t.Fatal(err)
	}
	db := adapter.GetWriteDB()
	_, err = db.Exec(`
		INSERT INTO codex_channels (name, provider_type, api_key, model, is_active, priority, failover_enabled) VALUES
		('primary', 'openai-compatible', '', '', 1, 1, 1),
		('paused', 'openai-compatible', '', '', 0, 2, 0),
		('backup', 'openai-compatible', '', '', 0, 3, 1);
		INSERT INTO codex_endpoints (channel_id, name, base_url, failover_enabled)
		SELECT id, name || '-endpoint', 'https://' || name || '.invalid', 1 FROM codex_channels;
	`)
	if err != nil {
		adapter.Close()
		t.Fatal(err)
	}
	channelStore := codexstore.NewSQLiteCodexChannelStore(db)
	endpointStore := codexstore.NewSQLiteCodexEndpointStore(db)
	manager := NewEndpointManager(channelStore, endpointStore, nil, newTestLogger())
	if err := manager.Reload(); err != nil {
		adapter.Close()
		t.Fatal(err)
	}
	return manager, db, func() { _ = adapter.Close() }
}
