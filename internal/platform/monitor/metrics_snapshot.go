package monitor

import (
	"time"
)

// GetMetrics returns a snapshot of current metrics
func (m *Metrics) GetMetrics() *Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create a copy of metrics
	snapshot := &Metrics{
		TotalRequests:               m.TotalRequests,
		SuccessfulRequests:          m.SuccessfulRequests,
		FailedRequests:              m.FailedRequests,
		SuspendedRequests:           m.SuspendedRequests,
		TotalSuspendedRequests:      m.TotalSuspendedRequests,
		SuccessfulSuspendedRequests: m.SuccessfulSuspendedRequests,
		TimeoutSuspendedRequests:    m.TimeoutSuspendedRequests,
		TotalSuspendedTime:          m.TotalSuspendedTime,
		MinSuspendedTime:            m.MinSuspendedTime,
		MaxSuspendedTime:            m.MaxSuspendedTime,
		TotalTokenUsage:             m.TotalTokenUsage,
		FailedRequestTokens:         m.FailedRequestTokens,
		FailedTokensByReason:        make(map[string]int64),
		FailedTokensByEndpoint:      make(map[string]int64),
		TotalResponseTime:           m.TotalResponseTime,
		MinResponseTime:             m.MinResponseTime,
		MaxResponseTime:             m.MaxResponseTime,
		StartTime:                   m.StartTime,
		EndpointStats:               make(map[string]*EndpointMetrics),
		ActiveConnections:           make(map[string]*ConnectionInfo),
		ConnectionHistory:           make([]*ConnectionInfo, len(m.ConnectionHistory)),
	}

	// Copy endpoint stats
	for k, v := range m.EndpointStats {
		snapshot.EndpointStats[k] = &EndpointMetrics{
			Name:               v.Name,
			URL:                v.URL,
			TotalRequests:      v.TotalRequests,
			SuccessfulRequests: v.SuccessfulRequests,
			FailedRequests:     v.FailedRequests,
			TotalResponseTime:  v.TotalResponseTime,
			MinResponseTime:    v.MinResponseTime,
			MaxResponseTime:    v.MaxResponseTime,
			LastUsed:           v.LastUsed,
			RetryCount:         v.RetryCount,
			Priority:           v.Priority,
			Healthy:            v.Healthy,
			TokenUsage:         v.TokenUsage,
		}
	}

	// Copy active connections
	for k, v := range m.ActiveConnections {
		snapshot.ActiveConnections[k] = &ConnectionInfo{
			ID:            v.ID,
			ClientIP:      v.ClientIP,
			UserAgent:     v.UserAgent,
			StartTime:     v.StartTime,
			LastActivity:  v.LastActivity,
			Method:        v.Method,
			Path:          v.Path,
			Endpoint:      v.Endpoint,
			Port:          v.Port,
			RetryCount:    v.RetryCount,
			Status:        v.Status,
			BytesReceived: v.BytesReceived,
			BytesSent:     v.BytesSent,
			IsStreaming:   v.IsStreaming,
			TokenUsage:    v.TokenUsage,
			IsSuspended:   v.IsSuspended,
			SuspendedAt:   v.SuspendedAt,
			ResumedAt:     v.ResumedAt,
			SuspendedTime: v.SuspendedTime,
		}
	}

	// Copy connection history
	for i, v := range m.ConnectionHistory {
		snapshot.ConnectionHistory[i] = &ConnectionInfo{
			ID:            v.ID,
			ClientIP:      v.ClientIP,
			UserAgent:     v.UserAgent,
			StartTime:     v.StartTime,
			LastActivity:  v.LastActivity,
			Method:        v.Method,
			Path:          v.Path,
			Endpoint:      v.Endpoint,
			Port:          v.Port,
			RetryCount:    v.RetryCount,
			Status:        v.Status,
			BytesReceived: v.BytesReceived,
			BytesSent:     v.BytesSent,
			IsStreaming:   v.IsStreaming,
			TokenUsage:    v.TokenUsage,
			IsSuspended:   v.IsSuspended,
			SuspendedAt:   v.SuspendedAt,
			ResumedAt:     v.ResumedAt,
			SuspendedTime: v.SuspendedTime,
		}
	}

	// Copy failed tokens maps
	for k, v := range m.FailedTokensByReason {
		snapshot.FailedTokensByReason[k] = v
	}
	for k, v := range m.FailedTokensByEndpoint {
		snapshot.FailedTokensByEndpoint[k] = v
	}

	// Copy response times (last 100)
	if len(m.ResponseTimes) > 0 {
		start := 0
		if len(m.ResponseTimes) > 100 {
			start = len(m.ResponseTimes) - 100
		}
		snapshot.ResponseTimes = make([]time.Duration, len(m.ResponseTimes[start:]))
		copy(snapshot.ResponseTimes, m.ResponseTimes[start:])
	}

	return snapshot
}

