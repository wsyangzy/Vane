package tracking

import (
	"context"
	"database/sql"
	"time"
)

// DashboardUsageRecord is the small metadata projection used by usage analytics.
// It excludes headers, request/response bodies, error details and routing snapshots.
type DashboardUsageRecord struct {
	ID, Provider, Model, Channel, Group, Status string
	At                                          time.Time
	Input, Output, CacheRead, CacheWrite        int64
	Cost                                        float64
	DurationMs                                  int64
}

func (ut *UsageTracker) QueryDashboardUsage(ctx context.Context, opts *QueryOptions) ([]DashboardUsageRecord, error) {
	opts = ut.scopedQueryOptions(opts)
	result := make([]DashboardUsageRecord, 0)
	seen := make(map[string]bool)
	err := ut.forEachHistorySource(ctx, opts.StartDate, opts.EndDate, func(source historyQuerySource) error {
		query := `SELECT request_id, start_time, COALESCE(provider, 'claude'), COALESCE(model_name, ''),
			COALESCE(channel, ''), COALESCE(group_name, ''), status, ` + DistributionInputTokensSQLExpression() + `,
			COALESCE(output_tokens, 0), COALESCE(cache_read_tokens, 0), COALESCE(cache_creation_tokens, 0),
			COALESCE(total_cost_usd, 0), COALESCE(duration_ms, 0)
			FROM request_logs WHERE COALESCE(environment, 'release') = ?`
		args := []any{queryEnvironment(opts)}
		if opts.StartDate != nil {
			query += " AND start_time >= ?"
			args = append(args, ut.formatStartTimeQueryBound(*opts.StartDate))
		}
		if opts.EndDate != nil {
			query += " AND start_time <= ?"
			args = append(args, ut.formatEndTimeQueryBound(*opts.EndDate))
		}
		if opts.Provider != "" {
			query += " AND COALESCE(NULLIF(provider, ''), 'claude') = ?"
			args = append(args, opts.Provider)
		}
		rows, err := source.db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item DashboardUsageRecord
			var stamp sql.NullTime
			if err := rows.Scan(&item.ID, &stamp, &item.Provider, &item.Model, &item.Channel, &item.Group, &item.Status, &item.Input, &item.Output, &item.CacheRead, &item.CacheWrite, &item.Cost, &item.DurationMs); err != nil {
				return err
			}
			if seen[item.ID] || !stamp.Valid {
				continue
			}
			seen[item.ID] = true
			item.At = stamp.Time
			if item.Provider == "" {
				item.Provider = "claude"
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	return result, err
}
