package tracking

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestResolveStreamOutcome(t *testing.T) {
	cases := []struct {
		name          string
		status        string
		streaming     bool
		failureReason string
		want          string
	}{
		{"流式完整", "completed", true, "", StreamOutcomeCompleted},
		{"流式 200 但缺 message_stop", "completed", true, "incomplete_stream", StreamOutcomeIncomplete},
		{"流式截断", "completed", true, "stream_truncated", StreamOutcomeIncomplete},
		{"非流式成功", "completed", false, "", StreamOutcomeNotStreaming},
		{"流式客户端断开", "cancelled", true, "", StreamOutcomeCanceled},
		{"非流式客户端断开", "cancelled", false, "", StreamOutcomeCanceled},
		{"流式失败", "failed", true, "stream_error", StreamOutcomeFailed},
		{"非流式失败", "failed", false, "route_exhausted", StreamOutcomeFailed},
		{"部分失败记录", "partial_failed", true, "stream_truncated", StreamOutcomeFailed},
		{"未结束", "", true, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveStreamOutcome(tc.status, tc.streaming, tc.failureReason); got != tc.want {
				t.Fatalf("ResolveStreamOutcome(%q, %v, %q) = %q, want %q", tc.status, tc.streaming, tc.failureReason, got, tc.want)
			}
		})
	}
}

// 成功率 SQL：新行按 stream_outcome，历史行（NULL）回退到旧口径
func TestSuccessRequestSQLConditionFallsBackForLegacyRows(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "outcome.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE request_logs (
		status TEXT, stream_outcome TEXT, http_status_code INTEGER, http_status_is_upstream BOOLEAN
	)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	rows := []struct {
		status     string
		outcome    any
		httpStatus any
		upstream   bool
		success    bool
	}{
		{"completed", nil, 200, true, true},           // 历史成功行
		{"processing", nil, nil, false, true},         // 历史进行中行（旧口径算成功）
		{"failed", nil, 502, true, false},             // 历史失败行
		{"completed", "completed", 200, true, true},   // 流式完整
		{"completed", "incomplete", 200, true, false}, // 流式缺结束事件：旧口径算成功，新口径不算
		{"completed", "not_streaming", 200, true, true},
		{"completed", "not_streaming", nil, false, true}, // 本地完成、无上游状态码
		{"cancelled", "canceled", nil, false, false},
		{"failed", "failed", 429, true, false},
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO request_logs VALUES (?, ?, ?, ?)`, row.status, row.outcome, row.httpStatus, row.upstream); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	query := `SELECT status, COALESCE(stream_outcome, ''), CASE WHEN ` + successRequestSQLCondition + ` THEN 1 ELSE 0 END FROM request_logs ORDER BY rowid`
	result, err := db.Query(query)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer result.Close()
	i := 0
	for result.Next() {
		var status, outcome string
		var success bool
		if err := result.Scan(&status, &outcome, &success); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if success != rows[i].success {
			t.Errorf("row %d status=%s outcome=%q: success=%v, want %v", i, status, outcome, success, rows[i].success)
		}
		// 内存侧判定必须与 SQL 一致
		httpStatus, _ := rows[i].httpStatus.(int)
		if got := isSuccessfulOutcome(outcome, status, httpStatus, rows[i].upstream); got != rows[i].success {
			t.Errorf("row %d isSuccessfulOutcome=%v, want %v", i, got, rows[i].success)
		}
		i++
	}
	if i != len(rows) {
		t.Fatalf("scanned %d rows, want %d", i, len(rows))
	}
}

// 缓存命中率按 provider 分别计算：Codex 的 input_tokens 已含 cached_tokens
func TestCacheHitPartsFollowProviderSemantics(t *testing.T) {
	// Claude：cache_read / (input + cache_creation + cache_read) = 800 / (100 + 100 + 800)
	num, den := cacheHitParts("claude", 100, 100, 800)
	if num != 800 || den != 1000 {
		t.Fatalf("claude parts = %d/%d, want 800/1000", num, den)
	}
	// Codex：cache_read / input = 800 / 1000（input 已含缓存，不再加 cache_read）
	num, den = cacheHitParts("codex", 1000, 0, 800)
	if num != 800 || den != 1000 {
		t.Fatalf("codex parts = %d/%d, want 800/1000", num, den)
	}
	// Codex 极旧行只存计费输入（cache_read > input）：分母补回 cache_read，命中率不超过 100%
	num, den = cacheHitParts("codex", 200, 0, 800)
	if num != 800 || den != 1000 {
		t.Fatalf("legacy codex parts = %d/%d, want 800/1000", num, den)
	}

	if rate := cacheHitRate(0, 0); rate != nil {
		t.Fatalf("empty usage should have no rate, got %v", *rate)
	}
	if rate := cacheHitRate(0, 10); rate == nil || *rate != 0 {
		t.Fatalf("zero cache reads should be a real 0%%, got %v", rate)
	}
}

// SQL 与内存两套口径必须对账一致
func TestCacheHitSQLMatchesInMemoryParts(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE request_logs (provider TEXT, input_tokens INTEGER, cache_creation_tokens INTEGER, cache_read_tokens INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	type row struct {
		provider                   string
		input, creation, cacheRead int64
	}
	rows := []row{{"claude", 100, 100, 800}, {"codex", 1000, 0, 800}, {"codex", 200, 0, 800}, {"claude", 0, 0, 0}}
	var wantNum, wantDen int64
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO request_logs VALUES (?, ?, ?, ?)`, r.provider, r.input, r.creation, r.cacheRead); err != nil {
			t.Fatalf("insert: %v", err)
		}
		num, den := cacheHitParts(r.provider, r.input, r.creation, r.cacheRead)
		wantNum += num
		wantDen += den
	}
	var gotNum, gotDen int64
	if err := db.QueryRowContext(context.Background(),
		`SELECT SUM(`+cacheHitNumeratorSQLExpression+`), SUM(`+cacheHitDenominatorSQLExpression+`) FROM request_logs`,
	).Scan(&gotNum, &gotDen); err != nil {
		t.Fatalf("query: %v", err)
	}
	if gotNum != wantNum || gotDen != wantDen {
		t.Fatalf("SQL parts = %d/%d, in-memory = %d/%d", gotNum, gotDen, wantNum, wantDen)
	}
}

func TestHotPoolCompleteAndArchiveResolvesStreamOutcome(t *testing.T) {
	pool := NewHotPool(DefaultHotPoolConfig())
	defer pool.Close()
	if err := pool.Add(NewActiveRequest("req-outcome", "127.0.0.1", "test", "POST", "/v1/messages", true)); err != nil {
		t.Fatalf("add: %v", err)
	}
	var archived *ActiveRequest
	pool.SetArchiveCallback(func(req *ActiveRequest) { archived = req })
	if err := pool.CompleteAndArchive("req-outcome", func(req *ActiveRequest) {
		req.Status = "completed"
		req.FailureReason = "incomplete_stream"
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if archived == nil || archived.StreamOutcome != StreamOutcomeIncomplete {
		t.Fatalf("archived outcome = %+v, want incomplete", archived)
	}
}
