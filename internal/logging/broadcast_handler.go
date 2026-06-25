package logging

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// LogEntry 表示一条结构化的日志记录
type LogEntry struct {
	Timestamp string            `json:"timestamp"` // ISO8601格式
	Level     string            `json:"level"`     // DEBUG/INFO/WARN/ERROR
	Message   string            `json:"message"`   // 日志消息
	Attrs     map[string]string `json:"attrs"`     // 附加属性（request_id等）
}

// BroadcastHandler 是一个slog.Handler，它会拦截所有日志并广播
// 1. 所有日志始终写入主文件（app.log）
// 2. 按 provider 属性额外写入对应文件（claude.log / codex.log）
// 3. 写入环形缓冲区（供查询历史）
// 4. 广播给订阅者（实时推送）
type BroadcastHandler struct {
	fileHandler   slog.Handler  // 默认文件Handler（app.log）
	claudeHandler slog.Handler  // Claude 代理日志Handler（claude.log）
	codexHandler  slog.Handler  // Codex 代理日志Handler（codex.log）
	debugHandler  slog.Handler  // DEBUG 级别专用日志Handler（debug.log）
	buffer        *RingBuffer   // 环形缓冲区（最近N条日志）
	Emitter       *EventEmitter // 事件发射器（Wails Events）- 导出用于设置
	minLevel      slog.Level    // 最低日志级别
	mu            sync.RWMutex
}

// NewBroadcastHandler 创建一个新的广播Handler
// fileHandler 为默认日志文件（app.log），claudeHandler/codexHandler 可为 nil（此时回退到 fileHandler）
func NewBroadcastHandler(fileHandler slog.Handler, bufferSize int, minLevel slog.Level, claudeHandler slog.Handler, codexHandler slog.Handler, debugHandler ...slog.Handler) *BroadcastHandler {
	var debug slog.Handler
	if len(debugHandler) > 0 {
		debug = debugHandler[0]
	}
	return &BroadcastHandler{
		fileHandler:   fileHandler,
		claudeHandler: claudeHandler,
		codexHandler:  codexHandler,
		debugHandler:  debug,
		buffer:        NewRingBuffer(bufferSize),
		Emitter:       NewEventEmitter(),
		minLevel:      minLevel,
	}
}

// SetEventEmitter 设置事件发射器（在Wails App启动后调用）
func (h *BroadcastHandler) SetEventEmitter(emitter *EventEmitter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Emitter = emitter
}

// Handle 实现 slog.Handler 接口
func (h *BroadcastHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < h.minLevel {
		return nil
	}

	// 1. 根据 provider 属性路由到对应文件
	routed := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "provider" {
			switch a.Value.String() {
			case "claude":
				if h.claudeHandler != nil {
					_ = h.claudeHandler.Handle(ctx, r)
					routed = true
				}
			case "codex":
				if h.codexHandler != nil {
					_ = h.codexHandler.Handle(ctx, r)
					routed = true
				}
			}
			return false
		}
		return true
	})

	// 2. 无 provider 属性的系统日志写入主文件（app.log）
	if !routed && h.fileHandler != nil {
		if err := h.fileHandler.Handle(ctx, r); err != nil {
			return err
		}
	}

	// 3. DEBUG 级别额外写入专用文件，便于排查时单独查看
	if r.Level == slog.LevelDebug && h.debugHandler != nil {
		_ = h.debugHandler.Handle(ctx, r)
	}

	// 4. 构建 LogEntry
	entry := h.buildLogEntry(r)

	// 5. 写入环形缓冲区
	h.buffer.Add(entry)

	// 6. 广播给订阅者（Wails Events）
	h.mu.RLock()
	emitter := h.Emitter
	h.mu.RUnlock()

	if emitter != nil {
		emitter.Emit(entry)
	}

	return nil
}

// Enabled 实现 slog.Handler 接口
func (h *BroadcastHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

// WithAttrs 实现 slog.Handler 接口
func (h *BroadcastHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandler := &BroadcastHandler{
		buffer:   h.buffer,
		Emitter:  h.Emitter,
		minLevel: h.minLevel,
	}
	if h.fileHandler != nil {
		newHandler.fileHandler = h.fileHandler.WithAttrs(attrs)
	}
	if h.claudeHandler != nil {
		newHandler.claudeHandler = h.claudeHandler.WithAttrs(attrs)
	}
	if h.codexHandler != nil {
		newHandler.codexHandler = h.codexHandler.WithAttrs(attrs)
	}
	if h.debugHandler != nil {
		newHandler.debugHandler = h.debugHandler.WithAttrs(attrs)
	}
	return newHandler
}

// WithGroup 实现 slog.Handler 接口
func (h *BroadcastHandler) WithGroup(name string) slog.Handler {
	newHandler := &BroadcastHandler{
		buffer:   h.buffer,
		Emitter:  h.Emitter,
		minLevel: h.minLevel,
	}
	if h.fileHandler != nil {
		newHandler.fileHandler = h.fileHandler.WithGroup(name)
	}
	if h.claudeHandler != nil {
		newHandler.claudeHandler = h.claudeHandler.WithGroup(name)
	}
	if h.codexHandler != nil {
		newHandler.codexHandler = h.codexHandler.WithGroup(name)
	}
	if h.debugHandler != nil {
		newHandler.debugHandler = h.debugHandler.WithGroup(name)
	}
	return newHandler
}

// GetRecentLogs 获取最近的N条日志
func (h *BroadcastHandler) GetRecentLogs(limit int) []LogEntry {
	return h.buffer.GetRecent(limit)
}

// buildLogEntry 从 slog.Record 构建 LogEntry
func (h *BroadcastHandler) buildLogEntry(r slog.Record) LogEntry {
	entry := LogEntry{
		Timestamp: r.Time.Format(time.RFC3339),
		Level:     r.Level.String(),
		Message:   r.Message,
		Attrs:     make(map[string]string),
	}

	// 提取所有属性
	r.Attrs(func(a slog.Attr) bool {
		entry.Attrs[a.Key] = a.Value.String()
		return true
	})

	return entry
}
