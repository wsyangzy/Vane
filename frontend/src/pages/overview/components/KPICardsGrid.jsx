// ============================================
// KPI 卡片网格组件
// 一个端点 = 一个组
// 服务状态改为成本和 tokens 统计
// 含 Claude/Codex 分项悬浮提示
// ============================================

import { useState, useEffect, useCallback } from 'react'
import {
  DollarSign,
  Zap,
  Server,
  Network,
  Activity
} from 'lucide-react'
import { KPICard } from '@components/ui'
import {
  getUsageSummary,
  getEndpointHealthChart,
  getCodexEndpointHealthChart,
  getCodexChannels,
  isWailsEnvironment
} from '@utils/wailsApi.js'

// 格式化成本显示
const formatCost = (cost) => {
  if (!cost || cost === 0) return '$0.00'
  if (cost >= 1) return `$${cost.toFixed(2)}`
  if (cost >= 0.01) return `$${cost.toFixed(3)}`
  return `$${cost.toFixed(4)}`
}

// 格式化 tokens 显示
const formatTokens = (tokens) => {
  if (!tokens || tokens === 0) return '0'
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(2)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return tokens.toString()
}

const KPICardsGrid = ({ data }) => {
  const { endpoints, connections, groups } = data

  // Claude/Codex 分项统计
  const [providerStats, setProviderStats] = useState(null)
  // Claude/Codex 端点健康分项（用于"端点数量"和"活动端点"卡片）
  const [endpointHealthStats, setEndpointHealthStats] = useState(null)

  const loadProviderStats = useCallback(async () => {
    if (!isWailsEnvironment()) return
    try {
      const [claude, codex] = await Promise.all([
        getUsageSummary('', '', 'claude').catch(() => null),
        getUsageSummary('', '', 'codex').catch(() => null)
      ])
      setProviderStats({ claude, codex })
    } catch {
      // 静默处理
    }
  }, [])

  const loadEndpointHealthStats = useCallback(async () => {
    if (!isWailsEnvironment()) return
    try {
      const [claude, codex, codexChannels] = await Promise.all([
        getEndpointHealthChart().catch(() => null),
        getCodexEndpointHealthChart().catch(() => null),
        getCodexChannels().catch(() => [])
      ])
      // 计算所有渠道的 Codex 端点总数（不仅是当前激活渠道）
      const codexTotalAll = (codexChannels || []).reduce(
        (sum, ch) => sum + ((ch.endpoints || []).length),
        0
      )
      setEndpointHealthStats({
        claude,
        codex,
        codexTotalAll
      })
    } catch {
      // 静默处理
    }
  }, [])

  useEffect(() => {
    loadProviderStats()
    loadEndpointHealthStats()
    const interval = setInterval(() => {
      loadProviderStats()
      loadEndpointHealthStats()
    }, 30000)
    return () => clearInterval(interval)
  }, [loadProviderStats, loadEndpointHealthStats])

  // 一个端点 = 一个组，显示当前活动端点
  // 优先从 groups 获取活跃状态，因为 group.is_active 才表示"正在使用"
  const activeGroup = groups.groups?.find(g => g.is_active)

  // 构建显示文本
  let activeEndpointText = '无活动端点'
  let activeEndpointStatusColor = 'bg-slate-50 text-slate-600'
  let activeEndpointTooltip = activeGroup ? activeGroup.name : '无活动端点'
  if (activeGroup) {
    const activeGroupName = activeGroup.name
    const list = endpoints.endpoints || []
    const inGroup = list.filter((e) => (e.group || e.channel || e.name) === activeGroupName)
    const checked = inGroup.filter((e) => {
      const neverChecked = e.never_checked || e.neverChecked
      const hasLastCheck = !!(e.last_check || e.lastCheck)
      return !neverChecked && hasLastCheck
    })
    const healthyCount = checked.filter((e) => e.status === 'healthy' || e.healthy).length

    const healthStatus = checked.length === 0
      ? '未检测'
      : (healthyCount > 0 ? '✓ 健康' : '✗ 异常')

    activeEndpointText = `${activeGroupName} (${healthStatus})`
    activeEndpointTooltip = activeEndpointText
    activeEndpointStatusColor = checked.length === 0
      ? 'bg-slate-50 text-slate-600'
      : (healthyCount > 0 ? 'bg-emerald-50 text-emerald-600' : 'bg-rose-50 text-rose-600')
  } else if (endpoints.healthy > 0) {
    activeEndpointText = `${endpoints.healthy} 个可用`
    activeEndpointTooltip = activeEndpointText
    activeEndpointStatusColor = 'bg-emerald-50 text-emerald-600'
  }

  // 成本和 tokens 数据
  const todayCost = formatCost(connections.today_cost)
  const totalCost = formatCost(connections.all_time_total_cost)
  const todayTokens = formatTokens(connections.today_tokens)
  const totalTokens = formatTokens(connections.all_time_total_tokens)

  // 构建 Claude/Codex 分项 subText
  const cs = providerStats?.claude
  const xs = providerStats?.codex
  const costSub = cs || xs
    ? `Claude ${formatCost(cs?.all_time_total_cost || 0)} / Codex ${formatCost(xs?.all_time_total_cost || 0)}`
    : undefined
  const todayCostSub = cs || xs
    ? `Claude ${formatCost(cs?.today_cost || 0)} / Codex ${formatCost(xs?.today_cost || 0)}`
    : undefined
  const tokensSub = cs || xs
    ? `Claude ${formatTokens(cs?.all_time_total_tokens || 0)} / Codex ${formatTokens(xs?.all_time_total_tokens || 0)}`
    : undefined
  const todayTokensSub = cs || xs
    ? `Claude ${formatTokens(cs?.today_tokens || 0)} / Codex ${formatTokens(xs?.today_tokens || 0)}`
    : undefined
  const requestsSub = cs || xs
    ? `Claude ${cs?.all_time_total_requests || 0} / Codex ${xs?.all_time_total_requests || 0}`
    : undefined
  const todayRequestsSub = cs || xs
    ? `Claude ${cs?.today_requests || 0} / Codex ${xs?.today_requests || 0}`
    : undefined

  // 端点分项 subText（端点数量 / 活动端点）
  // 端点数量：使用所有渠道汇总（Claude 已经是全量，Codex 用 codexTotalAll 汇总所有渠道）
  // 活动端点：使用当前激活渠道的健康端点数（health 状态仅对当前激活渠道有意义）
  const claudeHealth = endpointHealthStats?.claude
  const codexHealth = endpointHealthStats?.codex
  const codexTotalAll = endpointHealthStats?.codexTotalAll || 0
  const endpointTotalSub = claudeHealth || codexHealth
    ? `Claude ${claudeHealth?.total || 0} / Codex ${codexTotalAll}`
    : undefined
  const endpointActiveSub = claudeHealth || codexHealth
    ? `Claude ${claudeHealth?.healthy || 0} / Codex ${codexHealth?.healthy || 0}`
    : undefined

  return (
    <div className="grid grid-cols-2 md:grid-cols-4 xl:grid-cols-8 gap-4 mb-6">
      <KPICard
        title="今日成本"
        value={todayCost}
        subText={todayCostSub}
        icon={DollarSign}
        statusColor="bg-emerald-50 text-emerald-600"
      />
      <KPICard
        title="今日 Tokens"
        value={todayTokens}
        subText={todayTokensSub}
        icon={Zap}
        statusColor="bg-amber-50 text-amber-600"
      />
      <KPICard
        title="今日请求"
        value={connections.today_requests || 0}
        subText={todayRequestsSub}
        icon={Network}
        statusColor="bg-cyan-50 text-cyan-600"
      />
      <KPICard
        title="总成本"
        value={totalCost}
        subText={costSub}
        icon={DollarSign}
        statusColor="bg-blue-50 text-blue-600"
      />
      <KPICard
        title="总 Tokens"
        value={totalTokens}
        subText={tokensSub}
        icon={Zap}
        statusColor="bg-indigo-50 text-indigo-600"
      />
      <KPICard
        title="总请求数"
        value={connections.all_time_total_requests || 0}
        subText={requestsSub}
        icon={Network}
        statusColor="bg-purple-50 text-purple-600"
      />
      <KPICard
        title="端点数量"
        value={endpoints.total || 0}
        subText={endpointTotalSub}
        icon={Server}
        statusColor="bg-violet-50 text-violet-600"
      />
      <KPICard
        title="活动端点"
        value={activeEndpointText}
        tooltip={activeEndpointTooltip}
        subText={endpointActiveSub}
        icon={Activity}
        statusColor={activeEndpointStatusColor}
      />
    </div>
  )
}

export default KPICardsGrid
