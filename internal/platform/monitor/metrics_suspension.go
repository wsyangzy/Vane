package monitor

import (
	"time"
)

// RecordRequestSuspended records a request being suspended
func (m *Metrics) RecordRequestSuspended(connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SuspendedRequests++
	m.TotalSuspendedRequests++

	// Update connection status
	if conn, exists := m.ActiveConnections[connID]; exists {
		conn.IsSuspended = true
		conn.SuspendedAt = time.Now()
		conn.Status = "suspended"
		conn.LastActivity = time.Now()
	}
}

// RecordRequestResumed records a suspended request being resumed
func (m *Metrics) RecordRequestResumed(connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SuspendedRequests--
	m.SuccessfulSuspendedRequests++

	// Update connection status and calculate suspended time
	if conn, exists := m.ActiveConnections[connID]; exists && conn.IsSuspended {
		conn.IsSuspended = false
		conn.ResumedAt = time.Now()
		conn.Status = "active" // Back to active status
		conn.LastActivity = time.Now()

		// Calculate suspended duration
		if !conn.SuspendedAt.IsZero() {
			suspendedDuration := conn.ResumedAt.Sub(conn.SuspendedAt)
			conn.SuspendedTime = suspendedDuration

			// Update overall suspended time metrics
			m.TotalSuspendedTime += suspendedDuration
			if m.MinSuspendedTime == 0 || suspendedDuration < m.MinSuspendedTime {
				m.MinSuspendedTime = suspendedDuration
			}
			if suspendedDuration > m.MaxSuspendedTime {
				m.MaxSuspendedTime = suspendedDuration
			}
		}
	}
}

// RecordRequestSuspendTimeout records a suspended request timing out
func (m *Metrics) RecordRequestSuspendTimeout(connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SuspendedRequests--
	m.TimeoutSuspendedRequests++

	// Update connection status and calculate suspended time
	if conn, exists := m.ActiveConnections[connID]; exists && conn.IsSuspended {
		conn.IsSuspended = false
		conn.Status = "timeout"
		conn.LastActivity = time.Now()

		// Calculate suspended duration
		if !conn.SuspendedAt.IsZero() {
			suspendedDuration := time.Since(conn.SuspendedAt)
			conn.SuspendedTime = suspendedDuration

			// Update overall suspended time metrics
			m.TotalSuspendedTime += suspendedDuration
			if m.MinSuspendedTime == 0 || suspendedDuration < m.MinSuspendedTime {
				m.MinSuspendedTime = suspendedDuration
			}
			if suspendedDuration > m.MaxSuspendedTime {
				m.MaxSuspendedTime = suspendedDuration
			}
		}

		// Move to history since the request failed due to timeout
		m.ConnectionHistory = append(m.ConnectionHistory, conn)
		delete(m.ActiveConnections, connID)

		// Limit history size
		if len(m.ConnectionHistory) > 1000 {
			m.ConnectionHistory = m.ConnectionHistory[len(m.ConnectionHistory)-1000:]
		}
	}
}

// GetAverageSuspendedTime calculates average suspended time
func (m *Metrics) GetAverageSuspendedTime() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalProcessed := m.SuccessfulSuspendedRequests + m.TimeoutSuspendedRequests
	if totalProcessed == 0 {
		return 0
	}
	return m.TotalSuspendedTime / time.Duration(totalProcessed)
}

// GetSuspendedRequestStats returns suspended request statistics
func (m *Metrics) GetSuspendedRequestStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalProcessed := m.SuccessfulSuspendedRequests + m.TimeoutSuspendedRequests
	successRate := 0.0
	if totalProcessed > 0 {
		successRate = float64(m.SuccessfulSuspendedRequests) / float64(totalProcessed) * 100
	}

	return map[string]interface{}{
		"suspended_requests":            m.SuspendedRequests,
		"total_suspended_requests":      m.TotalSuspendedRequests,
		"successful_suspended_requests": m.SuccessfulSuspendedRequests,
		"timeout_suspended_requests":    m.TimeoutSuspendedRequests,
		"success_rate":                  successRate,
		"total_suspended_time":          m.TotalSuspendedTime.String(),
		"average_suspended_time":        m.GetAverageSuspendedTimeUnlocked().String(),
		"min_suspended_time":            m.MinSuspendedTime.String(),
		"max_suspended_time":            m.MaxSuspendedTime.String(),
	}
}

// GetAverageSuspendedTimeUnlocked calculates average suspended time (unlocked version)
func (m *Metrics) GetAverageSuspendedTimeUnlocked() time.Duration {
	totalProcessed := m.SuccessfulSuspendedRequests + m.TimeoutSuspendedRequests
	if totalProcessed == 0 {
		return 0
	}
	return m.TotalSuspendedTime / time.Duration(totalProcessed)
}

// GetSuspendedRequestHistory returns the suspended request history
func (m *Metrics) GetSuspendedRequestHistory() []SuspendedRequestHistoryPoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy of the suspended request history
	history := make([]SuspendedRequestHistoryPoint, len(m.SuspendedRequestHistory))
	copy(history, m.SuspendedRequestHistory)
	return history
}

// GetChartDataForSuspendedRequests gets suspended request chart data
func (m *Metrics) GetChartDataForSuspendedRequests(minutes int) []SuspendedRequestHistoryPoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	cutoff := now.Add(-time.Duration(minutes) * time.Minute)

	var result []SuspendedRequestHistoryPoint
	for _, point := range m.SuspendedRequestHistory {
		if point.Timestamp.After(cutoff) {
			result = append(result, point)
		}
	}

	return result
}

// GetActiveSuspendedConnections returns currently suspended connections
func (m *Metrics) GetActiveSuspendedConnections() []*ConnectionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var suspendedConnections []*ConnectionInfo
	for _, conn := range m.ActiveConnections {
		if conn.IsSuspended {
			suspendedConnections = append(suspendedConnections, &ConnectionInfo{
				ID:            conn.ID,
				ClientIP:      conn.ClientIP,
				UserAgent:     conn.UserAgent,
				StartTime:     conn.StartTime,
				LastActivity:  conn.LastActivity,
				Method:        conn.Method,
				Path:          conn.Path,
				Endpoint:      conn.Endpoint,
				Port:          conn.Port,
				RetryCount:    conn.RetryCount,
				Status:        conn.Status,
				BytesReceived: conn.BytesReceived,
				BytesSent:     conn.BytesSent,
				IsStreaming:   conn.IsStreaming,
				TokenUsage:    conn.TokenUsage,
				IsSuspended:   conn.IsSuspended,
				SuspendedAt:   conn.SuspendedAt,
				ResumedAt:     conn.ResumedAt,
				SuspendedTime: conn.SuspendedTime,
			})
		}
	}

	return suspendedConnections
}
