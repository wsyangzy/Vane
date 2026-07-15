// health_checker.go - Codex 端点健康检查器
// 定时对端点执行网络延迟探测（HTTP GET base_url + health_path），
// 记录 RTT 并根据超时情况标记端点健康状态。
//
// 配置热生效：interval / timeout / health_path 每次 ticker / 探测时动态从 config 读取，
// 与 Claude 端 internal/endpoint/health_check.go 的行为保持一致。

package codexproxy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"ai-relay-forwarder/config"
)

const (
	defaultCodexHealthInterval = 30 * time.Second
	defaultCodexHealthTimeout  = 5 * time.Second
	defaultCodexHealthPath     = "/v1/models"
)

// HealthChecker 定时对端点执行网络延迟探测
type HealthChecker struct {
	manager *EndpointManager
	cfg     *config.Config // 动态读取 Health.* 字段，配置热生效

	mu         sync.RWMutex
	latencies  map[int64]int64     // endpoint ID -> latency ms (-1 表示超时/不可达)
	lastChecks map[int64]time.Time // endpoint ID -> last probe time

	stopCh chan struct{}
	wg     sync.WaitGroup
	logger *slog.Logger
}

// NewHealthChecker 创建健康检查器实例
// 参数 cfg 用于运行时动态读取 health.check_interval / timeout / health_path，
// 设置页改动后下一次 ticker / 探测即生效，无需重启。
func NewHealthChecker(manager *EndpointManager, cfg *config.Config, logger *slog.Logger) *HealthChecker {
	if logger == nil {
		logger = slog.Default()
	}
	return &HealthChecker{
		manager:    manager,
		cfg:        cfg,
		latencies:  make(map[int64]int64),
		lastChecks: make(map[int64]time.Time),
		stopCh:     make(chan struct{}),
		logger:     logger,
	}
}

// Start 启动定时探测
func (h *HealthChecker) Start() {
	h.wg.Add(1)
	go h.loop()
	interval, timeout, healthPath := h.readHealthConfig()
	h.logger.Info("Codex 健康检查器已启动",
		"interval", interval.String(),
		"timeout", timeout.String(),
		"health_path", healthPath,
	)
}

// Stop 停止探测
func (h *HealthChecker) Stop() {
	close(h.stopCh)
	h.wg.Wait()
	h.logger.Info("Codex 健康检查器已停止")
}

// CheckNow 异步立即触发一次完整探测（不阻塞调用方）
// 用于渠道/端点 CRUD 后立即刷新健康状态，避免用户等待下一个 ticker 周期（最多 30s）
func (h *HealthChecker) CheckNow() {
	go h.checkAll()
}

// GetLatencies 获取所有端点的延迟数据（供前端展示）
func (h *HealthChecker) GetLatencies() map[int64]int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[int64]int64, len(h.latencies))
	for k, v := range h.latencies {
		result[k] = v
	}
	return result
}

// GetLastChecks 获取所有端点的最近探测时间（供前端展示）
func (h *HealthChecker) GetLastChecks() map[int64]time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[int64]time.Time, len(h.lastChecks))
	for k, v := range h.lastChecks {
		result[k] = v
	}
	return result
}

// readHealthConfig 动态读取健康检查配置（带默认值兜底）
// 每次 ticker 调整、每次探测时都重新读取，确保设置页改动立即生效
func (h *HealthChecker) readHealthConfig() (interval, timeout time.Duration, healthPath string) {
	interval = defaultCodexHealthInterval
	timeout = defaultCodexHealthTimeout
	healthPath = defaultCodexHealthPath
	if h.cfg == nil {
		return
	}
	if h.cfg.Health.CheckInterval > 0 {
		interval = h.cfg.Health.CheckInterval
	}
	if h.cfg.Health.Timeout > 0 {
		timeout = h.cfg.Health.Timeout
	}
	if trimmed := strings.TrimSpace(h.cfg.Health.HealthPath); trimmed != "" {
		healthPath = trimmed
	}
	return
}

