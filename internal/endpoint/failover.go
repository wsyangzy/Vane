// failover.go - 故障转移相关功能
// v6.2: 冷却机制已移除，失败渠道通过手动"重试"按钮重新激活

package endpoint

import (
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"
)

type groupCandidate struct {
	name      string
	priority  int
	bestRT    time.Duration
	hasBestRT bool
}

func (m *Manager) collectFailoverCandidates(excludeChannel string) []groupCandidate {
	exclude := make(map[string]struct{}, 1)
	if excludeChannel != "" {
		exclude[excludeChannel] = struct{}{}
	}
	return m.collectFailoverCandidatesExcluding(exclude)
}

// collectFailoverCandidatesExcluding 收集故障转移候选渠道
// exclude: 要排除的渠道名称集合
// v6.2: 冷却机制已移除
func (m *Manager) collectFailoverCandidatesExcluding(exclude map[string]struct{}) []groupCandidate {
	// 说明：
	// - 可用性判定不依赖"响应时间快慢"，而依赖：未暂停、渠道内至少存在一个可用端点
	// - "fastest" 仅用于在候选渠道之间做排序（按渠道内可用端点的最小响应时间），不会因为慢就判定不可用
	candidates := make([]groupCandidate, 0, 8)

	for _, g := range m.groupManager.GetAllGroups() {
		if g == nil || g.Name == "" {
			continue
		}
		if _, ok := exclude[g.Name]; ok {
			continue
		}
		if g.ManuallyPaused {
			continue
		}

		// 渠道内至少有一个"可用端点"才视为可切换（与渠道内路由候选保持一致）
		hasAvailableEndpoint := false

		bestRT := time.Duration(math.MaxInt64)
		hasBestRT := false
		for _, ep := range g.Endpoints {
			if ep == nil {
				continue
			}

			failoverEnabled := true
			if ep.Config.FailoverEnabled != nil {
				failoverEnabled = *ep.Config.FailoverEnabled
			}
			if !failoverEnabled {
				continue
			}

			ep.mutex.RLock()
			isHealthy := ep.Status.Healthy
			neverChecked := ep.Status.NeverChecked
			rt := ep.Status.ResponseTime
			ep.mutex.RUnlock()

			if !(isHealthy || neverChecked) {
				continue
			}

			hasAvailableEndpoint = true

			// fastest 策略的排序依据：优先使用"已测得"的响应时间；未测得则不参与 bestRT
			if rt > 0 {
				hasBestRT = true
				if rt < bestRT {
					bestRT = rt
				}
			}
		}

		if !hasAvailableEndpoint {
			continue
		}

		candidates = append(candidates, groupCandidate{
			name:      g.Name,
			priority:  g.Priority,
			bestRT:    bestRT,
			hasBestRT: hasBestRT,
		})
	}

	return candidates
}

func sortFailoverCandidates(strategy string, candidates []groupCandidate) {
	switch strategy {
	case "fastest":
		sort.Slice(candidates, func(i, j int) bool {
			// 有响应时间的优先于“未测得”的（避免完全随机）
			if candidates[i].hasBestRT != candidates[j].hasBestRT {
				return candidates[i].hasBestRT
			}
			if candidates[i].hasBestRT && candidates[j].hasBestRT && candidates[i].bestRT != candidates[j].bestRT {
				return candidates[i].bestRT < candidates[j].bestRT
			}
			// 回退：优先级越小越优先，其次名字稳定排序
			if candidates[i].priority != candidates[j].priority {
				return candidates[i].priority < candidates[j].priority
			}
			return candidates[i].name < candidates[j].name
		})
	default:
		// priority（或未知策略）：
		// 以渠道优先级（GroupInfo.Priority，v6.1+ 为 channel.priority）为主，保持稳定
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].priority != candidates[j].priority {
				return candidates[i].priority < candidates[j].priority
			}
			return candidates[i].name < candidates[j].name
		})
	}
}

