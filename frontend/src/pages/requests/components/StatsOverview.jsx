// ============================================
// StatsOverview - 统计概览组件
// 2025-12-01 11:11:13
// ============================================

import {
  Zap,
  CheckCircle2,
  Clock,
  DollarSign,
  FileText,
  XCircle
} from 'lucide-react';
import { KPICard } from '@components/ui';

/**
 * 格式化Token数量（转换为M单位）
 */
const formatTokens = (tokens) => {
  const numericTokens = Number(tokens) || 0;
  if (numericTokens === 0) return '0.00M';
  const inMillions = numericTokens / 1000000;
  return `${inMillions.toFixed(2)}M`;
};

/**
 * 格式化成本
 */
const formatCost = (cost) => {
  const num = parseFloat(cost);
  if (!cost || isNaN(num)) return '$0.00';
  return `$${num.toFixed(2)}`;
};

/**
 * 格式化持续时间
 */
const formatDuration = (duration) => {
  if (!duration || duration === 0) return '-';
  const ms = parseFloat(duration);
  if (isNaN(ms)) return '-';
  if (ms >= 1000) {
    return `${(ms / 1000).toFixed(1)}s`;
  } else {
    return `${Math.round(ms)}ms`;
  }
};

/**
 * StatsOverview - 统计概览组件
 * @param {Object} props
 * @param {Object} props.stats - 统计数据（全量）
 * @param {Object} props.claudeStats - Claude 提供商统计
 * @param {Object} props.codexStats - Codex 提供商统计
 * @param {number} props.total - 总记录数（来自分页）
 */
const StatsOverview = ({ stats, claudeStats, codexStats, total = 0, isBlurred = false }) => {
  if (!stats) return null;

  // 格式化后端返回的原始数据
  const totalRequests = stats.total_requests || total || 0;
  const successRate = stats.success_rate ? `${stats.success_rate.toFixed(1)}%` : '-%';
  const avgDuration = formatDuration(stats.avg_duration_ms);
  const totalCost = formatCost(stats.total_cost_usd);
  const totalTokens = formatTokens(stats.total_tokens);
  const failedRequests = stats.failed_requests || 0;

  // 按 Provider 分组的成本
  const claudeCost = claudeStats?.total_cost_usd || 0;
  const codexCost = codexStats?.total_cost_usd || 0;

  // 构建成本卡片的子文本（显示 Claude/Codex 分项）
  const costSubParts = [];
  if (claudeCost > 0) costSubParts.push(`Claude ${formatCost(claudeCost)}`);
  if (codexCost > 0) costSubParts.push(`Codex ${formatCost(codexCost)}`);
  const costSubText = costSubParts.length > 0
    ? costSubParts.join(' / ')
    : undefined;

  // 按 Provider 分组的请求数
  const claudeRequests = claudeStats?.total_requests || 0;
  const codexRequests = codexStats?.total_requests || 0;
  const requestSubParts = [];
  if (claudeRequests > 0) requestSubParts.push(`Claude ${claudeRequests}`);
  if (codexRequests > 0) requestSubParts.push(`Codex ${codexRequests}`);
  const requestSubText = requestSubParts.length > 0
    ? requestSubParts.join(' / ')
    : undefined;

  // 按 Provider 分组的 Token 数
  const claudeTokens = claudeStats?.total_tokens || 0;
  const codexTokens = codexStats?.total_tokens || 0;
  const tokenSubParts = [];
  if (claudeTokens > 0) tokenSubParts.push(`Claude ${formatTokens(claudeTokens)}`);
  if (codexTokens > 0) tokenSubParts.push(`Codex ${formatTokens(codexTokens)}`);
  const tokenSubText = tokenSubParts.length > 0
    ? tokenSubParts.join(' / ')
    : undefined;

  const kpis = [
    { label: '总请求数', value: totalRequests, subText: requestSubText, icon: Zap, statusColor: 'bg-orange-50 text-orange-500' },
    { label: '成功率', value: successRate, icon: CheckCircle2, statusColor: 'bg-emerald-50 text-emerald-500' },
    { label: '平均耗时', value: avgDuration, icon: Clock, statusColor: 'bg-amber-50 text-amber-500' },
    { label: '总成本', value: totalCost, subText: costSubText, icon: DollarSign, statusColor: 'bg-blue-50 text-blue-500' },
    { label: '总Token数 (M)', value: totalTokens, subText: tokenSubText, icon: FileText, statusColor: 'bg-indigo-50 text-indigo-500' },
    { label: '失败请求', value: failedRequests, icon: XCircle, statusColor: 'bg-rose-50 text-rose-500' }
  ];

  return (
    <div className={`grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4 transition-all duration-300 ${isBlurred ? 'opacity-40 pointer-events-none blur-[1px]' : ''}`}>
      {kpis.map((kpi, idx) => (
        <KPICard
          key={idx}
          title={kpi.label}
          value={kpi.value}
          subText={kpi.subText}
          icon={kpi.icon}
          statusColor={kpi.statusColor}
        />
      ))}
    </div>
  );
};

export default StatsOverview;
