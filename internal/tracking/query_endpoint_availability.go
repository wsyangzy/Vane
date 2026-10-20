package tracking

import (
	"context"
	"fmt"
	"sort"
)

// EndpointAvailabilityBucket 表示单个时间片内某端点的请求可用性聚合
type EndpointAvailabilityBucket struct {
	Key            string  `json:"key"`               // 分桶键：按小时为 "2006-01-02 15"，按天为 "2006-01-02"
	Total          int     `json:"total"`             // 有结论的请求数（success + failed）
	Success        int     `json:"success"`           // 成功完成数
	Failed         int     `json:"failed"`            // 失败数
	AvgFirstByteMs float64 `json:"avg_first_byte_ms"` // 平均首字节耗时
}

// 首字节耗时：优先用流式响应真正开始下发的时刻，非流式或未下发时回退为总耗时。
// start_time / output_sent_at 均以 "2006-01-02 15:04:05" 写入，julianday 可直接解析。
const endpointFirstByteMsSQLExpression = `COALESCE(
	CASE WHEN output_sent_at IS NOT NULL AND output_sent_at >= start_time
		THEN (julianday(output_sent_at) - julianday(start_time)) * 86400000.0
	END,
	duration_ms,
	0)`

// QueryEndpointAvailabilityBuckets 按时间片聚合端点的请求成功率与响应速度。
//
// 数据取自 request_logs 而非健康探测结果：探测通但实际转发失败的端点并不少见，
// 真实请求的成功率更能反映端点可用性。代价是没有流量的时间片没有记录，
// 由调用方补齐为空档 —— 不能省略，否则时间轴会被压缩而失真。
//
// 仅统计已有结论的请求：cancelled 是客户端主动放弃、进行中的请求尚无结果，
// 都不该计入可用率的分母。
func (ut *UsageTracker) QueryEndpointAvailabilityBuckets(ctx context.Context, opts *QueryOptions, bucket string) ([]EndpointAvailabilityBucket, error) {
	opts = ut.scopedQueryOptions(opts)
	if opts == nil || opts.StartDate == nil || opts.EndDate == nil {
		return []EndpointAvailabilityBucket{}, nil
	}
	bucketExpression := "substr(start_time, 1, 10)"
	if bucket == "hour" {
		bucketExpression = "substr(start_time, 1, 13)"
	} else if bucket != "day" {
		return nil, fmt.Errorf("unsupported endpoint availability bucket %q", bucket)
	}

	query := `SELECT ` + bucketExpression + ` AS bucket,
		SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END),
		SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),
		COALESCE(SUM(` + endpointFirstByteMsSQLExpression + `), 0)
		FROM request_logs WHERE 1=1`
	query, args := ut.appendRequestQueryFilters(query, nil, opts)
	query += " AND status IN ('completed', 'failed') GROUP BY bucket ORDER BY bucket ASC"

	type availabilityAcc struct {
		success      int
		failed       int
		latencySumMs float64
	}
	byKey := make(map[string]*availabilityAcc)

	err := ut.forEachHistorySource(ctx, opts.StartDate, opts.EndDate, func(source historyQuerySource) error {
		rows, err := source.db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			var success, failed int
			var latencySumMs float64
			if err := rows.Scan(&key, &success, &failed, &latencySumMs); err != nil {
				return err
			}
			acc := byKey[key]
			if acc == nil {
				acc = &availabilityAcc{}
				byKey[key] = acc
			}
			acc.success += success
			acc.failed += failed
			acc.latencySumMs += latencySumMs
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}

	items := make([]EndpointAvailabilityBucket, 0, len(byKey))
	for key, acc := range byKey {
		total := acc.success + acc.failed
		item := EndpointAvailabilityBucket{
			Key:     key,
			Total:   total,
			Success: acc.success,
			Failed:  acc.failed,
		}
		if total > 0 {
			item.AvgFirstByteMs = acc.latencySumMs / float64(total)
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, nil
}
