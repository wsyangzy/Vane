package monitor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// RecordRequest records a new request
func (m *Metrics) RecordRequest(endpoint, clientIP, userAgent, method, path string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalRequests++

	// Update endpoint stats
	if m.EndpointStats[endpoint] == nil {
		m.EndpointStats[endpoint] = &EndpointMetrics{
			Name:            endpoint,
			MinResponseTime: time.Duration(0),
			MaxResponseTime: time.Duration(0),
		}
	}
	m.EndpointStats[endpoint].TotalRequests++
	m.EndpointStats[endpoint].LastUsed = time.Now()

	// Generate connection ID
	connID := generateConnectionID()

	// Create connection info
	conn := &ConnectionInfo{
		ID:            connID,
		ClientIP:      clientIP,
		UserAgent:     userAgent,
		StartTime:     time.Now(),
		LastActivity:  time.Now(),
		Method:        method,
		Path:          path,
		Endpoint:      endpoint,
		Status:        "active",
		RetryCount:    0,
		BytesReceived: 0,
		BytesSent:     0,
	}

	m.ActiveConnections[connID] = conn

	return connID
}

// RecordResponse records a response
func (m *Metrics) RecordResponse(connID string, statusCode int, responseTime time.Duration, bytesSent int64, endpoint string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update overall metrics
	m.TotalResponseTime += responseTime
	m.ResponseTimes = append(m.ResponseTimes, responseTime)

	// Update min/max response times
	if m.MinResponseTime == 0 || responseTime < m.MinResponseTime {
		m.MinResponseTime = responseTime
	}
	if responseTime > m.MaxResponseTime {
		m.MaxResponseTime = responseTime
	}

	// Track success/failure
	if statusCode >= 200 && statusCode < 400 {
		m.SuccessfulRequests++
		// Ensure endpoint stats exist
		if m.EndpointStats[endpoint] == nil && endpoint != "unknown" {
			m.EndpointStats[endpoint] = &EndpointMetrics{
				Name:            endpoint,
				MinResponseTime: time.Duration(0),
				MaxResponseTime: time.Duration(0),
			}
		}
		if endpoint != "unknown" && m.EndpointStats[endpoint] != nil {
			m.EndpointStats[endpoint].SuccessfulRequests++
			m.EndpointStats[endpoint].TotalRequests++
		}
	} else {
		m.FailedRequests++
		// Ensure endpoint stats exist
		if m.EndpointStats[endpoint] == nil && endpoint != "unknown" {
			m.EndpointStats[endpoint] = &EndpointMetrics{
				Name:            endpoint,
				MinResponseTime: time.Duration(0),
				MaxResponseTime: time.Duration(0),
			}
		}
		if endpoint != "unknown" && m.EndpointStats[endpoint] != nil {
			m.EndpointStats[endpoint].FailedRequests++
			m.EndpointStats[endpoint].TotalRequests++
		}
	}

	// Update endpoint metrics
	if endpoint != "unknown" && m.EndpointStats[endpoint] != nil {
		endpointMetrics := m.EndpointStats[endpoint]
		endpointMetrics.TotalResponseTime += responseTime
		endpointMetrics.LastUsed = time.Now()
		if endpointMetrics.MinResponseTime == 0 || responseTime < endpointMetrics.MinResponseTime {
			endpointMetrics.MinResponseTime = responseTime
		}
		if responseTime > endpointMetrics.MaxResponseTime {
			endpointMetrics.MaxResponseTime = responseTime
		}
	}

	// Update connection
	if conn, exists := m.ActiveConnections[connID]; exists {
		conn.LastActivity = time.Now()
		conn.BytesSent = bytesSent

		if statusCode >= 200 && statusCode < 400 {
			conn.Status = "completed"
		} else {
			conn.Status = "failed"
		}

		// Move to history and remove from active
		m.ConnectionHistory = append(m.ConnectionHistory, conn)
		delete(m.ActiveConnections, connID)

		// Limit history size
		if len(m.ConnectionHistory) > 1000 {
			m.ConnectionHistory = m.ConnectionHistory[len(m.ConnectionHistory)-1000:]
		}
	}

	// Limit response times history
	if len(m.ResponseTimes) > 1000 {
		m.ResponseTimes = m.ResponseTimes[len(m.ResponseTimes)-1000:]
	}
}

// RecordRetry records a retry attempt
func (m *Metrics) RecordRetry(connID string, endpoint string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conn, exists := m.ActiveConnections[connID]; exists {
		conn.RetryCount++
		conn.LastActivity = time.Now()
		// Debug log to verify retry recording
		fmt.Printf("DEBUG: Recorded retry %d for connection %s on endpoint %s\n", conn.RetryCount, connID, endpoint)
	} else {
		fmt.Printf("DEBUG: Connection %s not found for retry recording\n", connID)
	}

	if endpointMetrics := m.EndpointStats[endpoint]; endpointMetrics != nil {
		endpointMetrics.RetryCount++
	}
}

// UpdateEndpointHealth updates endpoint health status
func (m *Metrics) UpdateEndpointHealth(endpoint, url string, healthy bool, priority int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.EndpointStats[endpoint] == nil {
		m.EndpointStats[endpoint] = &EndpointMetrics{
			Name:            endpoint,
			URL:             url,
			Priority:        priority,
			MinResponseTime: time.Duration(0),
			MaxResponseTime: time.Duration(0),
		}
	}

	m.EndpointStats[endpoint].Healthy = healthy
	m.EndpointStats[endpoint].URL = url
	m.EndpointStats[endpoint].Priority = priority
}

// UpdateConnectionEndpoint updates the endpoint name for an active connection
func (m *Metrics) UpdateConnectionEndpoint(connID, endpoint string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conn, exists := m.ActiveConnections[connID]; exists {
		conn.Endpoint = endpoint
		conn.LastActivity = time.Now()
	}
}

// MarkStreamingConnection marks a connection as streaming
func (m *Metrics) MarkStreamingConnection(connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conn, exists := m.ActiveConnections[connID]; exists {
		conn.IsStreaming = true
		conn.LastActivity = time.Now()
	}
}

// generateConnectionID generates a unique connection ID in format req-xxxxxxxx
func generateConnectionID() string {
	// Generate 4 random bytes for 8 hex characters
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return "req-" + hex.EncodeToString(bytes)
}
