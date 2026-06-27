// app_api.go - 暴露给前端的 API 方法 (Wails Bindings)
// 这些方法会被自动生成为 JavaScript 调用
//
// API 文件按功能模块拆分:
// - app_api.go         - 系统状态、配置、辅助函数 (本文件)
// - app_api_endpoint.go - 端点管理 + Key 管理
// - app_api_group.go    - 组管理
// - app_api_usage.go    - 使用统计 + 请求记录
// - app_api_chart.go    - 图表数据
// app_api_storage.go - 端点存储管理 (SQLite)

package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	appcache "ai-relay-forwarder/internal/cache"
	"ai-relay-forwarder/internal/utils"
)

// 端口检测缓存（避免频繁 TCP 连接）
// 使用 map 绑定到具体的 host:port，避免配置更改后返回旧端口状态
type portCheckCacheEntry struct {
	result    bool
	timestamp time.Time
}

var (
	portCheckCacheMap = make(map[string]portCheckCacheEntry) // key: "host:port"
	portCheckCacheMu  sync.RWMutex
	portCheckCacheTTL = 500 * time.Millisecond // 缓存有效期

	portCheckCacheHits              atomic.Uint64
	portCheckCacheMisses            atomic.Uint64
	portCheckCacheExpired           atomic.Uint64
	portCheckCacheInvalidations     atomic.Uint64
	portCheckCacheLastInvalidatedAt time.Time
)

// ============================================================
// 系统状态 API
// ============================================================

