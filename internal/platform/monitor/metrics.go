package monitor

import (
	"sync"
	"time"
)

// TokenUsage represents token usage statistics
type TokenUsage struct {
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
}

// Metrics contains all monitoring metrics
type Metrics struct {
	mu sync.RWMutex

	// Request metrics
	TotalRequests      int64
	SuccessfulRequests int64
	FailedRequests     int64

	// Suspended request metrics
	SuspendedRequests           int64         // Current number of suspended requests
	TotalSuspendedRequests      int64         // Total historical suspended requests
	SuccessfulSuspendedRequests int64         // Successfully resumed suspended requests
	TimeoutSuspendedRequests    int64         // Timed out suspended requests
	TotalSuspendedTime          time.Duration // Total time spent in suspension
	MinSuspendedTime            time.Duration // Minimum suspension time
	MaxSuspendedTime            time.Duration // Maximum suspension time

	// Token usage metrics
	TotalTokenUsage TokenUsage

	// Failed request token statistics
	FailedRequestTokens    int64            // Total token count for failed requests
	FailedTokensByReason   map[string]int64 // Token statistics by failure reason
	FailedTokensByEndpoint map[string]int64 // Failed token statistics by endpoint

	// Response time metrics
	ResponseTimes     []time.Duration
	TotalResponseTime time.Duration
	MinResponseTime   time.Duration
	MaxResponseTime   time.Duration

	// Endpoint metrics
	EndpointStats map[string]*EndpointMetrics

	// Connection metrics
	ActiveConnections map[string]*ConnectionInfo
	ConnectionHistory []*ConnectionInfo

	// System metrics
	StartTime time.Time

	// Historical data (circular buffer)
	RequestHistory          []RequestDataPoint
	ResponseHistory         []ResponseTimePoint
	TokenHistory            []TokenHistoryPoint
	SuspendedRequestHistory []SuspendedRequestHistoryPoint
	MaxHistoryPoints        int
}

// EndpointMetrics tracks metrics for a specific endpoint
type EndpointMetrics struct {
	Name               string
	URL                string
	TotalRequests      int64
	SuccessfulRequests int64
	FailedRequests     int64
	TotalResponseTime  time.Duration
	MinResponseTime    time.Duration
	MaxResponseTime    time.Duration
	LastUsed           time.Time
	RetryCount         int64
	Priority           int
	Healthy            bool
	TokenUsage         TokenUsage
}

// ConnectionInfo represents an active connection
type ConnectionInfo struct {
	ID            string
	ClientIP      string
	UserAgent     string
	StartTime     time.Time
	LastActivity  time.Time
	Method        string
	Path          string
	Endpoint      string
	Port          string
	RetryCount    int
	Status        string // "active", "completed", "failed", "timeout", "suspended", "resumed"
	BytesReceived int64
	BytesSent     int64
	IsStreaming   bool
	TokenUsage    TokenUsage // Token usage for this connection

	// Suspended request related fields
	IsSuspended   bool          // Whether the connection is currently suspended
	SuspendedAt   time.Time     // When the request was suspended
	ResumedAt     time.Time     // When the request was resumed
	SuspendedTime time.Duration // Total time spent suspended
}

// RequestDataPoint represents a point in time for request metrics
type RequestDataPoint struct {
	Timestamp  time.Time
	Total      int64
	Successful int64
	Failed     int64
}

// ResponseTimePoint represents response time at a point in time
type ResponseTimePoint struct {
	Timestamp   time.Time
	AverageTime time.Duration
	MinTime     time.Duration
	MaxTime     time.Duration
}

// TokenHistoryPoint represents token usage at a point in time
type TokenHistoryPoint struct {
	Timestamp           time.Time
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	TotalTokens         int64
}

// SuspendedRequestHistoryPoint represents suspended request metrics at a point in time
type SuspendedRequestHistoryPoint struct {
	Timestamp                   time.Time
	SuspendedRequests           int64         // Current suspended requests at this point
	TotalSuspendedRequests      int64         // Total historical suspended requests
	SuccessfulSuspendedRequests int64         // Successfully resumed
	TimeoutSuspendedRequests    int64         // Timed out
	AverageSuspendedTime        time.Duration // Average suspension time
}

// NewMetrics creates a new metrics instance
func NewMetrics() *Metrics {
	return &Metrics{
		EndpointStats:           make(map[string]*EndpointMetrics),
		ActiveConnections:       make(map[string]*ConnectionInfo),
		ConnectionHistory:       make([]*ConnectionInfo, 0),
		StartTime:               time.Now(),
		RequestHistory:          make([]RequestDataPoint, 0),
		ResponseHistory:         make([]ResponseTimePoint, 0),
		TokenHistory:            make([]TokenHistoryPoint, 0),
		SuspendedRequestHistory: make([]SuspendedRequestHistoryPoint, 0),
		MaxHistoryPoints:        2880, // 24 hours at 30-second intervals (used by app.go history collector)
		MinResponseTime:         time.Duration(0),
		MaxResponseTime:         time.Duration(0),
		MinSuspendedTime:        time.Duration(0),
		MaxSuspendedTime:        time.Duration(0),
		FailedTokensByReason:    make(map[string]int64),
		FailedTokensByEndpoint:  make(map[string]int64),
	}
}
