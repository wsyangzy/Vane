package tracking

import (
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// 日志去抖：相同 key 的日志在 throttleWindow 内只打一次
// 用于前端轮询触发的可预期 transient 错误（database is locked 等），
// 避免日志被刷屏掩盖真正的问题
//
// 🔧 [P0.5 修复] 增加过期清理机制，避免 sync.Map 无界增长
//   - 旧实现：只 LoadOrStore，从不 Delete，每个唯一 key 永久占用内存
//   - 风险：长时间运行后内存持续增长（每个独特错误消息 = 一个永久条目）
//   - 新实现：每次访问时 lazy 检查并清理过期条目（最后访问 > 5×throttleWindow 即清理）
var (
	logThrottle       sync.Map // key=string -> *int64 (unix nano)
	logThrottleWindow = int64(30 * time.Second)
	// 过期清理：超过 5 × throttleWindow（默认 150s）未再访问的条目可清理
	logThrottleExpiry     = int64(5 * 30 * time.Second)
	logThrottleSweepCount atomic.Int64
	logThrottleSweepEvery = int64(100) // 每 100 次 shouldLogThrottled 调用触发一次 sweep
)

// shouldLogThrottled 在 30s 内同 key 只返回 true 一次
func shouldLogThrottled(key string) bool {
	now := time.Now().UnixNano()
	val, _ := logThrottle.LoadOrStore(key, new(int64))
	last := val.(*int64)
	prev := atomic.LoadInt64(last)

	// 🔧 [P0.5 修复] 周期性触发过期清理（避免每次调用都遍历）
	if logThrottleSweepCount.Add(1)%logThrottleSweepEvery == 0 {
		go sweepLogThrottle(now)
	}

	if now-prev < logThrottleWindow {
		return false
	}
	return atomic.CompareAndSwapInt64(last, prev, now)
}

// sweepLogThrottle 清理超过 logThrottleExpiry 未访问的过期条目
// 🔧 [P0.5 新增] 在后台 goroutine 中执行，避免阻塞主调用路径
func sweepLogThrottle(now int64) {
	logThrottle.Range(func(k, v any) bool {
		if last, ok := v.(*int64); ok {
			if now-atomic.LoadInt64(last) > logThrottleExpiry {
				logThrottle.Delete(k)
			}
		}
		return true
	})
}