// loop 定时执行探测循环（间隔动态调整，与 Claude 端 health_check.go 行为对齐）
func (h *HealthChecker) loop() {
	defer h.wg.Done()

	// 启动后立即执行一次探测
	h.checkAll()

	currentInterval, _, _ := h.readHealthConfig()
	ticker := time.NewTicker(currentInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			// 动态调整间隔（设置页改了 check_interval 后下一轮即生效）
			newInterval, _, _ := h.readHealthConfig()
			if newInterval != currentInterval {
				h.logger.Info("Codex 健康检查间隔已更新",
					"old", currentInterval,
					"new", newInterval,
				)
				currentInterval = newInterval
				ticker.Stop()
				ticker = time.NewTicker(currentInterval)
			}
			h.checkAll()
		}
	}
}

// checkAll 对所有端点执行一轮探测
func (h *HealthChecker) checkAll() {
	channelFailoverEnabled := h.cfg != nil && h.cfg.CodexFailover.Enabled
	endpoints, err := h.manager.GetHealthCheckEndpoints(context.Background(), channelFailoverEnabled)
	if err != nil {
		h.logger.Warn("加载 Codex 健康检查端点失败", "error", err)
		return
	}
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

// checkEndpoint 对单个端点执行 HTTP GET base_url + healthPath 测量 RTT
// timeout / healthPath 每次都动态读取，配置变更立即生效
func (h *HealthChecker) checkEndpoint(ep *CodexEndpoint) {
	_, timeout, healthPath := h.readHealthConfig()
	url := buildProbeURL(ep.BaseURL, healthPath)
	skipHealthState := ep.SkipHealthCheck

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		h.logger.Error("构建探测请求失败",
			"endpoint", ep.Name,
			"url", url,
			"error", err,
		)
		h.recordLatency(ep.ID, -1)
		if !skipHealthState {
			h.manager.MarkUnhealthyWithDetail(ep.ID, 0, err.Error())
		}
		return
	}

	// 添加认证头
	if ep.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ep.APIKey))
	}

	// 每次探测用独立 client（开销小且确保 timeout 生效不依赖 client.Timeout）
	client := &http.Client{Timeout: timeout}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	latencyMs := elapsed.Milliseconds()

	if err != nil {
		h.logger.Warn("Codex 端点探测失败",
			"endpoint", ep.Name,
			"url", url,
			"error", err,
			"elapsed_ms", latencyMs,
		)
		h.recordLatency(ep.ID, -1)
		if !skipHealthState {
			h.manager.MarkUnhealthyWithDetail(ep.ID, 0, err.Error())
		}
		return
	}
	resp.Body.Close()

	// 任何能收到响应的情况都视为端点可达（包括 401/403 等认证错误）
	h.recordLatency(ep.ID, latencyMs)
	if !skipHealthState {
		h.manager.MarkHealthy(ep.ID)
	} else {
		h.manager.SetFailureDetail(ep.ID, 0, "")
	}

	h.logger.Debug("Codex 端点探测成功",
		"endpoint", ep.Name,
		"status", resp.StatusCode,
		"latency_ms", latencyMs,
		"skip_health_check", skipHealthState,
	)
}

// recordLatency 记录端点延迟
func (h *HealthChecker) recordLatency(endpointID int64, ms int64) {
	if endpointID <= 0 {
		return
	}

	if h.manager != nil {
		h.manager.RecordLatency(endpointID, ms)
	}

	h.mu.Lock()
	h.latencies[endpointID] = ms
	h.lastChecks[endpointID] = time.Now()
	h.mu.Unlock()
}

// buildProbeURL 构建探测 URL。
// - 若 healthPath 以 / 开头：与 baseURL 拼接时兼容 baseURL 已包含 /v1 的情况（当 healthPath 也以 /v1/ 开头时，避免出现 /v1/v1/ 重复）。
// - 若 healthPath 不以 / 开头：直接以 / 拼接到 baseURL 之后。
func buildProbeURL(baseURL, healthPath string) string {
	path := strings.TrimSpace(healthPath)
	if path == "" {
		path = defaultCodexHealthPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return path
	}

	u, err := url.Parse(base)
	if err != nil {
		return base + path
	}

	urlPath := strings.TrimRight(u.Path, "/")
	// 当 baseURL 路径已以 /v1 结尾、且 healthPath 也以 /v1/ 开头时，去掉重复的 /v1 前缀避免 /v1/v1/
	if strings.HasSuffix(urlPath, "/v1") && strings.HasPrefix(path, "/v1/") {
		u.Path = urlPath + strings.TrimPrefix(path, "/v1")
		return u.String()
	}

	u.Path = urlPath + path
	return u.String()
}
