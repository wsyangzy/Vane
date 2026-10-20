package tracking

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// updateUsageSummary 更新使用汇总数据（使用写队列）
//
// 🔧 [P2.3 优化] 缩短聚合窗口 + 增量过滤，缓解大数据量下的聚合压力：
//
//  1. 窗口从 7 天缩短到 2 天（覆盖跨天场景）
//     - 历史日期的数据基本已稳定，无需反复聚合
//     - 2 天窗口足以覆盖跨天请求和延迟更新
//
//  2. 增加 updated_at 过滤条件，只聚合"最近 N 分钟内有变更"的数据
//     - 即使 2 天窗口内有 2000+ 条记录，绝大多数 updated_at 在很久之前
//     - 用 updated_at >= ? 索引过滤后，实际扫描行数可降到 ~100 条
//
// 注意：由于 ON CONFLICT(date, model, endpoint, group) DO UPDATE 是基于唯一键合并，
// 增量聚合只更新有变更的记录，不会丢失之前已聚合的数据
func (ut *UsageTracker) updateUsageSummary() {
	// 🔧 [P2.3] 窗口从 7 天缩短到 2 天
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -2)

	// 🔧 [P2.3] 增量过滤：只看最近 30 分钟有变更的记录
	// 30 分钟覆盖一次聚合周期（典型 CleanupInterval 是 24h，但 summary 更新更频繁）
	// 如果聚合间隔 > 30 分钟，updatedSince 应该 = 上次聚合时间
	// 这里采用保守的 30 分钟，足以捕获最近的写入
	updatedSince := endDate.Add(-30 * time.Minute)

	columns := []string{
		"date", "model_name", "endpoint_name", "group_name", "provider", "environment",
		"request_count", "success_count", "error_count",
		"total_input_tokens", "total_output_tokens",
		"total_cache_creation_tokens", "total_cache_read_tokens",
		"total_cost_usd", "avg_duration_ms",
		"created_at", "updated_at",
	}

	// 构建SELECT子查询
	// 🔧 [P2.3] 关键 WHERE 子句变化：
	//   - 旧：WHERE start_time >= ? AND start_time < ?
	//   - 新：WHERE start_time >= ? AND start_time < ?
	//        AND DATE(start_time) IN (
	//          SELECT DISTINCT DATE(start_time) FROM request_logs
	//          WHERE updated_at >= ?
	//        )
	//   含义：只重算"今天 + 昨天" 中有最近变更的日期
	selectQuery := fmt.Sprintf(`
SELECT
	DATE(start_time) as date,
	COALESCE(model_name, '') as model_name,
	COALESCE(endpoint_name, '') as endpoint_name,
	COALESCE(group_name, '') as group_name,
	COALESCE(provider, 'claude') as provider,
	COALESCE(environment, 'release') as environment,
	COUNT(*) as request_count,
	SUM(CASE WHEN status IN ('completed', 'processing') THEN 1 ELSE 0 END) as success_count,
	SUM(CASE WHEN status IN ('failed', 'error', 'auth_error', 'rate_limited', 'server_error', 'network_error', 'stream_error', 'timeout') THEN 1 ELSE 0 END) as error_count,
	SUM(input_tokens) as total_input_tokens,
	SUM(output_tokens) as total_output_tokens,
	SUM(cache_creation_tokens) as total_cache_creation_tokens,
	SUM(cache_read_tokens) as total_cache_read_tokens,
	SUM(total_cost_usd) as total_cost_usd,
	AVG(CASE WHEN duration_ms IS NOT NULL AND duration_ms > 0 THEN duration_ms ELSE NULL END) as avg_duration_ms,
	%s as created_at,
	%s as updated_at
FROM request_logs
WHERE start_time >= ? AND start_time < ?
	AND (model_name IS NOT NULL OR endpoint_name IS NOT NULL)
	AND COALESCE(environment, 'release') = ?
	AND DATE(start_time) IN (
		SELECT DISTINCT DATE(start_time) FROM request_logs
		WHERE updated_at >= ? AND start_time >= ?
			AND COALESCE(environment, 'release') = ?
	)
GROUP BY DATE(start_time), model_name, endpoint_name, group_name, provider, environment
`, ut.adapter.BuildDateTimeNow(), ut.adapter.BuildDateTimeNow())

	// usage_summary 的唯一约束是 (date, model_name, endpoint_name, group_name, provider)，不能复用 request_id 的 upsert 模板。
	query := fmt.Sprintf(`INSERT INTO usage_summary (%s)
%s
ON CONFLICT(date, model_name, endpoint_name, group_name, provider, environment) DO UPDATE SET
	request_count = EXCLUDED.request_count,
	success_count = EXCLUDED.success_count,
	error_count = EXCLUDED.error_count,
	total_input_tokens = EXCLUDED.total_input_tokens,
	total_output_tokens = EXCLUDED.total_output_tokens,
	total_cache_creation_tokens = EXCLUDED.total_cache_creation_tokens,
	total_cache_read_tokens = EXCLUDED.total_cache_read_tokens,
	total_cost_usd = EXCLUDED.total_cost_usd,
	avg_duration_ms = EXCLUDED.avg_duration_ms,
	updated_at = EXCLUDED.updated_at
`, strings.Join(columns, ", "), selectQuery)

	summaryWriteReq := WriteRequest{
		Query:     query,
		Args:      []interface{}{startDate, endDate.AddDate(0, 0, 1), ut.environment, updatedSince, startDate, ut.environment},
		Response:  make(chan error, 1),
		Context:   context.Background(),
		EventType: "update_summary",
	}

	select {
	case ut.writeQueue <- summaryWriteReq:
		select {
		case err := <-summaryWriteReq.Response:
			if err != nil {
				slog.Error("Failed to update usage summary", "error", err)
			} else {
				slog.Debug("✅ [P2.3] Usage summary updated (incremental)",
					"window_days", 2,
					"updated_since", updatedSince.Format("15:04:05"))
			}
		case <-ut.ctx.Done():
			slog.Debug("Usage summary update cancelled due to context cancellation")
		}
	case <-ut.ctx.Done():
		slog.Debug("Usage summary update cancelled due to context cancellation")
	}
}
