// globals.go - 全局变量和共享函数
// 不带 build tag，供 app.go 等文件引用
// 与 main.go 分离，确保 IDE 在无 desktop tag 时也能正确解析

package main

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"claude-request-forwarder/config"
	"claude-request-forwarder/internal/logging"
)

// 版本信息（通过 ldflags 注入）
var (
	Version   = "6.4"
	Commit    = "unknown"
	BuildTime = "unknown"
	BuildMode = "dev" // 构建模式: "dev" 或 "release"（生产构建通过 ldflags 注入 "release"）
)

// 嵌入默认配置文件
//
//go:embed config/config.yaml
var defaultConfigContent []byte

// 嵌入前端资源
//
//go:embed all:frontend/dist
var assets embed.FS

// 嵌入应用图标
//
//go:embed build/appicon.png
var icon []byte

// 嵌入托盘图标
//
//go:embed build/trayicon.png
var trayIcon []byte

// currentLogHandler 当前日志处理器引用（用于关闭）
var currentLogHandler *SimpleHandler

// ============================================================
// 日志相关
// ============================================================

// setupLogger 配置结构化日志
func setupLogger(cfg config.LoggingConfig) (*slog.Logger, *logging.BroadcastHandler) {
	logLevel := cfg.Level

	// 生产构建时，自动将 debug 级别提升为 info（减少日志量）
	if BuildMode == "release" && logLevel == "debug" {
		logLevel = "info"
		fmt.Println("🔧 [Release Mode] 日志级别自动调整: debug → info")
	}

	var level slog.Level
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	var fileRotator *logging.FileRotator
	// 设置文件日志
	if cfg.FileEnabled {
		maxSize, err := logging.ParseSize(cfg.MaxFileSize)
		if err != nil {
			fmt.Printf("警告：无法解析日志文件大小配置 '%s'，使用默认值 100MB: %v\n", cfg.MaxFileSize, err)
			maxSize = 100 * 1024 * 1024
		}

		fileRotator, err = logging.NewFileRotator(cfg.FilePath, maxSize, cfg.MaxFiles, cfg.CompressRotated)
		if err != nil {
			fmt.Printf("警告：无法创建日志文件轮转器: %v\n", err)
			fileRotator = nil
		}
	}

	// 创建 SimpleHandler（文件和控制台输出）
	simpleHandler := &SimpleHandler{
		level:                    level,
		fileRotator:              fileRotator,
		disableFileResponseLimit: cfg.FileEnabled && cfg.DisableResponseLimit,
	}
	currentLogHandler = simpleHandler

	// 用 BroadcastHandler 包装（添加日志查看功能）
	broadcastHandler := logging.NewBroadcastHandler(simpleHandler, 1000)

	if cfg.FileEnabled {
		fmt.Printf("🔧 文件日志已启用: 路径=%s\n", cfg.FilePath)
	}

	return slog.New(broadcastHandler), broadcastHandler
}

// SimpleHandler 简化的日志处理器
type SimpleHandler struct {
	level                    slog.Level
	fileRotator              *logging.FileRotator
	disableFileResponseLimit bool
}

func (h *SimpleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *SimpleHandler) Handle(_ context.Context, r slog.Record) error {
	message := r.Message

	var attrs []string
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, fmt.Sprintf("%s=%v", a.Key, a.Value))
		return true
	})

	if len(attrs) > 0 {
		message = message + " " + strings.Join(attrs, " ")
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	pid := os.Getpid()
	gid := getGoroutineID()
	level := "INFO"
	switch r.Level {
	case slog.LevelDebug:
		level = "DEBUG"
	case slog.LevelWarn:
		level = "WARN"
	case slog.LevelError:
		level = "ERROR"
	}

	// 文件输出
	if h.fileRotator != nil {
		fileMessage := message
		if !h.disableFileResponseLimit && len(message) > 500 {
			fileMessage = message[:500] + "... (文件日志截断)"
		}
		formattedMessage := fmt.Sprintf("[%s] [PID:%d] [GID:%d] [%s] %s\n", timestamp, pid, gid, level, fileMessage)
		h.fileRotator.Write([]byte(formattedMessage))
	}

	// 控制台输出
	displayMessage := message
	if len(displayMessage) > 500 {
		displayMessage = displayMessage[:500] + "... (显示截断)"
	}

	consoleMessage := fmt.Sprintf("[%s] [PID:%d] [GID:%d] [%s] %s", timestamp, pid, gid, level, displayMessage)
	fmt.Println(consoleMessage)

	return nil
}

func (h *SimpleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *SimpleHandler) WithGroup(name string) slog.Handler {
	return h
}

func (h *SimpleHandler) Close() error {
	if h.fileRotator != nil {
		h.fileRotator.Sync()
		return h.fileRotator.Close()
	}
	return nil
}

func getGoroutineID() int {
	buf := make([]byte, 64)
	buf = buf[:runtime.Stack(buf, false)]
	idField := strings.Fields(string(buf))[1]
	id, err := strconv.Atoi(idField)
	if err != nil {
		return 0
	}
	return id
}
