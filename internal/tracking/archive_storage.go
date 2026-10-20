package tracking

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
)

// batchInsert 执行批量插入
//
// 错误处理重点（2026-05-14）：
// SQLite 单连接写入池（MaxOpenConns=1）+ modernc.org/sqlite 驱动场景下，
// 当 tx.Commit() 因 SQLITE_BUSY 失败时，连接虽被归还到 Go pool，
// 但底层 SQLite C handle 仍处于 BEGIN 事务状态。
// 下次 BeginTx 命中同一污染连接，会立即报 "cannot start a transaction within a transaction (1)"，
// 导致写入彻底瘫痪、所有读请求也被锁阻塞。
//
// 修复：用 sql.Conn 显式管理连接生命周期，commit/写入出错时调用 evictConn 强制驱逐该连接，
// 让 sql.DB 在下次取连接时重建一个干净的 SQLite handle。
func (am *ArchiveManager) batchInsert(events []*ArchiveEvent) error {
	if am.adapter == nil {
		return fmt.Errorf("database adapter not initialized")
	}

	if am.writeLocker != nil {
		am.writeLocker.Lock()
		defer am.writeLocker.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	writeDB := am.adapter.GetWriteDB()
	if writeDB == nil {
		return fmt.Errorf("write DB not available")
	}

	// 关键：用 *sql.Conn 显式持有连接，便于在事务出错时驱逐它
	conn, err := writeDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get write conn: %w", err)
	}
	connEvicted := false
	defer func() {
		if closeErr := conn.Close(); closeErr != nil && !connEvicted {
			slog.Warn("📦 关闭归档数据库连接失败", "error", closeErr)
		}
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		// BeginTx 失败极可能是连接已被污染，强制驱逐
		connEvicted = true
		if evictErr := evictConn(conn); evictErr != nil {
			slog.Warn("📦 驱逐归档数据库污染连接失败", "error", evictErr)
		}
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	// defer 中的 rollback 是兜底，正常路径走 commit
	committed := false
	defer func() {
		// 只有未 commit 时才需要 rollback
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				slog.Debug("📦 回滚归档事务失败", "error", rbErr)
			}
		}
	}()

	// 🔧 [P1.6 修复] 使用 INSERT ... ON CONFLICT(request_id) DO UPDATE，
	// 避免单条 request_id 唯一约束冲突导致整批回滚。
	// 冲突来源：HotPool 满时降级到 traditional eventChan 路径，可能先写入；
	//          然后 HotPool 归档时再次写入相同 request_id → 旧实现整批失败。
	// 新实现：冲突时用归档数据（含完整 token 与成本）覆盖，因为归档总是后到达的"权威"版本。
	const archiveColumns = `request_id, client_ip, user_agent, method, path,
			start_time, end_time, duration_ms, output_sent_at,
			channel, endpoint_name, endpoint_chain, group_name, model_name,
			original_model_name, model_rewrite_source, model_rewrite_target, route_snapshot, model_reasoning_effort, service_tier,
			status, stream_outcome, http_status_code, http_status_is_upstream, retry_count,
			failure_reason, last_failure_reason, cancel_reason,
			is_streaming, provider, environment, privacy_redacted, privacy_redaction_summary,
			input_tokens, output_tokens,
			reasoning_tokens, billable_output_tokens, billing_output_token_source, billing_rule_snapshot,
			cache_creation_tokens, cache_creation_5m_tokens, cache_creation_1h_tokens,
			cache_read_tokens,
			input_cost_usd, output_cost_usd,
			cache_creation_cost_usd, cache_creation_5m_cost_usd, cache_creation_1h_cost_usd,
			cache_read_cost_usd, total_cost_usd,
			cost_multiplier, input_cost_multiplier, output_cost_multiplier,
			cache_creation_cost_multiplier, cache_creation_cost_multiplier_1h, cache_read_cost_multiplier,
			model_cost_multiplier, model_input_cost_multiplier, model_output_cost_multiplier, model_cache_read_cost_multiplier`
	columns := strings.Split(strings.ReplaceAll(archiveColumns, "\n", " "), ",")
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",")
	insertSQL := fmt.Sprintf(`
		INSERT INTO request_logs (%s) VALUES (%s)
		ON CONFLICT(request_id) DO UPDATE SET
			end_time = excluded.end_time,
			duration_ms = excluded.duration_ms,
			output_sent_at = COALESCE(request_logs.output_sent_at, excluded.output_sent_at),
			endpoint_name = excluded.endpoint_name,
			endpoint_chain = excluded.endpoint_chain,
			model_name = excluded.model_name,
			original_model_name = excluded.original_model_name,
			model_rewrite_source = excluded.model_rewrite_source,
			model_rewrite_target = excluded.model_rewrite_target,
			route_snapshot = excluded.route_snapshot,
			model_reasoning_effort = excluded.model_reasoning_effort,
			service_tier = excluded.service_tier,
			status = excluded.status,
			stream_outcome = excluded.stream_outcome,
			http_status_code = excluded.http_status_code,
			http_status_is_upstream = excluded.http_status_is_upstream,
			retry_count = excluded.retry_count,
			failure_reason = excluded.failure_reason,
			last_failure_reason = excluded.last_failure_reason,
			cancel_reason = excluded.cancel_reason,
			is_streaming = excluded.is_streaming,
			provider = excluded.provider,
			environment = excluded.environment,
			privacy_redacted = excluded.privacy_redacted,
			privacy_redaction_summary = excluded.privacy_redaction_summary,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			reasoning_tokens = excluded.reasoning_tokens,
			billable_output_tokens = excluded.billable_output_tokens,
			billing_output_token_source = excluded.billing_output_token_source,
			billing_rule_snapshot = excluded.billing_rule_snapshot,
			cache_creation_tokens = excluded.cache_creation_tokens,
			cache_creation_5m_tokens = excluded.cache_creation_5m_tokens,
			cache_creation_1h_tokens = excluded.cache_creation_1h_tokens,
			cache_read_tokens = excluded.cache_read_tokens,
			input_cost_usd = excluded.input_cost_usd,
			output_cost_usd = excluded.output_cost_usd,
			cache_creation_cost_usd = excluded.cache_creation_cost_usd,
			cache_creation_5m_cost_usd = excluded.cache_creation_5m_cost_usd,
			cache_creation_1h_cost_usd = excluded.cache_creation_1h_cost_usd,
			cache_read_cost_usd = excluded.cache_read_cost_usd,
			total_cost_usd = excluded.total_cost_usd,
			cost_multiplier = excluded.cost_multiplier,
			input_cost_multiplier = excluded.input_cost_multiplier,
			output_cost_multiplier = excluded.output_cost_multiplier,
			cache_creation_cost_multiplier = excluded.cache_creation_cost_multiplier,
			cache_creation_cost_multiplier_1h = excluded.cache_creation_cost_multiplier_1h,
			cache_read_cost_multiplier = excluded.cache_read_cost_multiplier,
			model_cost_multiplier = excluded.model_cost_multiplier,
			model_input_cost_multiplier = excluded.model_input_cost_multiplier,
			model_output_cost_multiplier = excluded.model_output_cost_multiplier,
			model_cache_read_cost_multiplier = excluded.model_cache_read_cost_multiplier
	`, archiveColumns, placeholders)
	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			slog.Warn("📦 关闭归档批量写入语句失败", "error", closeErr)
		}
	}()

	for _, event := range events {
		req := event.Request
		httpStatus, httpStatusIsUpstream := normalizeUpstreamHTTPStatus(req.HTTPStatus, req.HTTPStatusIsUpstream)
		var persistedHTTPStatus any
		if httpStatusIsUpstream {
			persistedHTTPStatus = httpStatus
		}

		// 计算分开的成本
		costBreakdown := am.calculateCost(req)

		// 格式化时间
		startTime := am.formatTime(req.StartTime)
		var endTime interface{}
		if req.EndTime != nil {
			endTime = am.formatTime(*req.EndTime)
		} else {
			endTime = nil
		}
		var outputSentAt interface{}
		if req.OutputSentAt != nil {
			outputSentAt = am.formatTime(*req.OutputSentAt)
		} else {
			outputSentAt = nil
		}

		_, err := stmt.ExecContext(ctx,
			req.RequestID,
			req.ClientIP,
			req.UserAgent,
			req.Method,
			req.Path,
			startTime,
			endTime,
			req.DurationMs,
			outputSentAt,
			req.Channel,
			req.EndpointName,
			req.EndpointChain,
			req.GroupName,
			req.ModelName,
			nullString(req.OriginalModelName),
			nullString(req.ModelRewriteSource),
			nullString(req.ModelRewriteTarget),
			nullString(req.RouteSnapshot),
			nullString(req.ReasoningEffort),
			nullString(req.ServiceTier),
			req.Status,
			nullString(req.StreamOutcome),
			persistedHTTPStatus,
			httpStatusIsUpstream,
			req.RetryCount,
			nullString(req.FailureReason),
			nullString(req.LastFailureReason),
			nullString(req.CancelReason),
			req.IsStreaming,
			defaultProvider(req.Provider),
			eventEnvironment(event, am.config.Environment),
			req.PrivacyRedacted,
			nullString(req.PrivacySummary),
			req.InputTokens,
			req.OutputTokens,
			req.ReasoningTokens,
			req.BillableOutputTokens,
			NormalizeBillingOutputTokenSource(req.BillingOutputTokenSource),
			nullString(req.BillingRuleSnapshot),
			req.CacheCreationTokens,
			req.CacheCreation5mTokens,
			req.CacheCreation1hTokens,
			req.CacheReadTokens,
			costBreakdown.InputCost,
			costBreakdown.OutputCost,
			costBreakdown.CacheCreationCost,   // 总成本（向后兼容）
			costBreakdown.CacheCreation5mCost, // 5分钟缓存成本
			costBreakdown.CacheCreation1hCost, // 1小时缓存成本
			costBreakdown.CacheReadCost,
			costBreakdown.TotalCost,
			// 倍率快照（默认 1.0）
			defaultMultiplier(req.CostMultiplier),
			defaultMultiplier(req.InputCostMultiplier),
			defaultMultiplier(req.OutputCostMultiplier),
			defaultMultiplier(req.CacheCreationCostMultiplier),
			defaultMultiplier(req.CacheCreationCostMultiplier1h),
			defaultMultiplier(req.CacheReadCostMultiplier),
			defaultMultiplier(req.ModelCostMultiplier),
			defaultMultiplier(req.ModelInputCostMultiplier),
			defaultMultiplier(req.ModelOutputCostMultiplier),
			defaultMultiplier(req.ModelCacheReadCostMultiplier),
		)
		if err != nil {
			return fmt.Errorf("failed to insert request %s: %w", req.RequestID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		// Commit 失败（通常是 SQLITE_BUSY），驱逐这条连接避免后续 BeginTx 在污染连接上嵌套报错
		connEvicted = true
		if evictErr := evictConn(conn); evictErr != nil {
			slog.Warn("📦 驱逐归档数据库污染连接失败", "error", evictErr)
		}
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true

	// 🔧 [P6 新增] 归档成功后更新内存计数器
	// 注意：ON CONFLICT DO UPDATE 路径属于罕见场景（HotPool 溢出后又重新归档同 ID），
	// 这里按"全部为新插入"处理以保持 KISS；少量漂移可在应用重启时自然修复。
	if am.dbCounter != nil {
		providers := make([]string, 0, len(events))
		for _, event := range events {
			if event == nil || event.Request == nil {
				continue
			}
			providers = append(providers, defaultProvider(event.Request.Provider))
		}
		am.dbCounter.IncBatch(providers)
	}
	// 在 batchInsert 内通知，常规归档与失败文件回放两条写入路径都会使统计缓存失效
	am.notifyArchiveSuccess(events)

	return nil
}

// formatTime 格式化时间为易读格式（使用配置的时区）
// 格式：2025-12-04 17:18:48（北京时间，无时区后缀）
func (am *ArchiveManager) formatTime(t time.Time) string {
	if am.location != nil {
		t = t.In(am.location)
	}
	return t.Format("2006-01-02 15:04:05")
}

// nullString 处理空字符串为SQL NULL
func nullString(s string) interface{} {
	if s == "" {
		return sql.NullString{}
	}
	return s
}

// defaultProvider 返回提供商标识，为空时默认 "claude"。
func defaultProvider(p string) string {
	if p == "" {
		return "claude"
	}
	return p
}

// defaultMultiplier 返回倍率值，<= 0 时回退到 1.0（不影响成本）。
func defaultMultiplier(v float64) float64 {
	if v <= 0 {
		return 1.0
	}
	return v
}

// evictConn 强制驱逐一个 *sql.Conn，让 sql.DB 在下次 Get 时重建。
// 通过 conn.Raw 内部回调返回 driver.ErrBadConn，标准库会将此 conn 标记为坏，
// 关闭底层 driver.Conn 并不再放回 pool。
//
// 用途：modernc.org/sqlite 在 tx.Commit() 失败时，连接对应的 SQLite handle
// 仍处于 BEGIN 事务状态。若不驱逐，下次 BeginTx 拿到该污染连接会立即报
// "cannot start a transaction within a transaction (1)"，导致写入彻底瘫痪。
func evictConn(conn *sql.Conn) error {
	if conn == nil {
		return nil
	}
	rawErr := conn.Raw(func(driverConn interface{}) error {
		// 主动 Close 底层连接以释放 SQLite C 资源
		if c, ok := driverConn.(io.Closer); ok {
			if closeErr := c.Close(); closeErr != nil {
				slog.Debug("关闭待驱逐数据库底层连接失败", "error", closeErr)
			}
		}
		// 关键：返回 ErrBadConn 让 sql.DB 标记此 conn 为坏，不再放回 pool
		return driver.ErrBadConn
	})
	// Raw 在我们故意返回 ErrBadConn 时会回传该错误，过滤掉它即可
	if rawErr != nil && !errors.Is(rawErr, driver.ErrBadConn) {
		return rawErr
	}
	return nil
}