// GetAverageResponseTime calculates average response time
func (m *Metrics) GetAverageResponseTime() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.TotalRequests == 0 {
		return 0
	}
	return m.TotalResponseTime / time.Duration(m.TotalRequests)
}

// GetSuccessRate calculates success rate as percentage
func (m *Metrics) GetSuccessRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.TotalRequests == 0 {
		return 0
	}
	return float64(m.SuccessfulRequests) / float64(m.TotalRequests) * 100
}

// GetP95ResponseTime calculates 95th percentile response time
func (m *Metrics) GetP95ResponseTime() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.ResponseTimes) == 0 {
		return 0
	}

	// Simple approximation for P95
	index := int(float64(len(m.ResponseTimes)) * 0.95)
	if index >= len(m.ResponseTimes) {
		index = len(m.ResponseTimes) - 1
	}

	// For a proper implementation, we'd sort the slice
	// For now, return max as approximation
	return m.MaxResponseTime
}

// GetEndpointPerformanceData 获取端点性能统计数据
func (m *Metrics) GetEndpointPerformanceData() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []map[string]interface{}
	for _, endpoint := range m.EndpointStats {
		avgResponseTime := time.Duration(0)
		if endpoint.TotalRequests > 0 {
			avgResponseTime = endpoint.TotalResponseTime / time.Duration(endpoint.TotalRequests)
		}

		result = append(result, map[string]interface{}{
			"name":                endpoint.Name,
			"url":                 endpoint.URL,
			"healthy":             endpoint.Healthy,
			"total_requests":      endpoint.TotalRequests,
			"successful_requests": endpoint.SuccessfulRequests,
			"failed_requests":     endpoint.FailedRequests,
			"success_rate":        m.calculateEndpointSuccessRate(endpoint),
			"avg_response_time":   avgResponseTime.Milliseconds(),
			"min_response_time":   endpoint.MinResponseTime.Milliseconds(),
			"max_response_time":   endpoint.MaxResponseTime.Milliseconds(),
			"priority":            endpoint.Priority,
			"retry_count":         endpoint.RetryCount,
			"last_used":           endpoint.LastUsed,
			"token_usage":         endpoint.TokenUsage,
		})
	}

	return result
}

// calculateEndpointSuccessRate 计算端点成功率
func (m *Metrics) calculateEndpointSuccessRate(endpoint *EndpointMetrics) float64 {
	if endpoint.TotalRequests == 0 {
		return 0
	}
	return float64(endpoint.SuccessfulRequests) / float64(endpoint.TotalRequests) * 100
}

// GetConnectionActivityData 获取连接活动数据
func (m *Metrics) GetConnectionActivityData(minutes int) []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	cutoff := now.Add(-time.Duration(minutes) * time.Minute)

	// 创建时间间隔计数器
	intervals := make(map[string]int)
	intervalSize := time.Minute // 1分钟间隔

	// 从所有连接历史中收集数据
	for _, conn := range m.ConnectionHistory {
		if conn.StartTime.After(cutoff) {
			// 计算所属时间间隔
			interval := conn.StartTime.Truncate(intervalSize).Format("15:04")
			intervals[interval]++
		}
	}

	// 当前活跃连接
	for _, conn := range m.ActiveConnections {
		if conn.StartTime.After(cutoff) {
			interval := conn.StartTime.Truncate(intervalSize).Format("15:04")
			intervals[interval]++
		}
	}

	// 转换为图表数据格式
	var result []map[string]interface{}
	for interval, count := range intervals {
		result = append(result, map[string]interface{}{
			"time":  interval,
			"count": count,
		})
	}

	return result
}

// GetEndpointHealthDistribution 获取端点健康状态分布
func (m *Metrics) GetEndpointHealthDistribution() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	distribution := map[string]int{
		"healthy":   0,
		"unhealthy": 0,
	}

	for _, endpoint := range m.EndpointStats {
		if endpoint.Healthy {
			distribution["healthy"]++
		} else {
			distribution["unhealthy"]++
		}
	}

	return distribution
}
