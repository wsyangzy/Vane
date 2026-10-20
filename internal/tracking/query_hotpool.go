package tracking

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"

	_ "modernc.org/sqlite"
)

// QueryRequestDetailsWithHotPool 双源查询：热池 + 数据库
// 返回合并后的请求列表，热池中的活跃请求排在前面
// 🔧 [修复] 2025-12-11: 正确处理分页，确保返回数量不超过 limit
func (ut *UsageTracker) QueryRequestDetailsWithHotPool(ctx context.Context, opts *QueryOptions) ([]RequestDetail, int64, error) {
	opts = ut.scopedQueryOptions(opts)
	var results []RequestDetail

	// 获取分页参数
	limit := opts.Limit
	offset := opts.Offset
	if limit <= 0 {
		limit = 20 // 默认每页20条
	}

	// 1. 从热池获取所有符合过滤条件的活跃请求（用于计算总数和分页）
	allHotPoolRequests := ut.getFilteredHotPoolRequests(opts)
	hotPoolCount := len(allHotPoolRequests)

	// 2. 获取数据库总数（用于分页计算）
	// 🔧 [P6] 优先尝试内存计数器（仅支持 total / by-provider 维度）：
	//   - 命中：完全避免 DB 查询，杜绝 SQLITE_BUSY 触发的跳变
	//   - 存在月度归档时必须走跨库计数，不能只读活动库计数器
	var dbCount int
	hasHistory, err := ut.hasHistoryArchives()
	if err != nil {
		return nil, 0, err
	}
	if ut.dbCounter != nil && !hasHistory {
		if v, ok := ut.dbCounter.Get(opts); ok {
			dbCount = int(v)
			// 同步更新缓存，方便后续即便回退到 DB 路径时也有最新数值兜底
			ut.setCachedDBCount(opts, dbCount)
		} else {
			cnt, err := ut.CountRequestDetails(ctx, opts)
			if err != nil {
				// 数据库锁定等瞬时错误，使用上次缓存值避免显示跳变
				if shouldLogThrottled("count_request_details_failed") {
					slog.Error("数据库查询失败（已使用上次缓存值降级）", "error", err)
				}
				dbCount = ut.getCachedDBCount(opts)
			} else {
				dbCount = cnt
				ut.setCachedDBCount(opts, dbCount)
			}
		}
	} else if hasHistory {
		cnt, err := ut.CountRequestDetails(ctx, opts)
		if err != nil {
			return nil, 0, err
		}
		dbCount = cnt
		ut.setCachedDBCount(opts, dbCount)
	} else {
		// 计数器未注入（防御性路径），保持原有降级行为
		cnt, err := ut.CountRequestDetails(ctx, opts)
		if err != nil {
			if shouldLogThrottled("count_request_details_failed") {
				slog.Error("数据库查询失败（已使用上次缓存值降级）", "error", err)
			}
			dbCount = ut.getCachedDBCount(opts)
		} else {
			dbCount = cnt
			ut.setCachedDBCount(opts, dbCount)
		}
	}

	// 3. 计算总数：热池 + 数据库
	totalCount := int64(hotPoolCount) + int64(dbCount)

	// 4. 热池数据按开始时间倒序排列（最新的在前）
	if hotPoolCount > 0 {
		sort.Slice(allHotPoolRequests, func(i, j int) bool {
			return allHotPoolRequests[i].StartTime.After(allHotPoolRequests[j].StartTime)
		})
	}

	// 创建热池请求ID集合用于去重
	hotPoolIDs := make(map[string]bool)
	for _, req := range allHotPoolRequests {
		hotPoolIDs[req.RequestID] = true
	}

	// 5. 根据 offset 和 limit 决定从哪里取数据
	if offset < hotPoolCount {
		// 当前页包含热池数据
		hotPoolEnd := offset + limit
		if hotPoolEnd > hotPoolCount {
			hotPoolEnd = hotPoolCount
		}
		// 从热池取数据
		results = append(results, allHotPoolRequests[offset:hotPoolEnd]...)

		// 如果热池数据不够一页，从数据库补充
		remaining := limit - len(results)
		if remaining > 0 {
			// 从数据库查询，offset=0 因为热池数据已经占据了前面的位置
			dbOpts := *opts
			dbOpts.Offset = 0
			dbOpts.Limit = remaining + hotPoolCount // 多取一些用于去重
			dbRequests, err := ut.QueryRequestDetails(ctx, &dbOpts)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to query database requests: %w", err)
			}
			// 添加数据库请求（排除已在热池中的）
			for _, req := range dbRequests {
				if !hotPoolIDs[req.RequestID] && len(results) < limit {
					results = append(results, req)
				}
			}
		}
	} else {
		// 当前页只有数据库数据
		// 调整数据库查询的 offset（减去热池数据的数量）
		dbOpts := *opts
		dbOpts.Offset = offset - hotPoolCount
		dbOpts.Limit = limit + hotPoolCount // 多取一些用于去重

		dbRequests, err := ut.QueryRequestDetails(ctx, &dbOpts)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to query database: %w", err)
		}

		// 添加数据库请求（排除已在热池中的）
		for _, req := range dbRequests {
			if !hotPoolIDs[req.RequestID] && len(results) < limit {
				results = append(results, req)
			}
		}
	}

	return results, totalCount, nil
}

