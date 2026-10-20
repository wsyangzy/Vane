package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// 回归测试：历史迁移残留会让 endpoints.credential_group_id 指向其它渠道的凭证分组。
// 这类孤儿端点游离在所有分组之外 —— UI 分组批量操作碰不到它，排序又把它排到最后，
// 于是「不参与转移」永远关不掉。InitSchema 必须自愈：重影删除、无冲突的归位、无默认分组的跳过。
func TestSQLiteAdapter_InitSchema_RepairsClaudeCredentialGroupOwnership(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "credential-group-ownership.db")
	adapter, err := NewSQLiteAdapter(DatabaseConfig{DatabasePath: dbPath, Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("NewSQLiteAdapter: %v", err)
	}
	if err := adapter.Open(); err != nil {
		t.Fatalf("adapter.Open: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close() })

	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("initial InitSchema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	db := adapter.GetWriteDB()

	// 两个渠道各有默认分组，alpha 还有一个非默认分组；orphanage 渠道故意不配默认分组。
	if _, err := db.ExecContext(ctx, `
		INSERT INTO claude_credential_groups (id, channel, name, token, is_default) VALUES
			(101, 'alpha', '默认分组', 'sk-alpha-default', 1),
			(102, 'alpha', 'backup',   'sk-alpha-backup',  0),
			(103, 'beta',  '默认分组', 'sk-beta-default',  1),
			(104, 'orphanage', 'no-default', 'sk-orphanage', 0);

		INSERT INTO endpoints (id, channel, credential_group_id, name, url, priority, failover_enabled, enabled) VALUES
			-- alpha 默认分组内的正主，用户已把参与转移关掉
			(201, 'alpha', 101, 'main',  'https://alpha.example.com',      0, 0, 0),
			(202, 'alpha', 101, 'spare', 'https://alpha-spare.example.com', 1, 0, 0),
			-- alpha 非默认分组内的合法端点，不能被动到
			(203, 'alpha', 102, 'main',  'https://alpha.example.com',      0, 1, 1),
			-- 孤儿①：分组 103 属于 beta，且默认分组内已有同名 main → 应删除
			(204, 'alpha', 103, 'main',  'https://alpha-shadow.example.com', 5, 1, 0),
			-- 孤儿②：分组 103 属于 beta，名字不同但 URL 与正主 spare 相同 → 应删除
			(205, 'alpha', 103, 'shadow-by-url', 'https://alpha-spare.example.com', 6, 1, 0),
			-- 孤儿③：分组 103 属于 beta，名字和 URL 都不冲突 → 应归位到 101
			(206, 'alpha', 103, 'unique', 'https://alpha-unique.example.com', 7, 1, 0),
			-- 孤儿④：分组 101 属于 alpha；backfill 会先给本渠道补建默认分组，随后应归位到那个新分组
			(207, 'orphanage', 101, 'stranded', 'https://orphanage.example.com', 0, 1, 0);
	`); err != nil {
		t.Fatalf("seed mismatched credential groups: %v", err)
	}

	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("repairing InitSchema: %v", err)
	}

	endpointGroup := func(id int64) (int64, bool) {
		t.Helper()
		var groupID int64
		rows, err := adapter.GetReadDB().QueryContext(ctx,
			`SELECT credential_group_id FROM endpoints WHERE id = ?`, id)
		if err != nil {
			t.Fatalf("query endpoint %d: %v", id, err)
		}
		defer rows.Close()
		if !rows.Next() {
			return 0, false
		}
		if err := rows.Scan(&groupID); err != nil {
			t.Fatalf("scan endpoint %d: %v", id, err)
		}
		return groupID, true
	}

	// 重影端点被删除
	for _, id := range []int64{204, 205} {
		if _, ok := endpointGroup(id); ok {
			t.Fatalf("expected shadow endpoint %d to be removed", id)
		}
	}
	// 无冲突的孤儿归位到本渠道默认分组
	if groupID, ok := endpointGroup(206); !ok || groupID != 101 {
		t.Fatalf("expected endpoint 206 reassigned to group 101, got group=%d exists=%v", groupID, ok)
	}
	// 原本没有默认分组的渠道：backfill 补建后，孤儿端点归位到该渠道自己的默认分组
	var orphanageDefaultID int64
	if err := adapter.GetReadDB().QueryRowContext(ctx,
		`SELECT id FROM claude_credential_groups WHERE channel = 'orphanage' AND is_default = 1`).Scan(&orphanageDefaultID); err != nil {
		t.Fatalf("query orphanage default group: %v", err)
	}
	if groupID, ok := endpointGroup(207); !ok || groupID != orphanageDefaultID {
		t.Fatalf("expected endpoint 207 reassigned to orphanage default group %d, got group=%d exists=%v",
			orphanageDefaultID, groupID, ok)
	}
	// 合法端点及用户显式关闭的开关都不受影响
	for id, wantGroup := range map[int64]int64{201: 101, 202: 101, 203: 102} {
		if groupID, ok := endpointGroup(id); !ok || groupID != wantGroup {
			t.Fatalf("expected legit endpoint %d to stay in group %d, got group=%d exists=%v", id, wantGroup, groupID, ok)
		}
	}
	var failoverEnabled int
	if err := adapter.GetReadDB().QueryRowContext(ctx,
		`SELECT failover_enabled FROM endpoints WHERE id = 201`).Scan(&failoverEnabled); err != nil {
		t.Fatalf("query endpoint 201 failover flag: %v", err)
	}
	if failoverEnabled != 0 {
		t.Fatalf("expected user-disabled failover flag preserved, got %d", failoverEnabled)
	}

	var mismatched int
	if err := adapter.GetReadDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM endpoints e
		JOIN claude_credential_groups g ON g.id = e.credential_group_id
		WHERE g.channel <> e.channel
	`).Scan(&mismatched); err != nil {
		t.Fatalf("query remaining mismatches: %v", err)
	}
	if mismatched != 0 {
		t.Fatalf("expected no mismatch left, got %d", mismatched)
	}

	var versionCount int
	if err := adapter.GetReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_versions WHERE version = ?`,
		schemaVersionRepairClaudeCredentialGroupOwnership).Scan(&versionCount); err != nil {
		t.Fatalf("query migration version: %v", err)
	}
	if versionCount != 1 {
		t.Fatalf("expected one repair migration record, got %d", versionCount)
	}

	// 幂等：再跑一次不应有任何变化
	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("idempotent InitSchema: %v", err)
	}
	if groupID, ok := endpointGroup(206); !ok || groupID != 101 {
		t.Fatalf("expected repeated InitSchema to keep endpoint 206 at group 101, got group=%d exists=%v", groupID, ok)
	}
	var endpointCount int
	if err := adapter.GetReadDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM endpoints`).Scan(&endpointCount); err != nil {
		t.Fatalf("count endpoints: %v", err)
	}
	if endpointCount != 5 {
		t.Fatalf("expected 5 endpoints to remain after repair, got %d", endpointCount)
	}
}

// 防御分支：渠道确实没有默认分组时，孤儿端点必须原样保留 —— 不能删，也不能塞进别的渠道。
func TestRepairClaudeCredentialGroupOwnership_SkipsChannelWithoutDefaultGroup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "credential-group-no-default.db")
	adapter, err := NewSQLiteAdapter(DatabaseConfig{DatabasePath: dbPath, Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("NewSQLiteAdapter: %v", err)
	}
	if err := adapter.Open(); err != nil {
		t.Fatalf("adapter.Open: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close() })

	if err := adapter.InitSchema(); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	// 先建好分组，再插孤儿端点：外键是 ON DELETE SET NULL，顺序颠倒会把 group_id 清空。
	if _, err := adapter.GetWriteDB().ExecContext(ctx, `
		INSERT INTO claude_credential_groups (id, channel, name, token, is_default) VALUES
			(301, 'gamma', '默认分组', 'sk-gamma', 1),
			(302, 'delta',  'only-non-default', 'sk-delta', 0);

		INSERT INTO endpoints (id, channel, credential_group_id, name, url, failover_enabled, enabled)
		VALUES (401, 'delta', 301, 'stranded', 'https://delta.example.com', 1, 0);
	`); err != nil {
		t.Fatalf("seed channel without default group: %v", err)
	}

	if err := adapter.repairClaudeCredentialGroupOwnership(ctx); err != nil {
		t.Fatalf("repairClaudeCredentialGroupOwnership: %v", err)
	}

	var groupID int64
	if err := adapter.GetReadDB().QueryRowContext(ctx,
		`SELECT credential_group_id FROM endpoints WHERE id = 401`).Scan(&groupID); err != nil {
		t.Fatalf("query stranded endpoint: %v", err)
	}
	if groupID != 301 {
		t.Fatalf("expected stranded endpoint left untouched at group 301, got %d", groupID)
	}
}
