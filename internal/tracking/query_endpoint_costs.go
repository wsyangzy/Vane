package tracking

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// GetEndpointCostsForDate queries endpoint cost summary data for a specific date
func (ut *UsageTracker) GetEndpointCostsForDate(ctx context.Context, date string) ([]EndpointCostSummary, error) {
	if ut.readDB == nil {
		return nil, fmt.Errorf("read database not initialized")
	}

	// Validate date format
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, fmt.Errorf("invalid date format, expected YYYY-MM-DD: %w", err)
	}

	slog.Debug("Querying endpoint costs for date", "date", date)

	// Convert date to time range for timezone-aware querying
	// Parse the date and create start/end times in local timezone
	startTime, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, fmt.Errorf("failed to parse date: %w", err)
	}

	// Use configured timezone for the date range
	location := time.Local
	if ut != nil && ut.location != nil {
		location = ut.location
	}
	startOfDay := time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 0, 0, 0, 0, location)
	endOfDay := time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 23, 59, 59, 999999999, location)

	// %s 为成功判定表达式：月度归档可能缺 stream_outcome 列，按数据源分别选口径
	queryTemplate := `SELECT
		COALESCE(channel, '') as channel,
		COALESCE(endpoint_name, '') as endpoint_name,
		COALESCE(group_name, '') as group_name,
		COALESCE(endpoint_chain, '') as endpoint_chain,
		COALESCE(provider, 'claude') as provider,
		CASE WHEN %s THEN 1 ELSE 0 END as success,
		COALESCE(` + totalTokensSQLExpression + `, 0) as total_tokens,
		COALESCE(total_cost_usd, 0.0) as total_cost_usd,
		COALESCE(input_tokens, 0) as input_tokens,
		COALESCE(output_tokens, 0) as output_tokens,
		COALESCE(cache_creation_tokens, 0) as cache_creation_tokens,
		COALESCE(cache_read_tokens, 0) as cache_read_tokens,
		COALESCE(input_cost_usd, 0.0) as input_cost_usd,
		COALESCE(output_cost_usd, 0.0) as output_cost_usd,
		COALESCE(cache_creation_cost_usd, 0.0) as cache_creation_cost_usd,
		COALESCE(cache_read_cost_usd, 0.0) as cache_read_cost_usd
		FROM request_logs
		WHERE start_time >= ? AND start_time <= ?
		  AND COALESCE(environment, 'release') = ?`

	type endpointCostKey struct {
		Channel      string
		EndpointName string
		GroupName    string
		Provider     string
	}
	type endpointCostRow struct {
		Channel              string
		EndpointName         string
		GroupName            string
		EndpointChain        string
		Provider             string
		Success              bool
		TotalTokens          int64
		TotalCostUSD         float64
		InputTokens          int64
		OutputTokens         int64
		CacheCreationTokens  int64
		CacheReadTokens      int64
		InputCostUSD         float64
		OutputCostUSD        float64
		CacheCreationCostUSD float64
		CacheReadCostUSD     float64
	}

	grouped := make(map[endpointCostKey]*EndpointCostSummary)
	err = ut.forEachHistorySource(ctx, &startOfDay, &endOfDay, func(source historyQuerySource) error {
		hasOutcome, err := requestLogsHasColumn(ctx, source.db, "stream_outcome")
		if err != nil {
			return err
		}
		successExpr := `COALESCE(status, '') = 'completed'`
		if hasOutcome {
			successExpr = successRequestSQLCondition
		}
		rows, err := source.db.QueryContext(ctx, fmt.Sprintf(queryTemplate, successExpr),
			ut.formatStartTimeQueryBound(startOfDay), ut.formatEndTimeQueryBound(endOfDay), normalizeTrackingEnvironment(ut.environment))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row endpointCostRow
			if err := rows.Scan(
				&row.Channel, &row.EndpointName, &row.GroupName, &row.EndpointChain, &row.Provider, &row.Success,
				&row.TotalTokens, &row.TotalCostUSD,
				&row.InputTokens, &row.OutputTokens,
				&row.CacheCreationTokens, &row.CacheReadTokens,
				&row.InputCostUSD, &row.OutputCostUSD,
				&row.CacheCreationCostUSD, &row.CacheReadCostUSD,
			); err != nil {
				return fmt.Errorf("failed to scan endpoint cost: %w", err)
			}
			groupName := row.GroupName
			if groupName == "" {
				groupName = endpointChainCredentialGroup(row.EndpointChain)
			}
			key := endpointCostKey{
				Channel:      row.Channel,
				EndpointName: row.EndpointName,
				GroupName:    groupName,
				Provider:     row.Provider,
			}
			cost := grouped[key]
			if cost == nil {
				cost = &EndpointCostSummary{
					Channel:      row.Channel,
					EndpointName: row.EndpointName,
					GroupName:    groupName,
					Provider:     row.Provider,
				}
				grouped[key] = cost
			}
			cost.TotalTokens += row.TotalTokens
			cost.TotalCostUSD += row.TotalCostUSD
			cost.RequestCount++
			if row.Success {
				cost.SuccessCount++
			}
			numerator, denominator := cacheHitParts(row.Provider, row.InputTokens, row.CacheCreationTokens, row.CacheReadTokens)
			cost.cacheHitNumerator += numerator
			cost.cacheHitDenominator += denominator
			cost.InputTokens += row.InputTokens
			cost.OutputTokens += row.OutputTokens
			cost.CacheCreationTokens += row.CacheCreationTokens
			cost.CacheReadTokens += row.CacheReadTokens
			cost.InputCostUSD += row.InputCostUSD
			cost.OutputCostUSD += row.OutputCostUSD
			cost.CacheCreationCostUSD += row.CacheCreationCostUSD
			cost.CacheReadCostUSD += row.CacheReadCostUSD
		}
		return rows.Err()
	})
	if err != nil {
		slog.Error("Failed to query endpoint costs", "error", err, "date", date, "start_time", startOfDay, "end_time", endOfDay)
		return nil, fmt.Errorf("failed to query endpoint costs for date %s: %w", date, err)
	}

	costs := make([]EndpointCostSummary, 0, len(grouped))
	for _, cost := range grouped {
		cost.CacheHitRate = cacheHitRate(cost.cacheHitNumerator, cost.cacheHitDenominator)
		costs = append(costs, *cost)
	}
	sort.Slice(costs, func(i, j int) bool {
		if costs[i].TotalCostUSD != costs[j].TotalCostUSD {
			return costs[i].TotalCostUSD > costs[j].TotalCostUSD
		}
		if costs[i].Channel != costs[j].Channel {
			return costs[i].Channel < costs[j].Channel
		}
		if costs[i].EndpointName != costs[j].EndpointName {
			return costs[i].EndpointName < costs[j].EndpointName
		}
		return costs[i].GroupName < costs[j].GroupName
	})

	slog.Debug("Successfully queried endpoint costs",
		"date", date,
		"endpoint_count", len(costs),
		"total_endpoints", len(costs))

	return costs, nil
}