// SelectNextAvailableChannel 选择下一个可用渠道（用于手动停用活跃渠道或请求级故障转移）。
// excludeChannel: 要排除的渠道（例如当前活跃/失败渠道）。
func (m *Manager) SelectNextAvailableChannel(excludeChannel string) (string, error) {
	if m == nil || m.groupManager == nil || m.config == nil {
		return "", fmt.Errorf("配置未初始化")
	}

	candidates := m.collectFailoverCandidates(excludeChannel)
	if len(candidates) == 0 {
		return "", fmt.Errorf("没有可用的故障转移渠道")
	}

	// 方案B：渠道间故障转移顺序固定按优先级（避免 fastest 带来的“回切/抖动”）
	sortFailoverCandidates("priority", candidates)

	return candidates[0].name, nil
}

// SelectNextAvailableChannelExcluding 选择下一个可用渠道（排除多个渠道）。
// 方案B：用于请求链路内的"渠道间故障转移"，避免冷却结束后回到已故障转移过的渠道造成循环。
// 向后兼容：不检查循环次数限制，使用 SelectNextAvailableChannelExcludingWithLoopCount 进行完整限制检查。
func (m *Manager) SelectNextAvailableChannelExcluding(excludeChannels []string) (string, error) {
	return m.SelectNextAvailableChannelExcludingWithLoopCount(excludeChannels, 0)
}

// SelectNextAvailableChannelExcludingWithLoopCount 选择下一个可用渠道（带循环计数限制）。
// loopCount: 当前已完成的循环轮数（从 0 开始）。当所有渠道耗尽时，如果启用循环重试且未达到 MaxLoops，将开始新一轮。
// 返回值: (新渠道名, 是否开始新循环, 错误)
func (m *Manager) SelectNextAvailableChannelExcludingWithLoopCount(excludeChannels []string, loopCount int) (string, error) {
	if m == nil || m.groupManager == nil || m.config == nil {
		return "", fmt.Errorf("配置未初始化")
	}

	// v6.2.7: 在函数开头检查 loopCount 限制，防止绕过
	// 当启用循环重试且 loopCount > 0 时，说明已经进入循环重试模式
	if m.config.FailoverV2.ShouldLoopRetryOnExhausted() && loopCount > 0 {
		maxLoops := m.config.FailoverV2.GetMaxLoops()
		if maxLoops > 0 && loopCount >= maxLoops {
			slog.Warn(fmt.Sprintf("🔚 [循环限制] 已达到最大循环轮数 (%d/%d)，停止重试", loopCount, maxLoops))
			return "", fmt.Errorf("服务暂时不可用: 所有渠道均已尝试 (共循环 %d 轮), 请稍后重试", maxLoops)
		}
	}

	exclude := make(map[string]struct{}, len(excludeChannels))
	for _, name := range excludeChannels {
		if name == "" {
			continue
		}
		exclude[name] = struct{}{}
	}

	candidates := m.collectFailoverCandidatesExcluding(exclude)
	if len(candidates) == 0 {
		// v6.2: 使用 LoopRetry 配置替代原有的 RecoveryStrategy 判断
		if m.config.FailoverV2.ShouldLoopRetryOnExhausted() {
			// 检查循环次数限制
			maxLoops := m.config.FailoverV2.GetMaxLoops()
			if maxLoops > 0 && loopCount >= maxLoops {
				slog.Warn(fmt.Sprintf("🔚 [循环限制] 已达到最大循环轮数 (%d/%d)，停止重试", loopCount, maxLoops))
				return "", fmt.Errorf("服务暂时不可用: 所有渠道均已尝试 (共循环 %d 轮), 请稍后重试", maxLoops)
			}

			// 循环重试策略：当"所有渠道都尝试过"时，允许回到最高优先级渠道继续尝试
			// v6.2.7: 循环重试时，重置所有端点健康状态，允许重新选择
			m.resetAllEndpointsHealthForLoopRetry()
			candidates = m.collectFailoverCandidatesExcluding(nil)
			if len(candidates) == 0 {
				return "", fmt.Errorf("没有可用的故障转移渠道（无活跃端点）")
			}
			sortFailoverCandidates("priority", candidates)
			slog.Warn(fmt.Sprintf("🔄 [循环重试] 已尝试所有渠道，开始第 %d 轮重试，回到最高优先级渠道: %s", loopCount+1, candidates[0].name))
			return candidates[0].name, nil
		}
		if len(exclude) > 0 {
			// 快速失败策略：不允许循环重试
			slog.Warn(fmt.Sprintf("⏸️ [快速失败] 已尝试所有渠道（%d个），停止并提示用户", len(exclude)))
			return "", fmt.Errorf("没有可用的故障转移渠道（已尝试渠道数=%d）", len(exclude))
		}
		return "", fmt.Errorf("没有可用的故障转移渠道")
	}

	// 方案B：渠道间故障转移顺序固定按优先级
	sortFailoverCandidates("priority", candidates)
	return candidates[0].name, nil
}

