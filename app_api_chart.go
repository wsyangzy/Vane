// app_api_chart.go - 图表数据 API (Wails Bindings)
// 包含请求趋势、响应时间、端点健康状态、端点成本等图表数据

package main

import (
	"context"
	"time"

	"claude-request-forwarder/internal/endpoint"
)

// ============================================================
// 图表数据 API
// ============================================================

// ChartDataPoint 图表数据点
type ChartDataPoint struct {
	Time      string  `json:"time"`
	Total     int64   `json:"total"`
	Success   int64   `json:"success"`
	Fail      int64   `json:"fail"`
	Avg       float64 `json:"avg"`
	Min       float64 `json:"min"`
	Max       float64 `json:"max"`
	Value     int64   `json:"value"`
	Timestamp string  `json:"timestamp"`
}

// GetRequestTrendChart 获取请求趋势图表数据
func (a *App) GetRequestTrendChart(minutes int) []ChartDataPoint {
	a.mu.RLock()
	monitoringMiddleware := a.monitoringMiddleware
	logger := a.logger
	a.mu.RUnlock()

	if monitoringMiddleware == nil {
		if logger != nil {
			logger.Warn("GetRequestTrendChart: monitoringMiddleware is nil")
		}
		return []ChartDataPoint{}
	}

	metrics := monitoringMiddleware.GetMetrics()
	if metrics == nil {
		if logger != nil {
			logger.Warn("GetRequestTrendChart: metrics is nil")
		}
		return []ChartDataPoint{}
	}

	// 直接在原始 *Metrics 上调用，而不是获取副本
	// RequestHistory 存的是累计计数快照，这里转为相邻点增量，避免无请求时图表保持水平线
	requestHistory := metrics.GetChartDataForRequestHistoryWithBaseline(minutes)

	if logger != nil {
		logger.Info("📊 GetRequestTrendChart",
			"minutes", minutes,
			"history_points", len(requestHistory))
	}

	if len(requestHistory) == 0 {
		return []ChartDataPoint{}
	}

	// 没有 baseline 时，返回累计值（保持兼容，避免首点出现“大跳变”）
	if len(requestHistory) == 1 {
		p := requestHistory[0]
		return []ChartDataPoint{{
			Time:      p.Timestamp.Format("15:04"),
			Total:     p.Total,
			Success:   p.Successful,
			Fail:      p.Failed,
			Timestamp: p.Timestamp.Format(time.RFC3339Nano),
		}}
	}

	result := make([]ChartDataPoint, 0, len(requestHistory)-1)
	prev := requestHistory[0]
	for i := 1; i < len(requestHistory); i++ {
		curr := requestHistory[i]

		totalDelta := curr.Total - prev.Total
		successDelta := curr.Successful - prev.Successful
		failDelta := curr.Failed - prev.Failed
		if totalDelta < 0 {
			totalDelta = 0
		}
		if successDelta < 0 {
			successDelta = 0
		}
		if failDelta < 0 {
			failDelta = 0
		}

		result = append(result, ChartDataPoint{
			Time:      curr.Timestamp.Format("15:04"),
			Total:     totalDelta,
			Success:   successDelta,
			Fail:      failDelta,
			Timestamp: curr.Timestamp.Format(time.RFC3339Nano),
		})

		prev = curr
	}

	return result
}

// GetResponseTimeChart 获取响应时间图表数据
func (a *App) GetResponseTimeChart(minutes int) []ChartDataPoint {
	a.mu.RLock()
	monitoringMiddleware := a.monitoringMiddleware
	a.mu.RUnlock()

	if monitoringMiddleware == nil {
		return []ChartDataPoint{}
	}

	metrics := monitoringMiddleware.GetMetrics()
	responseHistory := metrics.GetChartDataForResponseTime(minutes)

	result := make([]ChartDataPoint, len(responseHistory))
	for i, point := range responseHistory {
		result[i] = ChartDataPoint{
			Time:      point.Timestamp.Format("15:04"),
			Avg:       float64(point.AverageTime.Milliseconds()),
			Min:       float64(point.MinTime.Milliseconds()),
			Max:       float64(point.MaxTime.Milliseconds()),
			Timestamp: point.Timestamp.Format(time.RFC3339Nano),
		}
	}

	return result
}

// GetConnectionActivityChart 获取连接活动图表数据
func (a *App) GetConnectionActivityChart(minutes int) []ChartDataPoint {
	a.mu.RLock()
	monitoringMiddleware := a.monitoringMiddleware
	a.mu.RUnlock()

	if monitoringMiddleware == nil {
		return []ChartDataPoint{}
	}

	// 连接活动图表使用请求历史数据
	metrics := monitoringMiddleware.GetMetrics()
	// RequestHistory 存的是累计计数快照，这里转为相邻点增量，作为“活动量”展示
	requestHistory := metrics.GetChartDataForRequestHistoryWithBaseline(minutes)

	if len(requestHistory) == 0 {
		return []ChartDataPoint{}
	}

	if len(requestHistory) == 1 {
		p := requestHistory[0]
		return []ChartDataPoint{{
			Time:      p.Timestamp.Format("15:04"),
			Value:     p.Total,
			Timestamp: p.Timestamp.Format(time.RFC3339Nano),
		}}
	}

	result := make([]ChartDataPoint, 0, len(requestHistory)-1)
	prev := requestHistory[0]
	for i := 1; i < len(requestHistory); i++ {
		curr := requestHistory[i]
		delta := curr.Total - prev.Total
		if delta < 0 {
			delta = 0
		}
		result = append(result, ChartDataPoint{
			Time:      curr.Timestamp.Format("15:04"),
			Value:     delta,
			Timestamp: curr.Timestamp.Format(time.RFC3339Nano),
		})
		prev = curr
	}

	return result
}

