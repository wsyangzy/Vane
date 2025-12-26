// app_api_chart.go - 图表数据 API (Wails Bindings)
// 包含请求趋势、响应时间、端点健康状态、端点成本等图表数据

package main

import (
	"context"
	"time"
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
type EndpointHealthData struct {
	Healthy   int `json:"healthy"`
	Unhealthy int `json:"unhealthy"`
	Unchecked int `json:"unchecked"`
	Skipped   int `json:"skipped"`
	Total     int `json:"total"`
}

// GetEndpointHealthChart 获取端点健康状态图表数据
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

	healthyCount := 0
	unhealthyCount := 0
	uncheckedCount := 0
	skippedCount := 0

	// v6.x: 健康检查范围需要与运行时健康检查一致（避免 UI 统计与实际检查范围不一致）
	// - 启用“渠道间故障转移”时：检查所有“参与渠道间故障转移”的渠道内、且“参与故障转移”的端点
	// - 未启用时：仅检查“当前活跃渠道”内、且“参与故障转移”的端点
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

	for _, endpoint := range endpoints {
		if endpoint == nil {
			continue
		}

		// 组名（渠道 key）：优先用 Channel，缺省回退 Name（与 GetEndpoints 行为保持一致）
		groupKey := endpoint.Config.Channel
		if groupKey == "" {
			groupKey = endpoint.Config.Name
		}

		// 端点级“参与故障转移”开关：默认参与
		failoverEnabled := true
		if endpoint.Config.FailoverEnabled != nil {
			failoverEnabled = *endpoint.Config.FailoverEnabled
		}

		inScope := failoverEnabled
		if autoSwitchEnabled {
			// 渠道级开关：不参与跨渠道故障转移的渠道不在健康检查范围
			if gm != nil && !gm.IsChannelFailoverEnabled(groupKey) {
				inScope = false
			}
		} else {
			// 未启用渠道间故障转移：只对活跃渠道做健康检查
			if gm != nil && !activeGroups[groupKey] {
				inScope = false
			}
		}

		if !inScope {
			uncheckedCount++
			continue
		}

		// v6.1: 配置为跳过健康检查的端点，单独计数为 skipped（可用但不参与主动健康判定）
		if endpoint.Config.SkipHealthCheck {
			skippedCount++
			continue
		}

		status := endpoint.GetStatus()
		if status.NeverChecked {
			uncheckedCount++
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
		Unchecked: uncheckedCount,
		Skipped:   skippedCount,
		Total:     len(endpoints),
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
