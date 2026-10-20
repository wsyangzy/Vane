package tracking

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newReplayTestManager(t *testing.T, dbPath string) (*ArchiveManager, *SQLiteAdapter) {
	t.Helper()
	adapter, err := NewSQLiteAdapter(DatabaseConfig{DatabasePath: dbPath, Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("NewSQLiteAdapter: %v", err)
	}
	if err := adapter.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := adapter.InitSchema(); err != nil {
		adapter.Close()
		t.Fatalf("InitSchema: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close() })
	return &ArchiveManager{
		adapter: adapter,
		config: ArchiveManagerConfig{
			BatchSize:   1,
			MaxRetry:    1,
			Environment: "release",
		},
		location: time.FixedZone("CST", 8*60*60),
	}, adapter
}

func writeReplayRecord(t *testing.T, path string, req *ActiveRequest, environment string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"environment": environment,
		"request":     req,
	})
	if err != nil {
		t.Fatalf("marshal replay record: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		t.Fatalf("write replay record: %v", err)
	}
}

func replayTestRequest(id string) *ActiveRequest {
	req := NewActiveRequest(id, "127.0.0.1", "test-agent", "POST", "/v1/responses", true)
	req.Status = "completed"
	req.Provider = "codex"
	req.ModelName = "gpt-6-astra"
	req.ReasoningEffort = `{"effort":"xhigh","context":"all_turns"}`
	req.InputTokens = 97073
	req.OutputTokens = 1171
	req.ReasoningTokens = 1034
	req.CacheReadTokens = 90236
	now := req.StartTime.Add(time.Second)
	req.EndTime = &now
	req.DurationMs = 1000
	return req
}

func TestReplayFailedArchivesPreservesFieldsAndRecoversOrphan(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "usage.replaying-data.db")
	manager, _ := newReplayTestManager(t, dbPath)
	dumpPath := manager.getDumpFilePath()

	writeReplayRecord(t, dumpPath+".20260909-010203", replayTestRequest("replay-backup"), "dev")
	writeReplayRecord(t, dumpPath, replayTestRequest("replay-main"), "dev")
	if err := manager.ReplayFailedArchives(); err != nil {
		t.Fatalf("replay active file: %v", err)
	}
	assertReplayedRequest(t, manager, "replay-backup", "dev")
	assertReplayedRequest(t, manager, "replay-main", "dev")

	orphanPath := dumpPath + ".replaying-crashed"
	writeReplayRecord(t, orphanPath, replayTestRequest("replay-orphan"), "release")
	if err := manager.ReplayFailedArchives(); err != nil {
		t.Fatalf("replay orphan file: %v", err)
	}
	assertReplayedRequest(t, manager, "replay-orphan", "release")
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("expected orphan replay file to be claimed, stat error=%v", err)
	}
}

func assertReplayedRequest(t *testing.T, manager *ArchiveManager, requestID, environment string) {
	t.Helper()
	var storedEnvironment, reasoning string
	var inputTokens, outputTokens, reasoningTokens, cacheReadTokens int64
	err := manager.adapter.GetReadDB().QueryRow(
		`SELECT environment, model_reasoning_effort, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens FROM request_logs WHERE request_id = ?`,
		requestID,
	).Scan(&storedEnvironment, &reasoning, &inputTokens, &outputTokens, &reasoningTokens, &cacheReadTokens)
	if err != nil {
		t.Fatalf("query replayed request %s: %v", requestID, err)
	}
	if storedEnvironment != environment || reasoning != replayTestRequest(requestID).ReasoningEffort {
		t.Fatalf("unexpected replay metadata: environment=%q reasoning=%q", storedEnvironment, reasoning)
	}
	if inputTokens != 97073 || outputTokens != 1171 || reasoningTokens != 1034 || cacheReadTokens != 90236 {
		t.Fatalf("unexpected replay tokens: input=%d output=%d reasoning=%d cache=%d", inputTokens, outputTokens, reasoningTokens, cacheReadTokens)
	}
}

func TestReplayFailedArchivesRestoresFileWhenDatabaseUnavailable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "usage.db")
	adapter, err := NewSQLiteAdapter(DatabaseConfig{DatabasePath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	manager := &ArchiveManager{
		adapter: adapter,
		config:  ArchiveManagerConfig{BatchSize: 1, MaxRetry: 1, Environment: "release"},
	}
	dumpPath := manager.getDumpFilePath()
	writeReplayRecord(t, dumpPath, replayTestRequest("replay-preserve"), "release")

	if err := manager.ReplayFailedArchives(); err == nil {
		t.Fatal("expected replay to fail without an open database")
	} else {
		t.Logf("expected replay failure: %v", err)
	}
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("expected failed replay file to be restored: %v", err)
	}
	if !strings.Contains(string(data), "replay-preserve") {
		t.Fatalf("restored replay file does not contain original request")
	}
	if orphans, _ := filepath.Glob(dumpPath + ".replaying-*"); len(orphans) != 0 {
		t.Fatalf("replay orphan files remain after restore: %v", orphans)
	}
}
