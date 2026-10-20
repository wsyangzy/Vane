// Package utils 提供通用工具函数
// 端口探测工具 - 新增 (2025-12-08)
package netutil

import (
	"fmt"
	"log/slog"
	"net"
	"sync"
)

// PortInfo 端口信息
type PortInfo struct {
	PreferredPort int  `json:"preferred_port"` // 用户设置的首选端口
	ActualPort    int  `json:"actual_port"`    // 实际使用的端口
	IsDefault     bool `json:"is_default"`     // 是否使用默认端口
	WasOccupied   bool `json:"was_occupied"`   // 首选端口是否被占用
}

// PortManager 端口管理器
type PortManager struct {
	mu            sync.RWMutex
	preferredPort int
	actualPort    int
	wasOccupied   bool
}

// NewPortManager 创建端口管理器
func NewPortManager(preferredPort int) *PortManager {
	return &PortManager{
		preferredPort: preferredPort,
	}
}

// FindAvailablePort 从首选端口开始，查找可用端口
// maxAttempts: 最多尝试次数（从首选端口开始递增）
// 返回可用端口号，如果找不到返回错误
func FindAvailablePort(preferred int, maxAttempts int) (int, error) {
	if maxAttempts <= 0 {
		maxAttempts = 10
	}

	for i := 0; i < maxAttempts; i++ {
		port := preferred + i
		if IsPortAvailable(port) {
			if i > 0 {
				slog.Warn(fmt.Sprintf("⚠️ 端口 %d 被占用，自动使用端口 %d", preferred, port))
			}
			return port, nil
		}
	}

	return 0, fmt.Errorf("无法找到可用端口 (尝试范围: %d-%d)", preferred, preferred+maxAttempts-1)
}

// IsPortAvailable 检查端口是否可用
func IsPortAvailable(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}

// FindAndBind 查找可用端口并绑定
// 返回 listener 和实际端口号
func FindAndBind(preferred int, maxAttempts int) (net.Listener, int, error) {
	return FindAndBindWithHost("127.0.0.1", preferred, maxAttempts)
}

// FindAndBindWithHost 查找可用端口并绑定到指定 host
// 返回 listener 和实际端口号
func FindAndBindWithHost(host string, preferred int, maxAttempts int) (net.Listener, int, error) {
	if maxAttempts <= 0 {
		maxAttempts = 10
	}

	for i := 0; i < maxAttempts; i++ {
		port := preferred + i
		addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			if i > 0 {
				slog.Warn(fmt.Sprintf("⚠️ 端口 %d 被占用，自动使用端口 %d", preferred, port))
			}
			return listener, port, nil
		}
	}

	// 首选区间全部被占用时，回退到系统分配的随机可用端口
	fallbackAddr := net.JoinHostPort(host, "0")
	fallbackListener, fallbackErr := net.Listen("tcp", fallbackAddr)
	if fallbackErr == nil {
		tcpAddr, ok := fallbackListener.Addr().(*net.TCPAddr)
		if ok && tcpAddr.Port > 0 {
			slog.Warn(fmt.Sprintf("⚠️ 端口 %d-%d 均不可用，自动回退到随机端口 %d", preferred, preferred+maxAttempts-1, tcpAddr.Port))
			return fallbackListener, tcpAddr.Port, nil
		}
		_ = fallbackListener.Close()
		fallbackErr = fmt.Errorf("无法解析随机端口地址: %v", fallbackListener.Addr())
	}

	return nil, 0, fmt.Errorf("无法找到可用端口 (尝试范围: %d-%d)，随机端口回退失败: %w", preferred, preferred+maxAttempts-1, fallbackErr)
}

// SetPreferredPort 设置首选端口
func (pm *PortManager) SetPreferredPort(port int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.preferredPort = port
}

// SetActualPort 设置实际使用的端口
func (pm *PortManager) SetActualPort(port int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.actualPort = port
	pm.wasOccupied = port != pm.preferredPort
}

// GetPreferredPort 获取首选端口
func (pm *PortManager) GetPreferredPort() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.preferredPort
}

// GetActualPort 获取实际端口
func (pm *PortManager) GetActualPort() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.actualPort
}

// GetPortInfo 获取完整端口信息
func (pm *PortManager) GetPortInfo() PortInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return PortInfo{
		PreferredPort: pm.preferredPort,
		ActualPort:    pm.actualPort,
		IsDefault:     pm.preferredPort == 8087, // 默认端口
		WasOccupied:   pm.wasOccupied,
	}
}

// FindAndSetPort 查找可用端口并设置
func (pm *PortManager) FindAndSetPort(maxAttempts int) (int, error) {
	port, err := FindAvailablePort(pm.preferredPort, maxAttempts)
	if err != nil {
		return 0, err
	}
	pm.SetActualPort(port)
	return port, nil
}