// queryOptsCacheKey 为 QueryOptions 生成稳定的缓存 key
func queryOptsCacheKey(opts *QueryOptions) string {
	if opts == nil {
		return ""
	}
	// 仅使用影响 count 结果的过滤字段（不包含 limit/offset/sort）
	return fmt.Sprintf("p=%s|c=%s|m=%s|e=%s|g=%s|s=%s|env=%s|sd=%v|ed=%v",
		opts.Provider, opts.Channel, opts.ModelName, opts.EndpointName,
		opts.GroupName, opts.Status, opts.Environment, opts.StartDate, opts.EndDate)
}

// getCachedDBCount 获取上次成功的 count（无缓存时返回 0）
func (ut *UsageTracker) getCachedDBCount(opts *QueryOptions) int {
	if ut == nil || ut.dbCountFallbackCache == nil {
		return 0
	}
	count, ok := ut.dbCountFallbackCache.Get(queryOptsCacheKey(opts))
	if !ok {
		return 0
	}
	return count
}

// setCachedDBCount 缓存成功查询的 count
func (ut *UsageTracker) setCachedDBCount(opts *QueryOptions, count int) {
	if ut == nil {
		return
	}
	if ut.dbCountFallbackCache == nil {
		ut.dbCountFallbackCache = appcache.New[int](appcache.Options{
			Name:       "requests:count-fallback",
			MaxEntries: 1000,
			Enabled:    true,
		})
	}
	ut.dbCountFallbackCache.Set(queryOptsCacheKey(opts), count)
}

// getFilteredHotPoolRequests 获取过滤后的热池请求
func (ut *UsageTracker) getFilteredHotPoolRequests(opts *QueryOptions) []RequestDetail {
	if ut.hotPool == nil {
		return nil
	}
	if opts != nil && opts.Environment != "" && opts.Environment != normalizeTrackingEnvironment(ut.environment) {
		return nil
	}

	activeRequests := ut.hotPool.GetActive()
	if len(activeRequests) == 0 {
		return nil
	}

	filtered := make([]RequestDetail, 0, len(activeRequests))
	for _, req := range activeRequests {
		if !activeRequestMatchesFilters(req, opts) {
			continue
		}

		filtered = append(filtered, ut.ActiveRequestToDetail(req))
	}

	return filtered
}

func activeRequestMatchesFilters(req *ActiveRequest, opts *QueryOptions) bool {
	if opts == nil {
		return true
	}

	req.mu.RLock()
	status := req.Status
	modelName := req.ModelName
	channel := req.Channel
	endpointName := req.EndpointName
	groupName := req.GroupName
	startTime := req.StartTime
	provider := req.Provider
	req.mu.RUnlock()

	// 状态过滤
	if opts.Status != "" {
		switch opts.Status {
		case "failed":
			switch status {
			case "failed", "partial_failed", "error", "auth_error", "rate_limited", "server_error", "network_error", "stream_error", "timeout":
				// ok
			default:
				return false
			}
		default:
			if status != opts.Status {
				return false
			}
		}
	}
	// 模型过滤
	if opts.ModelName != "" && modelName != opts.ModelName {
		return false
	}
	// 渠道过滤
	if opts.Channel != "" && channel != opts.Channel {
		return false
	}
	// 端点过滤
	if opts.EndpointName != "" && endpointName != opts.EndpointName {
		return false
	}
	// 组过滤
	if opts.GroupName != "" && groupName != opts.GroupName {
		return false
	}
	// 提供商过滤
	if opts.Provider != "" && provider != opts.Provider {
		return false
	}
	// 时间范围过滤
	if opts.StartDate != nil && startTime.Before(*opts.StartDate) {
		return false
	}
	if opts.EndDate != nil && startTime.After(*opts.EndDate) {
		return false
	}

	return true
}

// GetHotPoolRequestCount 获取热池中符合条件的请求数量
func (ut *UsageTracker) GetHotPoolRequestCount(opts *QueryOptions) int {
	filtered := ut.getFilteredHotPoolRequests(opts)
	return len(filtered)
}