// SetOnFailoverTriggered 设置故障转移回调
// 当请求失败触发“跨渠道”故障转移时调用，用于同步数据库
func (m *Manager) SetOnFailoverTriggered(fn func(failedChannel, newChannel string)) {
	m.onFailoverTriggered = fn
}

// TriggerRequestFailoverWithFailedEndpoints 触发请求级故障转移（跨渠道切换）。
//
// 语义：
// - 由上层在“当前渠道内所有端点都已尝试且重试耗尽”时调用
// - 会将本次请求中失败过的端点统一进入冷却，避免下一次请求立即重复撞同一批端点
// - 然后将失败渠道置为冷却，并切换到下一个可用渠道
//
// 返回: 新激活的渠道名，如果没有可用渠道则返回空字符串
func (m *Manager) TriggerRequestFailoverWithFailedEndpoints(failedEndpointNames []string, reason string) (string, error) {
	return m.TriggerRequestFailoverWithFailedEndpointsExcludingChannels(failedEndpointNames, nil, reason)
}

// TriggerRequestFailoverWithFailedEndpointsExcludingChannels 触发请求级故障转移（跨渠道切换）。
// excludeChannels: 本次请求链路中已尝试过的渠道（方案B：不回访，避免循环）。
// 向后兼容：不检查循环次数限制。
func (m *Manager) TriggerRequestFailoverWithFailedEndpointsExcludingChannels(failedEndpointNames []string, excludeChannels []string, reason string) (string, error) {
	return m.TriggerRequestFailoverWithLoopCount(failedEndpointNames, excludeChannels, reason, 0)
}

// TriggerRequestFailoverWithLoopCount 触发请求级故障转移（跨渠道切换，带循环计数限制）。
// excludeChannels: 本次请求链路中已尝试过的渠道（方案B：不回访，避免循环）。
// loopCount: 当前已完成的循环轮数（从 0 开始）。
// v6.2: 冷却机制已移除，失败渠道通过手动"重试"按钮重新激活
func (m *Manager) TriggerRequestFailoverWithLoopCount(failedEndpointNames []string, excludeChannels []string, reason string, loopCount int) (string, error) {
	slog.Warn(fmt.Sprintf("🔄 [渠道间故障转移] 触发渠道间故障转移，原因: %s", reason))

	// 未启用故障转移时不进行跨渠道切换（保持配置语义）
	if m.config == nil {
		return "", fmt.Errorf("配置未初始化")
	}
	if !m.config.Failover.Enabled {
		return "", fmt.Errorf("故障转移未启用")
	}

	// 1) 去重 + 找到失败渠道
	uniqueNames := make([]string, 0, len(failedEndpointNames))
	seen := make(map[string]struct{}, len(failedEndpointNames))
	for _, name := range failedEndpointNames {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		uniqueNames = append(uniqueNames, name)
	}
	if len(uniqueNames) == 0 {
		return "", fmt.Errorf("缺少失败端点信息")
	}

	var failedChannel string
	for _, name := range uniqueNames {
		ep := m.GetEndpointByNameAny(name)
		if ep == nil {
			continue
		}
		failedChannel = ChannelKey(ep)
		break
	}
	if failedChannel == "" {
		return "", fmt.Errorf("无法解析失败渠道（端点不存在或未初始化）")
	}

	// 方案B：确保失败渠道被纳入"已尝试渠道"集合（不回访）
	if failedChannel != "" {
		found := false
		for _, name := range excludeChannels {
			if name == failedChannel {
				found = true
				break
			}
		}
		if !found {
			excludeChannels = append(excludeChannels, failedChannel)
		}
	}

	// 2) v6.2: 冷却机制已移除，标记失败端点为不健康
	// v6.2.6: 同时设置 NeverChecked = false，防止 neverChecked 端点被误认为"可用"而导致无限循环
	// v6.2.9: 合并日志输出，避免循环重试时日志膨胀
	var markedEndpoints []string
	for _, name := range uniqueNames {
		ep := m.GetEndpointByNameAny(name)
		if ep == nil {
			continue
		}
		ep.mutex.Lock()
		ep.Status.Healthy = false
		ep.Status.NeverChecked = false // 关键修复：防止 neverChecked 端点在故障转移候选中被误判为可用
		ep.mutex.Unlock()
		markedEndpoints = append(markedEndpoints, name)
	}
	if len(markedEndpoints) > 0 {
		slog.Info(fmt.Sprintf("❌ [渠道间故障转移] 端点标记为不健康: %v (共%d个)", markedEndpoints, len(markedEndpoints)))
	}

	// 3) 选择并激活下一个可用渠道（由 strategy + 可用性共同决定）
	// 方案B：不回访已尝试渠道；且渠道间切换顺序固定按优先级
	// v6.2: 使用带循环计数的方法，支持 MaxLoops 限制
	newChannel, err := m.SelectNextAvailableChannelExcludingWithLoopCount(excludeChannels, loopCount)
	if err != nil {
		slog.Error("❌ [渠道间故障转移] 没有可用的故障转移渠道")
		return "", err
	}

	if err := m.groupManager.ManualActivateGroup(newChannel); err != nil {
		// 兼容：新渠道端点可能处于 neverChecked（尚未健康检查），但请求级故障转移应允许先切过去尝试。
		// 这里回退到强制激活，避免因"尚未健康检查"而无法完成跨渠道切换。
		slog.Warn(fmt.Sprintf("⚠️ [渠道间故障转移] 常规激活新渠道失败，回退强制激活: %s, 错误: %v", newChannel, err))
		if err2 := m.groupManager.ManualActivateGroupWithForce(newChannel, true); err2 != nil {
			slog.Error(fmt.Sprintf("❌ [渠道间故障转移] 强制激活新渠道失败: %v", err2))
			return "", fmt.Errorf("激活新渠道失败: %w", err2)
		}
	}

	slog.Info(fmt.Sprintf("✅ [渠道间故障转移] 已切换到渠道: %s", newChannel))

	// 4) 调用回调通知 App 层同步数据库
	if m.onFailoverTriggered != nil {
		go m.onFailoverTriggered(failedChannel, newChannel)
	}

	// 5) 触发前端刷新
	if m.onHealthCheckComplete != nil {
		go m.onHealthCheckComplete()
	}

	return newChannel, nil
}

