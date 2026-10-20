package app

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	appcache "github.com/wsyangzy/Vane/internal/platform/cache"
)

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