// SystemStatus 系统状态结构
type SystemStatus struct {
	Version       string `json:"version"`
	Uptime        string `json:"uptime"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	StartTime     string `json:"start_time"` // ISO8601 格式的启动时间
	ProxyPort     int    `json:"proxy_port"`
	ProxyHost     string `json:"proxy_host"`
	ProxyRunning  bool   `json:"proxy_running"`
	ActiveGroup   string `json:"active_group"`
	ConfigPath    string `json:"config_path"`
	AuthEnabled   bool   `json:"auth_enabled"`

	// Codex 代理状态（v7.2+ 合并到主端口，运行时默认可用）
	CodexProxyEnabled bool `json:"codex_proxy_enabled"`
}

// GetSystemStatus 获取系统状态
func (a *App) GetSystemStatus() SystemStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()

	uptime := time.Since(a.startTime)

	status := SystemStatus{
		Version:       Version,
		Uptime:        formatDuration(uptime),
		UptimeSeconds: int64(uptime.Seconds()),
		StartTime:     a.startTime.Format(time.RFC3339),
		ProxyRunning:  a.isRunning && a.proxyServer != nil,
		ConfigPath:    a.configPath,
	}

	if a.config != nil {
		status.ProxyPort = a.config.Server.Port
		status.ProxyHost = a.config.Server.Host
		status.AuthEnabled = a.config.Auth.Enabled

		// 真正检测端口是否在监听
		if status.ProxyRunning {
			status.ProxyRunning = a.checkPortListening(status.ProxyHost, status.ProxyPort)
		}

		// Codex 代理与 Claude 代理共用主端口，保留字段用于前端兼容。
		status.CodexProxyEnabled = true
	}

	if a.endpointManager != nil {
		gm := a.endpointManager.GetGroupManager()
		if gm != nil {
			activeGroups := gm.GetActiveGroups()
			if len(activeGroups) > 0 {
				status.ActiveGroup = activeGroups[0].Name
			}
		}
	}

	return status
}

// ClaudeCodeVersion 缓存 Claude Code 版本信息
var (
	claudeCodeVersionCache     string
	claudeCodeVersionCacheTime time.Time
	claudeCodeVersionCacheTTL  = 5 * time.Minute // 缓存 5 分钟
	claudeCodeVersionMu        sync.RWMutex

	claudeCodeVersionCacheHits              atomic.Uint64
	claudeCodeVersionCacheMisses            atomic.Uint64
	claudeCodeVersionCacheExpired           atomic.Uint64
	claudeCodeVersionCacheInvalidations     atomic.Uint64
	claudeCodeVersionCacheLastInvalidatedAt time.Time
)

// GetClaudeCodeVersion 获取 Claude Code CLI 版本
// 执行 `claude -v` 命令获取版本信息
func (a *App) GetClaudeCodeVersion() string {
	// 先检查缓存
	claudeCodeVersionMu.RLock()
	if claudeCodeVersionCache != "" && time.Since(claudeCodeVersionCacheTime) < claudeCodeVersionCacheTTL {
		cached := claudeCodeVersionCache
		claudeCodeVersionMu.RUnlock()
		claudeCodeVersionCacheHits.Add(1)
		return cached
	}
	expired := claudeCodeVersionCache != ""
	claudeCodeVersionMu.RUnlock()
	if expired {
		claudeCodeVersionCacheExpired.Add(1)
	}
	claudeCodeVersionCacheMisses.Add(1)

	// 创建命令并配置 Windows 隐藏窗口
	runClaudeCommand := func(args ...string) (string, error) {
		cmd := exec.Command("claude", args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		// Windows 下隐藏命令行窗口（CREATE_NO_WINDOW = 0x08000000）
		if runtime.GOOS == "windows" {
			cmd.SysProcAttr = &syscall.SysProcAttr{
				HideWindow: true,
			}
		}

		err := cmd.Run()
		if err != nil {
			return "", err
		}

		output := strings.TrimSpace(stdout.String())
		if output == "" {
			output = strings.TrimSpace(stderr.String())
		}
		return output, nil
	}

	// 尝试 claude -v
	output, err := runClaudeCommand("-v")
	if err != nil {
		// 尝试 claude --version
		output, err = runClaudeCommand("--version")
		if err != nil {
			return "未安装"
		}
	}

	if output == "" {
		return "未安装"
	}

	// 提取版本号（格式可能是 "claude 1.0.0" 或直接 "1.0.0"）
	version := output
	if strings.Contains(output, " ") {
		parts := strings.Fields(output)
		for _, part := range parts {
			// 查找看起来像版本号的部分（包含数字和点）
			if strings.Contains(part, ".") && len(part) > 0 && (part[0] >= '0' && part[0] <= '9') {
				version = part
				break
			}
		}
	}

	// 更新缓存
	claudeCodeVersionMu.Lock()
	claudeCodeVersionCache = version
	claudeCodeVersionCacheTime = time.Now()
	claudeCodeVersionMu.Unlock()

	return version
}

// CodexVersion 缓存 Codex CLI 版本信息
var (
	codexVersionCache     string
	codexVersionCacheTime time.Time
	codexVersionCacheTTL  = 5 * time.Minute // 缓存 5 分钟
	codexVersionMu        sync.RWMutex

	codexVersionCacheHits              atomic.Uint64
	codexVersionCacheMisses            atomic.Uint64
	codexVersionCacheExpired           atomic.Uint64
	codexVersionCacheInvalidations     atomic.Uint64
	codexVersionCacheLastInvalidatedAt time.Time
)

// GetCodexVersion 获取 Codex CLI 版本
// 执行 `codex --version` 命令获取版本信息
func (a *App) GetCodexVersion() string {
	// 先检查缓存
	codexVersionMu.RLock()
	if codexVersionCache != "" && time.Since(codexVersionCacheTime) < codexVersionCacheTTL {
		cached := codexVersionCache
		codexVersionMu.RUnlock()
		codexVersionCacheHits.Add(1)
		return cached
	}
	expired := codexVersionCache != ""
	codexVersionMu.RUnlock()
	if expired {
		codexVersionCacheExpired.Add(1)
	}
	codexVersionCacheMisses.Add(1)

	// 创建命令并配置 Windows 隐藏窗口
	runCodexCommand := func(args ...string) (string, error) {
		cmd := exec.Command("codex", args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		// Windows 下隐藏命令行窗口（CREATE_NO_WINDOW = 0x08000000）
		if runtime.GOOS == "windows" {
			cmd.SysProcAttr = &syscall.SysProcAttr{
				HideWindow: true,
			}
		}

		err := cmd.Run()
		if err != nil {
			return "", err
		}

		output := strings.TrimSpace(stdout.String())
		if output == "" {
			output = strings.TrimSpace(stderr.String())
		}
		return output, nil
	}

	// 尝试 codex --version
	output, err := runCodexCommand("--version")
	if err != nil {
		// 尝试 codex -v
		output, err = runCodexCommand("-v")
		if err != nil {
			return "未安装"
		}
	}

	if output == "" {
		return "未安装"
	}

	// 提取版本号（格式可能是 "codex 1.0.0" 或直接 "1.0.0"）
	version := output
	if strings.Contains(output, " ") {
		parts := strings.Fields(output)
		for _, part := range parts {
			// 查找看起来像版本号的部分（包含数字和点）
			if strings.Contains(part, ".") && len(part) > 0 && (part[0] >= '0' && part[0] <= '9') {
				version = part
				break
			}
		}
	}

	// 更新缓存
	codexVersionMu.Lock()
	codexVersionCache = version
	codexVersionCacheTime = time.Now()
	codexVersionMu.Unlock()

	return version
}

// checkPortListening 检测端口是否在监听（带缓存，避免频繁 TCP 连接）
func (a *App) checkPortListening(host string, port int) bool {
	// 使用 net.JoinHostPort 正确处理 IPv6 地址
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	// 先检查缓存（绑定到具体的 host:port）
	portCheckCacheMu.RLock()
	if entry, ok := portCheckCacheMap[addr]; ok {
		if time.Since(entry.timestamp) < portCheckCacheTTL {
			portCheckCacheMu.RUnlock()
			portCheckCacheHits.Add(1)
			return entry.result
		}
		portCheckCacheExpired.Add(1)
	}
	portCheckCacheMu.RUnlock()
	portCheckCacheMisses.Add(1)

	// 缓存过期或不存在，执行真实检测
	conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)

	result := err == nil
	if conn != nil {
		conn.Close()
	}

	// 更新缓存
	portCheckCacheMu.Lock()
	portCheckCacheMap[addr] = portCheckCacheEntry{
		result:    result,
		timestamp: time.Now(),
	}
	portCheckCacheMu.Unlock()

	return result
}

func portCheckCacheStats() appcache.Stats {
	now := time.Now()
	portCheckCacheMu.Lock()
	for key, entry := range portCheckCacheMap {
		if now.Sub(entry.timestamp) >= portCheckCacheTTL {
			delete(portCheckCacheMap, key)
			portCheckCacheExpired.Add(1)
		}
	}
	lastUpdatedAt := time.Time{}
	for _, entry := range portCheckCacheMap {
		if entry.timestamp.After(lastUpdatedAt) {
			lastUpdatedAt = entry.timestamp
		}
	}
	stats := appcache.Stats{
		Name:              cacheNameRuntimePortCheck,
		Enabled:           true,
		TTL:               portCheckCacheTTL,
		Size:              len(portCheckCacheMap),
		Hits:              portCheckCacheHits.Load(),
		Misses:            portCheckCacheMisses.Load(),
		Expired:           portCheckCacheExpired.Load(),
		Invalidations:     portCheckCacheInvalidations.Load(),
		LastUpdatedAt:     lastUpdatedAt,
		LastInvalidatedAt: portCheckCacheLastInvalidatedAt,
	}
	portCheckCacheMu.Unlock()
	return stats
}

func invalidatePortCheckCache() int {
	portCheckCacheMu.Lock()
	removed := len(portCheckCacheMap)
	portCheckCacheMap = make(map[string]portCheckCacheEntry)
	portCheckCacheLastInvalidatedAt = time.Now()
	portCheckCacheMu.Unlock()
	portCheckCacheInvalidations.Add(1)
	return removed
}

func claudeCodeVersionCacheStats() appcache.Stats {
	claudeCodeVersionMu.Lock()
	if claudeCodeVersionCache != "" && time.Since(claudeCodeVersionCacheTime) >= claudeCodeVersionCacheTTL {
		claudeCodeVersionCache = ""
		claudeCodeVersionCacheTime = time.Time{}
		claudeCodeVersionCacheExpired.Add(1)
	}
	size := 0
	if claudeCodeVersionCache != "" {
		size = 1
	}
	stats := appcache.Stats{
		Name:              cacheNameRuntimeClaudeCLI,
		Enabled:           true,
		TTL:               claudeCodeVersionCacheTTL,
		MaxEntries:        1,
		Size:              size,
		Hits:              claudeCodeVersionCacheHits.Load(),
		Misses:            claudeCodeVersionCacheMisses.Load(),
		Expired:           claudeCodeVersionCacheExpired.Load(),
		Invalidations:     claudeCodeVersionCacheInvalidations.Load(),
		LastUpdatedAt:     claudeCodeVersionCacheTime,
		LastInvalidatedAt: claudeCodeVersionCacheLastInvalidatedAt,
	}
	claudeCodeVersionMu.Unlock()
	return stats
}

func invalidateClaudeCodeVersionCache() int {
	claudeCodeVersionMu.Lock()
	removed := 0
	if claudeCodeVersionCache != "" {
		removed = 1
	}
	claudeCodeVersionCache = ""
	claudeCodeVersionCacheTime = time.Time{}
	claudeCodeVersionCacheLastInvalidatedAt = time.Now()
	claudeCodeVersionMu.Unlock()
	claudeCodeVersionCacheInvalidations.Add(1)
	return removed
}

func codexVersionCacheStats() appcache.Stats {
	codexVersionMu.Lock()
	if codexVersionCache != "" && time.Since(codexVersionCacheTime) >= codexVersionCacheTTL {
		codexVersionCache = ""
		codexVersionCacheTime = time.Time{}
		codexVersionCacheExpired.Add(1)
	}
	size := 0
	if codexVersionCache != "" {
		size = 1
	}
	stats := appcache.Stats{
		Name:              cacheNameRuntimeCodexCLI,
		Enabled:           true,
		TTL:               codexVersionCacheTTL,
		MaxEntries:        1,
		Size:              size,
		Hits:              codexVersionCacheHits.Load(),
		Misses:            codexVersionCacheMisses.Load(),
		Expired:           codexVersionCacheExpired.Load(),
		Invalidations:     codexVersionCacheInvalidations.Load(),
		LastUpdatedAt:     codexVersionCacheTime,
		LastInvalidatedAt: codexVersionCacheLastInvalidatedAt,
	}
	codexVersionMu.Unlock()
	return stats
}

func invalidateCodexVersionCache() int {
	codexVersionMu.Lock()
	removed := 0
	if codexVersionCache != "" {
		removed = 1
	}
	codexVersionCache = ""
	codexVersionCacheTime = time.Time{}
	codexVersionCacheLastInvalidatedAt = time.Now()
	codexVersionMu.Unlock()
	codexVersionCacheInvalidations.Add(1)
	return removed
}

// ============================================================
// 端口冲突 API
// ============================================================

// PortConflictStatus 端口冲突状态
type PortConflictStatus struct {
	HasConflict  bool   `json:"has_conflict"`  // 是否存在冲突
	ConflictPort int    `json:"conflict_port"` // 冲突的端口号
	ErrorMessage string `json:"error_message"` // 错误信息
	ProxyAddress string `json:"proxy_address"` // 当前代理地址（成功时）
	ProxyRunning bool   `json:"proxy_running"` // 代理是否正在运行
}

// GetPortConflictStatus 获取端口冲突状态
func (a *App) GetPortConflictStatus() PortConflictStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()

	status := PortConflictStatus{
		HasConflict:  a.portConflict,
		ConflictPort: a.portConflictPort,
		ErrorMessage: a.portConflictErr,
		ProxyRunning: a.isRunning && a.proxyServer != nil,
	}

	if status.ProxyRunning {
		status.ProxyAddress = a.GetProxyAddress()
	}

	return status
}

// PortConflictResult 端口冲突处理结果
type PortConflictResult struct {
	Success      bool   `json:"success"`
	ProxyAddress string `json:"proxy_address"` // 成功时的代理地址
	ErrorMessage string `json:"error_message"` // 失败时的错误信息
}

// ResolvePortConflict 处理端口冲突
// action: "retry" - 重试首选端口, "switch" - 切换到其他可用端口
func (a *App) ResolvePortConflict(action string) PortConflictResult {
	a.mu.Lock()

	if !a.portConflict {
		addr := a.GetProxyAddress()
		a.mu.Unlock()
		return PortConflictResult{
			Success:      true,
			ProxyAddress: addr,
		}
	}

	preferredPort := a.config.Server.Port
	if a.portManager != nil {
		preferredPort = a.portManager.GetPreferredPort()
	}

	var listener net.Listener
	var actualPort int
	var err error

	switch action {
	case "retry":
		// 重试首选端口
		addr := fmt.Sprintf("%s:%d", a.config.Server.Host, preferredPort)
		listener, err = net.Listen("tcp", addr)
		if err != nil {
			a.mu.Unlock()
			return PortConflictResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("端口 %d 仍被占用: %v", preferredPort, err),
			}
		}
		actualPort = preferredPort

	case "switch":
		// 切换到其他可用端口（使用配置的 host 地址绑定）
		host := a.config.Server.Host
		if host == "" || host == "localhost" {
			host = "127.0.0.1"
		}
		listener, actualPort, err = utils.FindAndBindWithHost(host, preferredPort, 10)
		if err != nil {
			a.mu.Unlock()
			return PortConflictResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("无法找到可用端口: %v", err),
			}
		}

	default:
		a.mu.Unlock()
		return PortConflictResult{
			Success:      false,
			ErrorMessage: "无效的操作: " + action,
		}
	}

	// 成功绑定端口，启动代理服务器
	a.portConflict = false
	a.portConflictPort = 0
	a.portConflictErr = ""

	if a.portManager != nil {
		a.portManager.SetActualPort(actualPort)
	}
	a.config.Server.Port = actualPort

	// 创建并启动代理服务器
	a.startProxyServerWithListener(listener, actualPort)

	// 代理地址统一使用 127.0.0.1（避免 localhost 在某些环境下解析问题）
	displayHost := a.config.Server.Host
	if displayHost == "" || displayHost == "localhost" {
		displayHost = "127.0.0.1"
	}
	proxyAddr := fmt.Sprintf("http://%s:%d", displayHost, actualPort)
	a.logger.Info("✅ 代理服务器启动成功", "address", proxyAddr)

	// 释放锁后发送系统状态更新事件，通知前端刷新端口信息
	a.mu.Unlock()
	a.emitSystemStatus()

	return PortConflictResult{
		Success:      true,
		ProxyAddress: proxyAddr,
	}
}

// ============================================================
// 配置 API
// ============================================================

// ConfigInfo 配置信息（脱敏）
type ConfigInfo struct {
	ServerHost      string `json:"server_host"`
	ServerPort      int    `json:"server_port"`
	AuthEnabled     bool   `json:"auth_enabled"`
	ProxyEnabled    bool   `json:"proxy_enabled"`
	TrackingEnabled bool   `json:"tracking_enabled"`
	FailoverEnabled bool   `json:"failover_enabled"`
	// FailoverDefaultCooldownSeconds 全局默认冷却时间（秒），用于前端在端点未设置专属冷却时展示"实际生效值"。
	// 设置页面已移除此配置项（loop_retry 模式下冷却时间与循环重试逻辑冲突），使用固定默认值 600 秒。
	FailoverDefaultCooldownSeconds int `json:"failover_default_cooldown_seconds"`
	EndpointCount                  int `json:"endpoint_count"`
}

// GetConfig 获取当前配置（脱敏）
func (a *App) GetConfig() ConfigInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.config == nil {
		return ConfigInfo{}
	}

	return ConfigInfo{
		ServerHost:                     a.config.Server.Host,
		ServerPort:                     a.config.Server.Port,
		AuthEnabled:                    a.config.Auth.Enabled,
		ProxyEnabled:                   a.config.Proxy.Enabled,
		TrackingEnabled:                a.config.UsageTracking.Enabled,
		FailoverEnabled:                a.config.Failover.Enabled,
		FailoverDefaultCooldownSeconds: 600, // 固定默认值，设置页面已移除此配置项
		EndpointCount:                  len(a.config.Endpoints),
	}
}

// ============================================================
// 系统功能 API
// ============================================================

// GetProxyURL 获取代理 URL
func (a *App) GetProxyURL() string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.config == nil {
		return ""
	}

	// 统一使用 127.0.0.1，避免 localhost 在某些环境下的解析问题
	host := a.config.Server.Host
	if host == "" || host == "localhost" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", host, a.config.Server.Port)
}

// IsProxyRunning 检查代理是否运行中
func (a *App) IsProxyRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.isRunning
}

// GetSuspendedRequestsCount 获取当前挂起等待端点恢复的请求数量
// 用于前端渠道管理页面显示等待提示
func (a *App) GetSuspendedRequestsCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.proxyHandler != nil {
		return a.proxyHandler.GetSuspendedRequestsCount()
	}
	return 0
}

// GetSuspendedEndpointCounts 获取当前挂起请求的端点计数
func (a *App) GetSuspendedEndpointCounts() map[string]int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.proxyHandler != nil {
		return a.proxyHandler.GetSuspendedEndpointCounts()
	}
	return map[string]int{}
}

// GetManualRetryRequestsCount 获取等待用户操作的挂起请求数量
func (a *App) GetManualRetryRequestsCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.proxyHandler != nil {
		return a.proxyHandler.GetManualRetryRequestsCount()
	}
	return 0
}

// GetManualRetryChannelCounts 获取等待用户操作的渠道计数
func (a *App) GetManualRetryChannelCounts() map[string]int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.proxyHandler != nil {
		return a.proxyHandler.GetManualRetryChannelCounts()
	}
	return map[string]int{}
}

// ============================================================
// 辅助函数
// ============================================================

// formatDuration 格式化时长
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// parseTimeWithLocation 解析时间字符串（支持多种格式）
// 支持格式:
//   - 2025-12-05T00:00 (不带时区，使用 loc)
//   - 2025-12-05T00:00:00 (不带时区，使用 loc)
//   - 2025-12-05T00:00:00+08:00 (带时区)
//   - 2025-12-05 00:00:00 (简单格式，使用 loc)
//   - 2025-12-05 (仅日期，当天开始)
func parseTimeWithLocation(s string, loc *time.Location) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time string")
	}

	// 尝试各种格式
	formats := []struct {
		layout string
		useLoc bool // 是否使用 loc
	}{
		{"2006-01-02T15:04:05Z07:00", false}, // RFC3339
		{"2006-01-02T15:04:05+08:00", false}, // 带固定时区
		{"2006-01-02T15:04:05", true},        // 不带时区
		{"2006-01-02T15:04", true},           // 不带秒
		{"2006-01-02 15:04:05", true},        // 简单格式
		{"2006-01-02 15:04", true},           // 简单格式不带秒
		{"2006-01-02", true},                 // 仅日期
	}

	for _, f := range formats {
		var t time.Time
		var err error

		if f.useLoc && loc != nil {
			t, err = time.ParseInLocation(f.layout, s, loc)
		} else {
			t, err = time.Parse(f.layout, s)
		}

		if err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

// ============================================================
// 文件操作 API
// ============================================================

// SaveFileResult 文件保存结果
type SaveFileResult struct {
	Success  bool   `json:"success"`
	FilePath string `json:"file_path"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"` // 字节数
	Error    string `json:"error,omitempty"`
}

