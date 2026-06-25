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
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/logging"
)

// 版本信息（通过 ldflags 注入）
var (
	Version   = "7.5"
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
var currentLogHandlers []*SimpleHandler

func datedLogPath(basePath string, fixedBaseName string) string {
	ext := filepath.Ext(basePath)
	if ext == "" {
		ext = ".log"
	}
	name := strings.TrimSuffix(filepath.Base(basePath), filepath.Ext(basePath))
	if fixedBaseName != "" {
		name = fixedBaseName
	}
	if name == "" || name == "." {
		name = "app"
	}
	return filepath.Join(filepath.Dir(basePath), fmt.Sprintf("%s-%s%s", name, time.Now().Format("2006-01-02"), ext))
}

// ============================================================
// 日志相关
// ============================================================

// setupLogger 配置结构化日志
// 按 provider 分文件存储：app.log（默认）、claude.log、codex.log
func setupLogger(cfg config.LoggingConfig) (*slog.Logger, *logging.BroadcastHandler) {
	logLevel := cfg.Level

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

	// 关闭旧的 handler（如果存在）
	for _, h := range currentLogHandlers {
		if h != nil {
			h.Close()
		}
	}
	currentLogHandlers = nil

	var appRotator, claudeRotator, codexRotator, debugRotator *logging.FileRotator
	// 设置文件日志
	if cfg.FileEnabled {
		maxSize, err := logging.ParseSize(cfg.MaxFileSize)
		if err != nil {
			fmt.Printf("警告：无法解析日志文件大小配置 '%s'，使用默认值 100MB: %v\n", cfg.MaxFileSize, err)
			maxSize = 100 * 1024 * 1024
		}

		// 主日志文件（app.log / app-dev.log）
		appLogFile := datedLogPath(cfg.FilePath, "")
		appRotator, err = logging.NewFileRotator(appLogFile, maxSize, cfg.MaxFiles, cfg.CompressRotated)
		if err != nil {
			fmt.Printf("警告：无法创建日志文件轮转器: %v\n", err)
			appRotator = nil
		}

		// Claude 代理日志文件（与主日志同目录）
		claudeLogFile := datedLogPath(cfg.FilePath, "claude")
		claudeRotator, err = logging.NewFileRotator(claudeLogFile, maxSize, cfg.MaxFiles, cfg.CompressRotated)
		if err != nil {
			fmt.Printf("警告：无法创建 Claude 日志文件轮转器: %v\n", err)
			claudeRotator = nil
		}

		// Codex 代理日志文件
		codexLogFile := datedLogPath(cfg.FilePath, "codex")
		codexRotator, err = logging.NewFileRotator(codexLogFile, maxSize, cfg.MaxFiles, cfg.CompressRotated)
		if err != nil {
			fmt.Printf("警告：无法创建 Codex 日志文件轮转器: %v\n", err)
			codexRotator = nil
		}

		if level <= slog.LevelDebug {
			// DEBUG 专用日志文件，保存所有 DEBUG 记录，便于排查时单独查看
			debugLogFile := datedLogPath(cfg.FilePath, "debug")
			debugRotator, err = logging.NewFileRotator(debugLogFile, maxSize, cfg.MaxFiles, cfg.CompressRotated)
			if err != nil {
				fmt.Printf("警告：无法创建 DEBUG 日志文件轮转器: %v\n", err)
				debugRotator = nil
			}
		}
	}

	disableLimit := cfg.FileEnabled && cfg.DisableResponseLimit

	// 创建三个 SimpleHandler
	appHandler := &SimpleHandler{
		level:                    level,
		fileRotator:              appRotator,
		disableFileResponseLimit: disableLimit,
	}
	claudeHandler := &SimpleHandler{
		level:                    level,
		fileRotator:              claudeRotator,
		disableFileResponseLimit: disableLimit,
	}
	codexHandler := &SimpleHandler{
		level:                    level,
		fileRotator:              codexRotator,
		disableFileResponseLimit: disableLimit,
	}
	var debugHandler *SimpleHandler
	if debugRotator != nil {
		debugHandler = &SimpleHandler{
			level:                    slog.LevelDebug,
			fileRotator:              debugRotator,
			disableFileResponseLimit: true,
			suppressConsole:          true,
		}
	}
	currentLogHandlers = []*SimpleHandler{appHandler, claudeHandler, codexHandler}
	if debugHandler != nil {
		currentLogHandlers = append(currentLogHandlers, debugHandler)
	}

	// 用 BroadcastHandler 包装（添加日志查看功能 + provider 路由）
	var broadcastHandler *logging.BroadcastHandler
	if debugHandler != nil {
		broadcastHandler = logging.NewBroadcastHandler(appHandler, 1000, level, claudeHandler, codexHandler, debugHandler)
	} else {
		broadcastHandler = logging.NewBroadcastHandler(appHandler, 1000, level, claudeHandler, codexHandler)
	}

	if cfg.FileEnabled {
		if debugHandler != nil {
			fmt.Printf("🔧 文件日志已启用: 路径=%s (分文件: claude/codex/debug，按日期命名)\n", cfg.FilePath)
		} else {
			fmt.Printf("🔧 文件日志已启用: 路径=%s (分文件: claude/codex，按日期命名)\n", cfg.FilePath)
		}
	}

	return slog.New(broadcastHandler), broadcastHandler
}

// SimpleHandler 简化的日志处理器
type SimpleHandler struct {
	level                    slog.Level
	fileRotator              *logging.FileRotator
	disableFileResponseLimit bool
	suppressConsole          bool
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

	if !h.suppressConsole {
		// 控制台输出
		displayMessage := message
		if len(displayMessage) > 500 {
			displayMessage = displayMessage[:500] + "... (显示截断)"
		}

		consoleMessage := fmt.Sprintf("[%s] [PID:%d] [GID:%d] [%s] %s", timestamp, pid, gid, level, displayMessage)
		fmt.Println(consoleMessage)
	}

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
