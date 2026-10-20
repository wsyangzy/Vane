package storage

import (
	"context"
	"fmt"

	_ "modernc.org/sqlite"
)

const schemaVersionRepairCrossProviderMultiplierCost = "20260902_repair_cross_provider_multiplier_cost"

// repairCrossProviderMultiplierCost 重算被跨 provider 倍率污染的 Claude 请求成本。
//
// 倍率缓存曾以 "channel::group::endpoint" 为 key，由 Claude 与 Codex 共用一张 map。
// 两侧允许配置同名的渠道 / 凭证分组 / 端点（实际存在 anyrouter::默认分组::主站直连
// 在两个 provider 下同时成立），而 app.go 先写 Claude 段再写 Codex 段，后者静默覆盖
// 前者，于是 Claude 请求按 Codex 的倍率计费。实测 685 条 Claude 请求按 0.4 计价，
// 成本少算 $551.96。key 加 provider 维度后新请求已正确，此处只清理历史数据。
//
// 修复口径：按当前 Claude 凭证分组配置算出每条记录应有的总倍率，与落库值不符时按比例
// 还原各成本列并回写正确倍率。成本 = 基础单价 × 分项倍率 × 总倍率，本缺陷只污染总倍率
// （受影响记录的六项分项倍率与模型倍率实测均为 1.0），因此比例还原是精确的，无需重查定价。
//
// 只执行一次（schema_versions 幂等）：历史记录必须反映请求发生时的倍率。若每次启动都
// 比对当前配置，用户日后调整分组倍率会把历史账单一并改写。
//
// 汇总表无需同步：成本统计直接聚合 request_logs（queries.go 的
// queryUsageStatsTotalsFromSource），凭证分组日成本也是实时计算的。
func (s *SQLiteAdapter) repairCrossProviderMultiplierCost(ctx context.Context) error {
	applied, err := s.isSchemaVersionApplied(ctx, schemaVersionRepairCrossProviderMultiplierCost)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	for _, table := range []string{"request_logs", "claude_credential_groups", "codex_channels", "codex_credential_groups"} {
		exists, err := s.tableExists(ctx, table)
		if err != nil {
			return fmt.Errorf("failed to check %s for cross-provider multiplier repair: %w", table, err)
		}
		if !exists {
			// 表尚未建全（首次启动），不写标记，留待下次启动重判。
			return nil
		}
	}

	type pollutedRequest struct {
		id       int64
		recorded float64
		correct  float64
		cost     float64
	}

	// 判定由两个条件合成，缺一不可：
	//
	//  1. 落库倍率 ≠ 当前 Claude 分组配置 —— 必要但不充分。倍率是请求发生时的快照，
	//     用户新建分组或调整倍率后，早于该变更的记录本就与当前配置不同。实测存在这种
	//     记录（Right Code/aws 分组建立前的 7 条请求落库 1.0，建立后配置为 0.3），
	//     它们的 1.0 是当时的正确值，不能按今天的配置追溯改写。
	//
	//  2. 落库倍率 == 同名 Codex 渠道 + 分组的倍率 —— 这才是污染的指纹。旧 key 不含
	//     provider，Codex 段后写覆盖 Claude 段，因此被污染记录的倍率必然等于肇事方
	//     Codex 分组的倍率。反之，Claude 的 "Right Code" 在 Codex 侧叫 "RightCode"
	//     （无空格），渠道名对不上，那 7 条记录因此被自然排除。
	//
	// 该条件同时保证幂等：修正后落库倍率不再等于 Codex 侧的值。
	//
	// group_name 匹配不到 Claude 分组时（分组已删除），应有倍率取 1.0 —— 当时的真实
	// 倍率已无从考证，而 1.0 是配置缺失时计费链路本身的回退值。
	rows, err := s.GetReadDB().QueryContext(ctx, `
		SELECT r.id, r.cost_multiplier, COALESCE(g.cost_multiplier, 1.0), COALESCE(r.total_cost_usd, 0)
		FROM request_logs r
		LEFT JOIN claude_credential_groups g
			ON g.channel = r.channel AND g.name = r.group_name
		WHERE COALESCE(r.provider, 'claude') = 'claude'
			AND r.cost_multiplier > 0
			AND ABS(r.cost_multiplier - COALESCE(g.cost_multiplier, 1.0)) > 0.000000001
			AND EXISTS (
				SELECT 1
				FROM codex_credential_groups cg
				JOIN codex_channels cc ON cc.id = cg.channel_id
				WHERE cc.name = r.channel
					AND cg.name = r.group_name
					AND ABS(cg.cost_multiplier - r.cost_multiplier) < 0.000000001
			)
		ORDER BY r.id ASC
	`)
	if err != nil {
		return fmt.Errorf("failed to query Claude requests with foreign multipliers: %w", err)
	}
	polluted := make([]pollutedRequest, 0)
	for rows.Next() {
		var row pollutedRequest
		if err := rows.Scan(&row.id, &row.recorded, &row.correct, &row.cost); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan Claude request with foreign multiplier: %w", err)
		}
		polluted = append(polluted, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("failed to iterate Claude requests with foreign multipliers: %w", err)
	}
	rows.Close()

	if len(polluted) == 0 {
		return s.recordSchemaVersion(ctx, schemaVersionRepairCrossProviderMultiplierCost,
			"recompute Claude request costs polluted by provider-less multiplier keys")
	}

	s.logger.Warn(fmt.Sprintf("🔧 [数据库迁移] 检测到 %d 条 Claude 请求套用了其它 provider 的倍率，开始重算成本", len(polluted)))
	backupPath, err := s.backupDatabaseFile(ctx, "pre-multiplier-repair")
	if err != nil {
		return fmt.Errorf("failed to back up database before cross-provider multiplier repair: %w", err)
	}
	if backupPath != "" {
		s.logger.Info(fmt.Sprintf("💾 [数据库迁移] 重算前已备份数据库: %s", backupPath))
	}

	tx, err := s.GetWriteDB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cross-provider multiplier repair: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// cost_multiplier 同时出现在赋值右侧与左侧：SQLite 对同一条 UPDATE 的所有右侧
	// 表达式统一取旧值求值，因此这里的比例基于污染值计算，是安全的。
	const updateSQL = `
		UPDATE request_logs SET
			input_cost_usd = COALESCE(input_cost_usd, 0) * ?,
			output_cost_usd = COALESCE(output_cost_usd, 0) * ?,
			cache_creation_cost_usd = COALESCE(cache_creation_cost_usd, 0) * ?,
			cache_creation_5m_cost_usd = COALESCE(cache_creation_5m_cost_usd, 0) * ?,
			cache_creation_1h_cost_usd = COALESCE(cache_creation_1h_cost_usd, 0) * ?,
			cache_read_cost_usd = COALESCE(cache_read_cost_usd, 0) * ?,
			total_cost_usd = COALESCE(total_cost_usd, 0) * ?,
			cost_multiplier = ?
		WHERE id = ?`

	var costBefore, costAfter float64
	for _, row := range polluted {
		ratio := row.correct / row.recorded
		if _, err := tx.ExecContext(ctx, updateSQL,
			ratio, ratio, ratio, ratio, ratio, ratio, ratio, row.correct, row.id); err != nil {
			return fmt.Errorf("failed to recompute cost for request %d: %w", row.id, err)
		}
		costBefore += row.cost
		costAfter += row.cost * ratio
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cross-provider multiplier repair: %w", err)
	}

	s.logger.Info(fmt.Sprintf("✅ [数据库迁移] 跨 provider 倍率污染重算完成：修正 %d 条，成本合计 $%.4f → $%.4f",
		len(polluted), costBefore, costAfter))

	return s.recordSchemaVersion(ctx, schemaVersionRepairCrossProviderMultiplierCost,
		"recompute Claude request costs polluted by provider-less multiplier keys")
}
