package tracking

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// QueryUsageSummary queries usage summary data
func (ut *UsageTracker) QueryUsageSummary(ctx context.Context, opts *QueryOptions) ([]UsageSummary, error) {
	opts = ut.scopedQueryOptions(opts)
	sourceOpts := *opts
	sourceOpts.Offset = 0
	if opts.Limit > 0 {
		sourceOpts.Limit = max(opts.Offset, 0) + opts.Limit
	}

	var summaries []UsageSummary
	err := ut.forEachHistorySource(ctx, opts.StartDate, opts.EndDate, func(source historyQuerySource) error {
		sourceSummaries, err := queryUsageSummaryFromSource(ctx, source.db, &sourceOpts)
		if err != nil {
			return err
		}
		summaries = append(summaries, sourceSummaries...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Date != summaries[j].Date {
			return summaries[i].Date > summaries[j].Date
		}
		if summaries[i].TotalCostUSD != summaries[j].TotalCostUSD {
			return summaries[i].TotalCostUSD > summaries[j].TotalCostUSD
		}
		if summaries[i].ModelName != summaries[j].ModelName {
			return summaries[i].ModelName < summaries[j].ModelName
		}
		if summaries[i].EndpointName != summaries[j].EndpointName {
			return summaries[i].EndpointName < summaries[j].EndpointName
		}
		return summaries[i].GroupName < summaries[j].GroupName
	})

	start := max(opts.Offset, 0)
	if start >= len(summaries) {
		return []UsageSummary{}, nil
	}
	end := len(summaries)
	if opts.Limit > 0 && start+opts.Limit < end {
		end = start + opts.Limit
	}
	return summaries[start:end], nil
}

func queryUsageSummaryFromSource(ctx context.Context, db *sql.DB, opts *QueryOptions) ([]UsageSummary, error) {
	if db == nil {
		return nil, fmt.Errorf("read database not initialized")
	}

	query := `SELECT date, model_name, endpoint_name,
		COALESCE(group_name, '') as group_name,
		request_count, success_count, error_count,
		total_input_tokens, total_output_tokens,
		total_cache_creation_tokens, total_cache_read_tokens,
		total_cost_usd, COALESCE(avg_duration_ms, 0.0) as avg_duration_ms, created_at, updated_at
		FROM usage_summary WHERE 1=1
			AND COALESCE(environment, 'release') = ?`

	args := []interface{}{queryEnvironment(opts)}

	if opts.StartDate != nil {
		query += " AND date >= ?"
		args = append(args, opts.StartDate.Format("2006-01-02"))
	}
	if opts.EndDate != nil {
		query += " AND date <= ?"
		args = append(args, opts.EndDate.Format("2006-01-02"))
	}
	if opts.ModelName != "" {
		query += " AND model_name = ?"
		args = append(args, opts.ModelName)
	}
	if opts.EndpointName != "" {
		query += " AND endpoint_name = ?"
		args = append(args, opts.EndpointName)
	}
	if opts.GroupName != "" {
		query += " AND group_name = ?"
		args = append(args, opts.GroupName)
	}
	if opts.Provider != "" {
		query += " AND provider = ?"
		args = append(args, opts.Provider)
	}

	query += " ORDER BY date DESC, total_cost_usd DESC"

	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}
	if opts.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query usage summary: %w", err)
	}
	defer rows.Close()

	var summaries []UsageSummary
	for rows.Next() {
		var summary UsageSummary
		err := rows.Scan(
			&summary.Date, &summary.ModelName, &summary.EndpointName, &summary.GroupName,
			&summary.RequestCount, &summary.SuccessCount, &summary.ErrorCount,
			&summary.TotalInputTokens, &summary.TotalOutputTokens,
			&summary.TotalCacheCreationTokens, &summary.TotalCacheReadTokens,
			&summary.TotalCostUSD, &summary.AvgDurationMs,
			&summary.CreatedAt, &summary.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan usage summary: %w", err)
		}
		summaries = append(summaries, summary)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating usage summary rows: %w", err)
	}

	return summaries, nil
}
