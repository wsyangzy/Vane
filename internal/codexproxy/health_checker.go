// health_checker.go - Codex 端点健康检查器
// 定时对端点执行网络延迟探测（HTTP GET base_url/v1/models），
// 记录 RTT 并根据超时情况标记端点健康状态。

package codexproxy

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HealthChecker 定时对端点执行网络延迟探测
type HealthChecker struct {
	manager  *EndpointManager
	interval time.Duration // 探测间隔，默认 30s
	timeout  time.Duration // 单次探测超时，默认 5s

	mu        sync.RWMutex
	latencies map[string]int64 // endpoint name -> latency ms (-1 表示超时/不可达)

	client *http.Client
	stopCh chan struct{}
	wg     sync.WaitGroup
	logger *slog.Logger
}

// NewHealthChecker 创建健康检查器实例
func NewHealthChecker(
	manager *EndpointManager,
	interval, timeout time.Duration,
	logger *slog.Logger,
) *HealthChecker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &HealthChecker{
		manager:   manager,
		interval:  interval,
		timeout:   timeout,
		latencies: make(map[string]int64),
		client: &http.Client{
			Timeout: timeout,
		},
		stopCh: make(chan struct{}),
		logger: logger,
	}
}

// Start 启动定时探测
func (h *HealthChecker) Start() {
	h.wg.Add(1)
	go h.loop()
	h.logger.Info("Codex 健康检查器已启动",
		"interval", h.interval.String(),
		"timeout", h.timeout.String(),
	)
}

// Stop 停止探测
func (h *HealthChecker) Stop() {
	close(h.stopCh)
	h.wg.Wait()
	h.logger.Info("Codex 健康检查器已停止")
}

// GetLatencies 获取所有端点的延迟数据（供前端展示）
func (h *HealthChecker) GetLatencies() map[string]int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[string]int64, len(h.latencies))
	for k, v := range h.latencies {
		result[k] = v
	}
	return result
}

// loop 定时执行探测循环
func (h *HealthChecker) loop() {
	defer h.wg.Done()

	// 启动后立即执行一次探测
	h.checkAll()

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.checkAll()
		}
	}
}

// checkAll 对所有端点执行一轮探测
func (h *HealthChecker) checkAll() {
	endpoints := h.manager.GetAllEndpoints()
	if len(endpoints) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, ep := range endpoints {
		wg.Add(1)
		go func(endpoint *CodexEndpoint) {
			defer wg.Done()
			h.checkEndpoint(endpoint)
		}(ep)
	}
	wg.Wait()
}

// checkEndpoint 对单个端点执行 HTTP GET base_url/v1/models 测量 RTT
// 超时标记为 unhealthy（cooldown 30s），成功则标记 healthy 并记录延迟
func (h *HealthChecker) checkEndpoint(ep *CodexEndpoint) {
	url := buildProbeURL(ep.BaseURL)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		h.logger.Error("构建探测请求失败",
			"endpoint", ep.Name,
			"url", url,
			"error", err,
		)
		h.recordLatency(ep.Name, -1)
		h.manager.MarkUnhealthy(ep.Name, 0)
		return
	}

	// 添加认证头
	if ep.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ep.APIKey))
	}

	start := time.Now()
	resp, err := h.client.Do(req)
	elapsed := time.Since(start)
	latencyMs := elapsed.Milliseconds()

	if err != nil {
		h.logger.Warn("Codex 端点探测失败",
			"endpoint", ep.Name,
			"url", url,
			"error", err,
			"elapsed_ms", latencyMs,
		)
		h.recordLatency(ep.Name, -1)
		h.manager.MarkUnhealthy(ep.Name, 0)
		return
	}
	resp.Body.Close()

	// 任何能收到响应的情况都视为端点可达（包括 401/403 等认证错误）
	h.recordLatency(ep.Name, latencyMs)
	h.manager.MarkHealthy(ep.Name)

	h.logger.Debug("Codex 端点探测成功",
		"endpoint", ep.Name,
		"status", resp.StatusCode,
		"latency_ms", latencyMs,
	)
}

// recordLatency 记录端点延迟
func (h *HealthChecker) recordLatency(name string, ms int64) {
	h.mu.Lock()
	h.latencies[name] = ms
	h.mu.Unlock()
}

// buildProbeURL 构建探测 URL：base_url + /v1/models
func buildProbeURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	return base + "/v1/models"
}
