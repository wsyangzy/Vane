package tracking

import (
	"log/slog"
	"time"

	_ "modernc.org/sqlite"
)

// HotPoolAlertCallback 热池满告警回调
// 🔧 [P5.8 新增] 由外部（app.go）注册，将告警事件推送到前端 Toast
type HotPoolAlertCallback func(reason string, droppedCount int64)

// SetHotPoolAlertCallback 注册热池告警回调
// 🔧 [P5.8 新增] 由 app.go 在启动时调用
func (ut *UsageTracker) SetHotPoolAlertCallback(cb HotPoolAlertCallback) {
	ut.hotPoolAlertCb.Store(&cb)
}

// hotPoolAlertDebounce 告警防抖：避免短时间内重复告警
// 🔧 [P5.8 新增]
func (ut *UsageTracker) hotPoolAlertDebounce(reason string) bool {
	now := time.Now().UnixNano()
	last := ut.hotPoolAlertLast.Load()
	// 60s 内同一原因只告警一次
	if now-last < int64(60*time.Second) {
		return false
	}
	return ut.hotPoolAlertLast.CompareAndSwap(last, now)
}

// triggerHotPoolAlert 触发热池告警（带防抖）
// 🔧 [P5.8 新增]
func (ut *UsageTracker) triggerHotPoolAlert(reason string) {
	dropped := ut.hotPoolDroppedTotal.Add(1)

	if !ut.hotPoolAlertDebounce(reason) {
		return
	}

	cbPtr := ut.hotPoolAlertCb.Load()
	if cbPtr != nil && *cbPtr != nil {
		(*cbPtr)(reason, dropped)
	}

	slog.Warn("🚨 [热池告警] 数据写入压力大",
		"reason", reason,
		"total_dropped", dropped,
		"hint", "如频繁出现，请增加 hot_pool.max_size 或检查数据库性能")
}

// GetHotPoolDroppedCount 返回累计丢弃的请求数（供前端查询）
// 🔧 [P5.8 新增 - 暴露给前端]
func (ut *UsageTracker) GetHotPoolDroppedCount() int64 {
	return ut.hotPoolDroppedTotal.Load()
}