// SaveLogFile 保存日志到文件（使用原生文件对话框）
// content: 日志内容
// defaultFileName: 默认文件名
func (a *App) SaveLogFile(content string, defaultFileName string) SaveFileResult {
	if a.wailsApp == nil {
		return SaveFileResult{Success: false, Error: "应用未初始化"}
	}

	// 使用 Wails v3 原生保存文件对话框
	filePath, err := a.wailsApp.Dialog.SaveFile().
		SetMessage("保存日志文件").
		SetFilename(defaultFileName).
		AddFilter("文本文件", "*.txt").
		AddFilter("日志文件", "*.log").
		AddFilter("所有文件", "*.*").
		PromptForSingleSelection()

	if err != nil {
		return SaveFileResult{Success: false, Error: fmt.Sprintf("打开保存对话框失败: %v", err)}
	}

	// 用户取消了对话框
	if filePath == "" {
		return SaveFileResult{Success: false, Error: "cancelled"}
	}

	// 写入文件
	err = os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		return SaveFileResult{Success: false, Error: fmt.Sprintf("写入文件失败: %v", err)}
	}

	// 获取文件信息
	fileInfo, _ := os.Stat(filePath)
	var fileSize int64
	if fileInfo != nil {
		fileSize = fileInfo.Size()
	}

	return SaveFileResult{
		Success:  true,
		FilePath: filePath,
		FileName: filepath.Base(filePath),
		FileSize: fileSize,
	}
}
