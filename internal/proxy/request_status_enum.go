package proxy

// 🔧 [P5.2 新增] 请求生命周期状态枚举常量
// 集中定义所有合法的 request status 值，避免散落在代码中的字符串字面量
//
// 命名约定：使用 ReqStatus 前缀避免与 token_parser.go 中的 StatusCompleted/StatusProcessing 冲突
// （token_parser 中的常量语义不同 - 那是 API 响应错误分类，这里是请求生命周期状态）
//
// 状态转移图：
//
//	pending → forwarding → streaming/processing → completed
//	                    ↘ retry → forwarding (重试) → ...
//	                    ↘ suspended → forwarding (恢复) → ...
//	任何状态 → failed
//	任何状态 → cancelled
const (
	// 终态以外的中间状态
	ReqStatusPending    = "pending"    // 请求已接收，未开始转发
	ReqStatusForwarding = "forwarding" // 正在转发到上游
	ReqStatusStreaming  = "streaming"  // 流式响应中
	ReqStatusProcessing = "processing" // 非流式响应处理中
	ReqStatusRetry      = "retry"      // 重试中（中间态）
	ReqStatusSuspended  = "suspended"  // 挂起中（等待端点恢复或渠道切换）

	// 终态
	ReqStatusCompleted = "completed" // 请求成功完成
	ReqStatusFailed    = "failed"    // 请求最终失败
	ReqStatusCancelled = "cancelled" // 客户端取消
)

// isTerminalReqStatus 判断是否为终态
// 🔧 [P5.2 新增]
func isTerminalReqStatus(status string) bool {
	switch status {
	case ReqStatusCompleted, ReqStatusFailed, ReqStatusCancelled:
		return true
	default:
		return false
	}
}

// isValidReqStatusTransition 检查状态转移是否合法
// 🔧 [P5.2 新增]
//
// 规则：
//  1. 任何状态都可以转到 failed/cancelled（错误处理路径）
//  2. 终态不能再变更
//  3. 中间态之间可以任意转换（具体业务逻辑控制）
//
// 注：当前仅作日志告警用，不阻断状态变更（避免破坏向后兼容）
func isValidReqStatusTransition(from, to string) bool {
	// 终态不能再变更
	if isTerminalReqStatus(from) {
		// 例外：cancelled → failed（取消后又被发现是失败）允许
		if from == ReqStatusCancelled && to == ReqStatusFailed {
			return true
		}
		// 例外：failed → cancelled（失败后用户手动取消）允许
		if from == ReqStatusFailed && to == ReqStatusCancelled {
			return true
		}
		// 其他终态变更非法
		return false
	}

	// 任何非终态都可以转到任何状态（中间态灵活变更）
	return true
}

// validReqStatusValues 返回所有合法的状态值（用于校验）
// 🔧 [P5.2 新增]
func validReqStatusValues() []string {
	return []string{
		ReqStatusPending, ReqStatusForwarding, ReqStatusStreaming, ReqStatusProcessing,
		ReqStatusRetry, ReqStatusSuspended,
		ReqStatusCompleted, ReqStatusFailed, ReqStatusCancelled,
	}
}

// isValidReqStatus 检查 status 字符串是否为合法枚举值
// 🔧 [P5.2 新增]
func isValidReqStatus(status string) bool {
	for _, valid := range validReqStatusValues() {
		if status == valid {
			return true
		}
	}
	return false
}
