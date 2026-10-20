package host

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// webviewIdleReloadThreshold 为触发重新导航的隐藏时长下限。
// 低于此值认为渲染进程仍然存活，不打扰用户的页面状态；超过则宁可重载一次。
// 这是经验阈值，渲染进程实际被回收的时间取决于系统内存压力，可按需调整。
const webviewIdleReloadThreshold = 10 * time.Minute

// webviewIdleTracker 记录窗口进入隐藏态的时刻，判断恢复时是否需要重建页面。
// 事件回调由 Wails 在独立 goroutine 中派发（application.go 的 go a.handleWindowEvent），
// 故用原子操作而非裸字段。
type webviewIdleTracker struct {
	threshold time.Duration
	hiddenAt  atomic.Int64 // UnixNano；0 表示当前处于可见态
}

// markHidden 记录首次进入隐藏态的时刻。
// 隐藏到托盘与最小化可能连续派发多个事件，用 CAS 保证只记最早的一次。
func (t *webviewIdleTracker) markHidden(now time.Time) {
	t.hiddenAt.CompareAndSwap(0, now.UnixNano())
}

// shouldReload 取出并清零隐藏时刻，返回本次恢复是否需要重建页面。
// Swap 保证一次隐藏只会触发一次重建，即使恢复路径派发了多个事件。
func (t *webviewIdleTracker) shouldReload(now time.Time) bool {
	hiddenAt := t.hiddenAt.Swap(0)
	if hiddenAt == 0 {
		return false
	}
	return now.Sub(time.Unix(0, hiddenAt)) >= t.threshold
}

// registerWebviewRecovery 注册窗口隐藏/恢复钩子，在隐藏过久后强制重建页面。
//
// 用 SetURL 而非 Reload：Windows 平台的 reload() 实现是 execJS("window.location.reload()")，
// 依赖 JS 引擎存活，而白屏时失效的正是这一环；forceReload() 在 Windows 上更是空实现。
// SetURL 走 impl.setURL → chromium.Navigate 原生 COM 导航，可绕过已死的渲染进程重建页面，
// 并将 webviewNavigationCompleted 重置为 false，配合 show() 的可见性逻辑让 WebView 重新显示。
func registerWebviewRecovery(window *application.WebviewWindow) {
	if window == nil {
		return
	}
	tracker := &webviewIdleTracker{threshold: webviewIdleReloadThreshold}

	onHidden := func(*application.WindowEvent) {
		tracker.markHidden(time.Now())
	}
	onRestored := func(*application.WindowEvent) {
		if !tracker.shouldReload(time.Now()) {
			return
		}
		slog.Info("🔄 [WebView 自愈] 窗口隐藏超时，重新导航以重建可能已被回收的渲染进程",
			"threshold", webviewIdleReloadThreshold)
		window.SetURL("/")
	}

	// Windows 只派发 events.Windows.*，macOS/Linux 派发 events.Common.*，
	// 且 Wails 的 HandleWindowEvent 按事件数值直接查表、不做平台↔通用映射，
	// 因此两组常量都要注册，缺一侧即在该平台上成为死代码。
	for _, eventType := range []events.WindowEventType{
		events.Windows.WindowHide,
		events.Windows.WindowMinimise,
		events.Common.WindowHide,
		events.Common.WindowMinimise,
	} {
		window.RegisterHook(eventType, onHidden)
	}

	// 恢复信号用 WindowUnMinimise 而非 WindowRestore：后者在拖拽 resize 期间
	// 每帧都会随 SIZE_RESTORED 触发，而前者有 isMinimizing 门控，只在真正从最小化恢复时派发。
	for _, eventType := range []events.WindowEventType{
		events.Windows.WindowShow,
		events.Windows.WindowUnMinimise,
		events.Common.WindowShow,
		events.Common.WindowUnMinimise,
	} {
		window.RegisterHook(eventType, onRestored)
	}
}
