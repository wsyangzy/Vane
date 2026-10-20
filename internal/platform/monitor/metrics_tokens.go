package monitor

import (
	"time"
)

// RecordTokenUsage records token usage for a specific request
func (m *Metrics) RecordTokenUsage(connID string, endpoint string, tokens *TokenUsage) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 安全处理 nil tokens
	if tokens == nil {
		return
	}

	// Update overall token metrics
	m.TotalTokenUsage.InputTokens += tokens.InputTokens
	m.TotalTokenUsage.OutputTokens += tokens.OutputTokens
	m.TotalTokenUsage.CacheCreationTokens += tokens.CacheCreationTokens
	m.TotalTokenUsage.CacheReadTokens += tokens.CacheReadTokens

	// Update endpoint-specific token metrics
	if endpoint != "unknown" && m.EndpointStats[endpoint] != nil {
		m.EndpointStats[endpoint].TokenUsage.InputTokens += tokens.InputTokens
		m.EndpointStats[endpoint].TokenUsage.OutputTokens += tokens.OutputTokens
		m.EndpointStats[endpoint].TokenUsage.CacheCreationTokens += tokens.CacheCreationTokens
		m.EndpointStats[endpoint].TokenUsage.CacheReadTokens += tokens.CacheReadTokens
	}

	// Update connection info if available
	if conn, exists := m.ActiveConnections[connID]; exists {
		// Update token usage for this connection
		conn.TokenUsage.InputTokens += tokens.InputTokens
		conn.TokenUsage.OutputTokens += tokens.OutputTokens
		conn.TokenUsage.CacheCreationTokens += tokens.CacheCreationTokens
		conn.TokenUsage.CacheReadTokens += tokens.CacheReadTokens
		conn.LastActivity = time.Now()
	}

	// Note: Token history points are now added by AddHistoryDataPoints() method
	// This avoids duplicate history entries and provides better data sampling
}

// RecordFailedRequestTokenUsage 记录失败请求Token使用指标
func (m *Metrics) RecordFailedRequestTokenUsage(connID, endpoint string, tokens *TokenUsage, failureReason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 安全处理 nil tokens
	var inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens int64
	if tokens != nil {
		inputTokens = tokens.InputTokens
		outputTokens = tokens.OutputTokens
		cacheCreationTokens = tokens.CacheCreationTokens
		cacheReadTokens = tokens.CacheReadTokens
	}

	// 计算总Token数
	totalTokens := inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens

	// 记录失败请求Token统计
	m.FailedRequestTokens += totalTokens

	// 按失败原因分类统计
	if m.FailedTokensByReason == nil {
		m.FailedTokensByReason = make(map[string]int64)
	}
	m.FailedTokensByReason[failureReason] += totalTokens

	// 按端点分类统计失败Token
	if m.FailedTokensByEndpoint == nil {
		m.FailedTokensByEndpoint = make(map[string]int64)
	}
	m.FailedTokensByEndpoint[endpoint] += totalTokens

	// 注意：不再更新连接的Token使用情况
	// 这避免了与RecordTokenUsage的重复计算
	// 连接的Token统计应该由RecordTokenUsage负责
}

// GetTotalTokenStats returns total token usage statistics
func (m *Metrics) GetTotalTokenStats() TokenUsage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.TotalTokenUsage
}

// GetTokenHistory returns the token usage history
func (m *Metrics) GetTokenHistory() []TokenHistoryPoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy of the token history
	history := make([]TokenHistoryPoint, len(m.TokenHistory))
	copy(history, m.TokenHistory)
	return history
}
