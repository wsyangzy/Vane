package tracking

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// GetUsageSummary 获取使用摘要（便利方法）
func (ut *UsageTracker) GetUsageSummary(ctx context.Context, startTime, endTime time.Time) ([]UsageSummary, error) {
	opts := &QueryOptions{
		StartDate: &startTime,
		EndDate:   &endTime,
	}
	return ut.QueryUsageSummary(ctx, opts)
}

// GetRequestLogs 获取请求日志（便利方法）
func (ut *UsageTracker) GetRequestLogs(ctx context.Context, startTime, endTime time.Time, modelName, endpointName, groupName string, limit, offset int) ([]RequestDetail, error) {
	opts := &QueryOptions{
		StartDate:    &startTime,
		EndDate:      &endTime,
		ModelName:    modelName,
		EndpointName: endpointName,
		GroupName:    groupName,
		Limit:        limit,
		Offset:       offset,
	}
	return ut.QueryRequestDetails(ctx, opts)
}

// GetUsageStats 获取使用统计（便利方法，使用读连接）
func (ut *UsageTracker) GetUsageStats(ctx context.Context, startTime, endTime time.Time) (*UsageStatsDetailed, error) {
	totals, err := ut.QueryUsageStatsTotals(ctx, &QueryOptions{StartDate: &startTime, EndDate: &endTime})
	if err != nil {
		return nil, fmt.Errorf("failed to query detailed usage stats: %w", err)
	}
	stats := &UsageStatsDetailed{
		TotalRequests:   totals.TotalRequests,
		SuccessRequests: totals.SuccessRequests,
		ErrorRequests:   totals.FailedRequests,
		TotalTokens:     totals.TotalTokens,
		TotalCost:       totals.TotalCostUSD,
		ModelStats:      make(map[string]ModelStat),
		EndpointStats:   make(map[string]EndpointStat),
		GroupStats:      make(map[string]GroupStat),
	}
	startBound := ut.formatStartTimeQueryBound(startTime)
	endBound := ut.formatEndTimeQueryBound(endTime)
	err = ut.forEachHistorySource(ctx, &startTime, &endTime, func(source historyQuerySource) error {
		queries := []struct {
			column string
			apply  func(string, int64, float64)
		}{
			{column: "model_name", apply: func(name string, requests int64, cost float64) {
				current := stats.ModelStats[name]
				current.RequestCount += requests
				current.TotalCost += cost
				stats.ModelStats[name] = current
			}},
			{column: "endpoint_name", apply: func(name string, requests int64, cost float64) {
				current := stats.EndpointStats[name]
				current.RequestCount += requests
				current.TotalCost += cost
				stats.EndpointStats[name] = current
			}},
			{column: "group_name", apply: func(name string, requests int64, cost float64) {
				current := stats.GroupStats[name]
				current.RequestCount += requests
				current.TotalCost += cost
				stats.GroupStats[name] = current
			}},
		}
		for _, groupedQuery := range queries {
			query := fmt.Sprintf(`SELECT %s, COUNT(*), COALESCE(SUM(total_cost_usd), 0)
				FROM request_logs
				WHERE start_time >= ? AND start_time <= ?
				  AND COALESCE(environment, 'release') = ?
				  AND %s IS NOT NULL AND %s != ''
				GROUP BY %s`, groupedQuery.column, groupedQuery.column, groupedQuery.column, groupedQuery.column)
			rows, err := source.db.QueryContext(ctx, query, startBound, endBound, ut.environment)
			if err != nil {
				return err
			}
			for rows.Next() {
				var name string
				var requests int64
				var cost float64
				if err := rows.Scan(&name, &requests, &cost); err != nil {
					_ = rows.Close()
					return err
				}
				groupedQuery.apply(name, requests, cost)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return err
			}
			if err := rows.Close(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query detailed usage dimensions: %w", err)
	}
	return stats, nil
}

// ExportToCSV 导出为CSV格式
func (ut *UsageTracker) ExportToCSV(ctx context.Context, startTime, endTime time.Time, modelName, endpointName, groupName string) ([]byte, error) {
	logs, err := ut.GetRequestLogs(ctx, startTime, endTime, modelName, endpointName, groupName, 10000, 0) // Export up to 10k records
	if err != nil {
		return nil, fmt.Errorf("failed to get request logs for CSV export: %w", err)
	}

	// CSV header
	csv := "request_id,client_ip,user_agent,method,path,start_time,end_time,duration_ms,channel,endpoint_name,group_name,model_name,status,http_status_code,retry_count,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,input_cost_usd,output_cost_usd,cache_creation_cost_usd,cache_read_cost_usd,total_cost_usd,created_at,updated_at\n"

	// CSV rows
	for _, log := range logs {
		endTime := ""
		if log.EndTime != nil {
			endTime = log.EndTime.Format(time.RFC3339)
		}

		durationMs := ""
		if log.DurationMs != nil {
			durationMs = fmt.Sprintf("%d", *log.DurationMs)
		}

		httpStatus := ""
		if log.HTTPStatusCode != nil {
			httpStatus = fmt.Sprintf("%d", *log.HTTPStatusCode)
		}

		csv += fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%d,%d,%d,%d,%d,%.6f,%.6f,%.6f,%.6f,%.6f,%s,%s\n",
			log.RequestID, log.ClientIP, log.UserAgent, log.Method, log.Path,
			log.StartTime.Format(time.RFC3339), endTime, durationMs,
			log.Channel, log.EndpointName, log.GroupName, log.ModelName, log.Status,
			httpStatus, log.RetryCount,
			log.InputTokens, log.OutputTokens, log.CacheCreationTokens, log.CacheReadTokens,
			log.InputCostUSD, log.OutputCostUSD, log.CacheCreationCostUSD, log.CacheReadCostUSD, log.TotalCostUSD,
			log.CreatedAt.Format(time.RFC3339), log.UpdatedAt.Format(time.RFC3339),
		)
	}

	return []byte(csv), nil
}

// ExportToJSON 导出为JSON格式
func (ut *UsageTracker) ExportToJSON(ctx context.Context, startTime, endTime time.Time, modelName, endpointName, groupName string) ([]byte, error) {
	logs, err := ut.GetRequestLogs(ctx, startTime, endTime, modelName, endpointName, groupName, 10000, 0) // Export up to 10k records
	if err != nil {
		return nil, fmt.Errorf("failed to get request logs for JSON export: %w", err)
	}

	// 使用标准库的json包序列化
	jsonBytes, err := json.Marshal(logs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal logs to JSON: %w", err)
	}

	return jsonBytes, nil
}
