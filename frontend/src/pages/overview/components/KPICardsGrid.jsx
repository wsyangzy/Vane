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
  // Codex 渠道用于概览 KPI 的渠道数和端点总数
  const [codexChannelStats, setCodexChannelStats] = useState({ channels: [], totalAll: 0, loaded: false })

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

  const loadCodexChannelStats = useCallback(async () => {
    if (!isWailsEnvironment()) return
    try {
      const codexChannels = await getCodexChannels().catch(() => [])
      const channels = Array.isArray(codexChannels) ? codexChannels : []
      const codexTotalAll = channels.reduce(
        (sum, ch) => sum + ((ch.endpoints || []).length),
        0
      )
      setCodexChannelStats({
        channels,
        totalAll: codexTotalAll,
        loaded: true
      })
    } catch {
      setCodexChannelStats(prev => ({ ...prev, loaded: true }))
    }
  }, [])

  useEffect(() => {
    const initialLoad = setTimeout(() => {
      loadProviderStats()
      loadCodexChannelStats()
    }, 0)
    const interval = setInterval(() => {
      loadProviderStats()
      loadCodexChannelStats()
    }, 30000)
    return () => {
      clearTimeout(initialLoad)
      clearInterval(interval)
    }
  }, [loadProviderStats, loadCodexChannelStats])

  const endpointList = Array.isArray(endpoints.endpoints) ? endpoints.endpoints : []
  const claudeChannels = Array.isArray(groups.groups) ? groups.groups : []
  const claudeChannelCount = claudeChannels.length
  const claudeTotalAll = endpointList.length || endpoints.total || 0
  const codexChannels = codexChannelStats.channels || []
  const codexChannelCount = codexChannels.length
  const codexTotalAll = codexChannelStats.totalAll || 0
  const combinedChannelTotal = claudeChannelCount + codexChannelCount
  const combinedEndpointTotal = claudeTotalAll + codexTotalAll

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

  const endpointTotalSub = codexChannelStats.loaded
    ? `Claude ${claudeTotalAll} / Codex ${codexTotalAll}`
    : undefined
  const channelTotalSub = codexChannelStats.loaded
    ? `Claude ${claudeChannelCount} / Codex ${codexChannelCount}`
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
        title="渠道数量"
        value={codexChannelStats.loaded ? combinedChannelTotal : claudeChannelCount}
        subText={channelTotalSub}
        icon={Activity}
        statusColor="bg-teal-50 text-teal-600"
      />
      <KPICard
        title="端点数量"
        value={codexChannelStats.loaded ? combinedEndpointTotal : (endpoints.total || 0)}
        subText={endpointTotalSub}
        icon={Server}
        statusColor="bg-violet-50 text-violet-600"
      />
    </div>
  )
}

export default KPICardsGrid
