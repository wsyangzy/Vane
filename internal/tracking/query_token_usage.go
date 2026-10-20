package tracking

import (
	"context"
	"fmt"
	"sort"
	"time"
)

func (ut *UsageTracker) QueryTokenUsageTotals(ctx context.Context, startTime, endTime time.Time, provider string) (TokenUsageTotals, error) {
	query := `SELECT
		COALESCE(SUM(` + distributionInputTokensSQLExpression + `), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(cache_creation_tokens), 0),
		COALESCE(SUM(cache_read_tokens), 0),
		COALESCE(SUM(` + totalTokensSQLExpression + `), 0)
		FROM request_logs
		WHERE start_time >= ? AND start_time < ?
		  AND COALESCE(environment, 'release') = ?`
	args := []any{ut.formatStartTimeQueryBound(startTime), ut.formatStartTimeQueryBound(endTime), normalizeTrackingEnvironment(ut.environment)}
	if provider != "" {
		query += " AND provider = ?"
		args = append(args, provider)
	}

	var totals TokenUsageTotals
	err := ut.forEachHistorySource(ctx, &startTime, &endTime, func(source historyQuerySource) error {
		var sourceTotals TokenUsageTotals
		if err := source.db.QueryRowContext(ctx, query, args...).Scan(
			&sourceTotals.InputTokens,
			&sourceTotals.OutputTokens,
			&sourceTotals.CacheCreationTokens,
			&sourceTotals.CacheReadTokens,
			&sourceTotals.TotalTokens,
		); err != nil {
			return err
		}
		totals.InputTokens += sourceTotals.InputTokens
		totals.OutputTokens += sourceTotals.OutputTokens
		totals.CacheCreationTokens += sourceTotals.CacheCreationTokens
		totals.CacheReadTokens += sourceTotals.CacheReadTokens
		totals.TotalTokens += sourceTotals.TotalTokens
		return nil
	})
	return totals, err
}

func (ut *UsageTracker) QueryTokenUsageBuckets(ctx context.Context, opts *QueryOptions, bucket string) ([]TokenUsageBucket, error) {
	opts = ut.scopedQueryOptions(opts)
	if opts == nil || opts.StartDate == nil || opts.EndDate == nil {
		return []TokenUsageBucket{}, nil
	}
	bucketExpression := "substr(start_time, 1, 10)"
	if bucket == "hour" {
		bucketExpression = "substr(start_time, 1, 13)"
	} else if bucket != "day" {
		return nil, fmt.Errorf("unsupported token usage bucket %q", bucket)
	}

	query := `SELECT ` + bucketExpression + ` AS bucket,
		COUNT(*),
		COALESCE(SUM(` + distributionInputTokensSQLExpression + `), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(cache_creation_tokens), 0),
		COALESCE(SUM(cache_read_tokens), 0),
		COALESCE(SUM(` + totalTokensSQLExpression + `), 0)
		FROM request_logs WHERE 1=1`
	query, args := ut.appendRequestQueryFilters(query, nil, opts)
	query += " GROUP BY bucket ORDER BY bucket ASC"

	byKey := make(map[string]TokenUsageBucket)
	err := ut.forEachHistorySource(ctx, opts.StartDate, opts.EndDate, func(source historyQuerySource) error {
		rows, err := source.db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item TokenUsageBucket
			if err := rows.Scan(
				&item.Key,
				&item.RequestCount,
				&item.InputTokens,
				&item.OutputTokens,
				&item.CacheCreationTokens,
				&item.CacheReadTokens,
				&item.TotalTokens,
			); err != nil {
				return err
			}
			current := byKey[item.Key]
			current.Key = item.Key
			current.RequestCount += item.RequestCount
			current.InputTokens += item.InputTokens
			current.OutputTokens += item.OutputTokens
			current.CacheCreationTokens += item.CacheCreationTokens
			current.CacheReadTokens += item.CacheReadTokens
			current.TotalTokens += item.TotalTokens
			byKey[item.Key] = current
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	items := make([]TokenUsageBucket, 0, len(byKey))
	for _, item := range byKey {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, nil
}
