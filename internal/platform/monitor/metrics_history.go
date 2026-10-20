package monitor

import (
	"time"
)

// AddHistoryDataPoints 定期收集历史数据点
func (m *Metrics) AddHistoryDataPoints() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// 添加请求历史数据点
	requestPoint := RequestDataPoint{
		Timestamp:  now,
		Total:      m.TotalRequests,
		Successful: m.SuccessfulRequests,
		Failed:     m.FailedRequests,
	}
	m.RequestHistory = append(m.RequestHistory, requestPoint)

	// 添加响应时间历史数据点
	responsePoint := ResponseTimePoint{
		Timestamp:   now,
		AverageTime: m.GetAverageResponseTimeUnlocked(),
		MinTime:     m.MinResponseTime,
		MaxTime:     m.MaxResponseTime,
	}
	m.ResponseHistory = append(m.ResponseHistory, responsePoint)

	// 添加Token使用历史数据点 (定期快照，而不是在每次Token使用时添加)
	tokenPoint := TokenHistoryPoint{
		Timestamp:           now,
		InputTokens:         m.TotalTokenUsage.InputTokens,
		OutputTokens:        m.TotalTokenUsage.OutputTokens,
		CacheCreationTokens: m.TotalTokenUsage.CacheCreationTokens,
		CacheReadTokens:     m.TotalTokenUsage.CacheReadTokens,
		TotalTokens:         m.TotalTokenUsage.InputTokens + m.TotalTokenUsage.OutputTokens + m.TotalTokenUsage.CacheCreationTokens + m.TotalTokenUsage.CacheReadTokens,
	}

	// 只有当Token数据有变化时才添加新点，避免重复数据
	if len(m.TokenHistory) == 0 ||
		m.TokenHistory[len(m.TokenHistory)-1].InputTokens != tokenPoint.InputTokens ||
		m.TokenHistory[len(m.TokenHistory)-1].OutputTokens != tokenPoint.OutputTokens ||
		m.TokenHistory[len(m.TokenHistory)-1].CacheCreationTokens != tokenPoint.CacheCreationTokens ||
		m.TokenHistory[len(m.TokenHistory)-1].CacheReadTokens != tokenPoint.CacheReadTokens {
		m.TokenHistory = append(m.TokenHistory, tokenPoint)
	}

	// 添加挂起请求历史数据点
	suspendedPoint := SuspendedRequestHistoryPoint{
		Timestamp:                   now,
		SuspendedRequests:           m.SuspendedRequests,
		TotalSuspendedRequests:      m.TotalSuspendedRequests,
		SuccessfulSuspendedRequests: m.SuccessfulSuspendedRequests,
		TimeoutSuspendedRequests:    m.TimeoutSuspendedRequests,
		AverageSuspendedTime:        m.GetAverageSuspendedTimeUnlocked(),
	}

	// 只有当挂起请求数据有变化时才添加新点
	if len(m.SuspendedRequestHistory) == 0 ||
		m.SuspendedRequestHistory[len(m.SuspendedRequestHistory)-1].SuspendedRequests != suspendedPoint.SuspendedRequests ||
		m.SuspendedRequestHistory[len(m.SuspendedRequestHistory)-1].TotalSuspendedRequests != suspendedPoint.TotalSuspendedRequests ||
		m.SuspendedRequestHistory[len(m.SuspendedRequestHistory)-1].SuccessfulSuspendedRequests != suspendedPoint.SuccessfulSuspendedRequests ||
		m.SuspendedRequestHistory[len(m.SuspendedRequestHistory)-1].TimeoutSuspendedRequests != suspendedPoint.TimeoutSuspendedRequests {
		m.SuspendedRequestHistory = append(m.SuspendedRequestHistory, suspendedPoint)
	}

	// 限制历史数据大小
	if len(m.RequestHistory) > m.MaxHistoryPoints {
		m.RequestHistory = m.RequestHistory[len(m.RequestHistory)-m.MaxHistoryPoints:]
	}

	if len(m.ResponseHistory) > m.MaxHistoryPoints {
		m.ResponseHistory = m.ResponseHistory[len(m.ResponseHistory)-m.MaxHistoryPoints:]
	}

	if len(m.TokenHistory) > m.MaxHistoryPoints {
		m.TokenHistory = m.TokenHistory[len(m.TokenHistory)-m.MaxHistoryPoints:]
	}

	if len(m.SuspendedRequestHistory) > m.MaxHistoryPoints {
		m.SuspendedRequestHistory = m.SuspendedRequestHistory[len(m.SuspendedRequestHistory)-m.MaxHistoryPoints:]
	}
}

// GetAverageResponseTimeUnlocked 计算平均响应时间（无锁版本）
func (m *Metrics) GetAverageResponseTimeUnlocked() time.Duration {
	if m.TotalRequests == 0 {
		return 0
	}
	return m.TotalResponseTime / time.Duration(m.TotalRequests)
}

// GetChartDataForRequestHistory 获取请求历史图表数据
func (m *Metrics) GetChartDataForRequestHistory(minutes int) []RequestDataPoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	cutoff := now.Add(-time.Duration(minutes) * time.Minute)

	var result []RequestDataPoint
	for _, point := range m.RequestHistory {
		if point.Timestamp.After(cutoff) {
			result = append(result, point)
		}
	}

	return result
}

// GetChartDataForRequestHistoryWithBaseline 获取请求历史图表数据（带 baseline）
// 返回值会尽量在结果开头附带一个“cutoff 之前的最后一个点”，用于调用方计算相邻点增量，避免累计值图表在无请求时保持水平线。
func (m *Metrics) GetChartDataForRequestHistoryWithBaseline(minutes int) []RequestDataPoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	cutoff := now.Add(-time.Duration(minutes) * time.Minute)

	var baseline *RequestDataPoint
	var result []RequestDataPoint

	for _, point := range m.RequestHistory {
		if point.Timestamp.After(cutoff) {
			result = append(result, point)
			continue
		}
		copyPoint := point
		baseline = &copyPoint
	}

	if baseline == nil {
		return result
	}
	out := make([]RequestDataPoint, 0, len(result)+1)
	out = append(out, *baseline)
	out = append(out, result...)
	return out
}

// GetChartDataForResponseTime 获取响应时间图表数据
func (m *Metrics) GetChartDataForResponseTime(minutes int) []ResponseTimePoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	cutoff := now.Add(-time.Duration(minutes) * time.Minute)

	var result []ResponseTimePoint
	for _, point := range m.ResponseHistory {
		if point.Timestamp.After(cutoff) {
			result = append(result, point)
		}
	}

	return result
}

// GetChartDataForTokenHistory 获取Token历史图表数据
func (m *Metrics) GetChartDataForTokenHistory(minutes int) []TokenHistoryPoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	cutoff := now.Add(-time.Duration(minutes) * time.Minute)

	var result []TokenHistoryPoint
	for _, point := range m.TokenHistory {
		if point.Timestamp.After(cutoff) {
			result = append(result, point)
		}
	}

	return result
}