// ============================================================
// 端点健康状态图表 API
// ============================================================

// EndpointHealthData 端点健康状态数据结构
// 简化分类
//   - healthy + unhealthy + excluded = total（当前检测范围内）
//   - skipped 是 healthy 的子集，仅用于显示
type EndpointHealthData struct {
	Healthy   int `json:"healthy"`   // 健康（含跳过检测）
	Unhealthy int `json:"unhealthy"` // 异常
	Skipped   int `json:"skipped"`   // 跳过检测的数量（healthy 的子集）
	Excluded  int `json:"excluded"`  // 不参与检测（不参与故障转移 或 尚未检测）
	Total     int `json:"total"`     // 当前检测范围内的端点总数
}

// GetEndpointHealthChart 获取端点健康状态图表数据
// 统计当前检测范围内的端点
//   - 启用渠道间转移：统计所有参与的渠道
//   - 未启用：仅统计活跃渠道
func (a *App) GetEndpointHealthChart() EndpointHealthData {
	a.mu.RLock()
	endpointManager := a.endpointManager
	a.mu.RUnlock()

	if endpointManager == nil {
		return EndpointHealthData{}
	}

	endpoints := endpointManager.GetAllEndpoints()
	cfg := endpointManager.GetConfig()
	gm := endpointManager.GetGroupManager()

	// 确定检测范围
	autoSwitchEnabled := cfg != nil && cfg.Failover.Enabled
	activeGroups := map[string]bool{}
	if !autoSwitchEnabled && gm != nil {
		for _, g := range gm.GetActiveGroups() {
			if g == nil || g.Name == "" {
				continue
			}
			activeGroups[g.Name] = true
		}
	}

	// 筛选当前检测范围内的端点
	var inScopeEndpoints []*endpoint.Endpoint
	for _, ep := range endpoints {
		if ep == nil {
			continue
		}

		groupKey := ep.Config.Channel
		if groupKey == "" {
			groupKey = ep.Config.Name
		}

		inChannel := false
		if autoSwitchEnabled {
			// 渠道间转移模式：所有参与的渠道
			if gm != nil && gm.IsChannelFailoverEnabled(groupKey) {
				inChannel = true
			}
		} else {
			// 单渠道模式：仅活跃渠道
			if activeGroups[groupKey] {
				inChannel = true
			}
		}

		if inChannel {
			inScopeEndpoints = append(inScopeEndpoints, ep)
		}
	}

	// 统计健康状态
	healthyCount := 0
	unhealthyCount := 0
	skippedCount := 0
	excludedCount := 0

	for _, ep := range inScopeEndpoints {
		// 端点级：不参与故障转移 → 不参与检测
		failoverEnabled := true
		if ep.Config.FailoverEnabled != nil {
			failoverEnabled = *ep.Config.FailoverEnabled
		}

		if !failoverEnabled {
			excludedCount++
			continue
		}

		// 跳过检测 → 视为健康
		if ep.Config.SkipHealthCheck {
			healthyCount++
			skippedCount++ // 记录跳过数量（healthy 子集）
			continue
		}

		status := ep.GetStatus()
		if status.NeverChecked {
			// 尚未检测 → 不参与当前统计
			excludedCount++
			continue
		}

		if status.Healthy {
			healthyCount++
		} else {
			unhealthyCount++
		}
	}

	return EndpointHealthData{
		Healthy:   healthyCount,
		Unhealthy: unhealthyCount,
		Skipped:   skippedCount,
		Excluded:  excludedCount,
		Total:     len(inScopeEndpoints), // 当前范围内的端点总数
	}
}

// ============================================================
// 端点成本图表 API
// ============================================================

// EndpointCostItem 端点成本数据项（用于前端图表）
type EndpointCostItem struct {
	Name   string  `json:"name"`
	Tokens int64   `json:"tokens"`
	Cost   float64 `json:"cost"`
}

// GetEndpointCosts 获取当日端点成本数据
func (a *App) GetEndpointCosts() []EndpointCostItem {
	a.mu.RLock()
	usageTracker := a.usageTracker
	logger := a.logger
	a.mu.RUnlock()

	if usageTracker == nil {
		return []EndpointCostItem{}
	}

	// 获取当日日期
	date := time.Now().Format("2006-01-02")

	// 查询端点成本数据
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	costs, err := usageTracker.GetEndpointCostsForDate(ctx, date)
	if err != nil {
		if logger != nil {
			logger.Error("获取端点成本数据失败", "error", err)
		}
		return []EndpointCostItem{}
	}

	// 转换为前端期望的格式
	result := make([]EndpointCostItem, len(costs))
	for i, cost := range costs {
		// 创建标签，格式：端点名称 (组名) 或 端点名称
		name := cost.EndpointName
		if cost.GroupName != "" {
			name = cost.EndpointName + " (" + cost.GroupName + ")"
		}

		// 计算总 Token
		totalTokens := cost.InputTokens + cost.OutputTokens + cost.CacheCreationTokens + cost.CacheReadTokens

		result[i] = EndpointCostItem{
			Name:   name,
			Tokens: totalTokens,
			Cost:   cost.TotalCostUSD,
		}
	}

	return result
}