// TriggerRequestFailover 兼容旧签名：仅传入最后失败的端点。
func (m *Manager) TriggerRequestFailover(failedEndpointName string, reason string) (string, error) {
	return m.TriggerRequestFailoverWithFailedEndpoints([]string{failedEndpointName}, reason)
}

// v6.2: 以下冷却相关函数已移除
// - IsEndpointInCooldown
// - ClearEndpointCooldown
// - GetEndpointCooldownInfo
// 失败渠道通过手动"重试"按钮重新激活

// resetAllEndpointsHealthForLoopRetry 循环重试时重置所有端点健康状态
// v6.2.7: 循环重试策略下，允许对已失败的端点进行重新尝试
// v6.2.10: 移除智能重置逻辑，统一重置所有端点（用户可通过 UI 查看失败原因）
func (m *Manager) resetAllEndpointsHealthForLoopRetry() {
	if m == nil || m.groupManager == nil {
		return
	}

	resetCount := 0

	for _, g := range m.groupManager.GetAllGroups() {
		if g == nil || g.ManuallyPaused {
			continue
		}
		for _, ep := range g.Endpoints {
			if ep == nil {
				continue
			}

			// 检查 failover_enabled
			failoverEnabled := true
			if ep.Config.FailoverEnabled != nil {
				failoverEnabled = *ep.Config.FailoverEnabled
			}
			if !failoverEnabled {
				continue
			}

			ep.mutex.Lock()
			if !ep.Status.Healthy {
				ep.Status.Healthy = true
				ep.Status.NeverChecked = true // 标记为"未检查"，让选择逻辑认为它是可用的
				// 注意：不清除 LastRequestFailCode 和 LastRequestFailReason，保留给 UI 显示
				resetCount++
			}
			ep.mutex.Unlock()
		}
	}

	if resetCount > 0 {
		slog.Info(fmt.Sprintf("🔄 [循环重试] 已重置 %d 个端点健康状态，准备新一轮尝试", resetCount))
	}
}
