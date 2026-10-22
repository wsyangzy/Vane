// ============================================
// StatsOverview - 统计概览组件
// 2025-12-01 11:11:13
// ============================================

import {
  Zap,
  CheckCircle2,
  Clock,
  Database,
  DollarSign,
  FileText,
  XCircle
} from 'lucide-react';
import { Tooltip } from '@/shared/ui/index.js';
import { formatCacheHitRate } from '../utils/tokenDisplay.js';

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
const StatsOverview = ({ stats, claudeStats, codexStats, total = 0 }) => {
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

  // 缓存命中率：两个 provider 的 input_tokens 口径不同（Codex 已含缓存），合并成一个数没有意义，
  // 主值取筛选范围自身的命中率，子文本按 provider 分列
  const cacheHitRate = formatCacheHitRate(stats.cache_hit_rate);
  const cacheSubParts = [];
  if (claudeRequests > 0) cacheSubParts.push(`Claude ${formatCacheHitRate(claudeStats?.cache_hit_rate)}`);
  if (codexRequests > 0) cacheSubParts.push(`Codex ${formatCacheHitRate(codexStats?.cache_hit_rate)}`);
  const cacheSubText = cacheSubParts.length > 1 ? cacheSubParts.join(' / ') : undefined;

  const kpis = [
    { label: '总请求数', value: totalRequests, subText: requestSubText, icon: Zap, statusColor: 'text-orange-500' },
    { label: '成功率', value: successRate, icon: CheckCircle2, statusColor: 'text-emerald-500' },
    { label: '平均耗时', value: avgDuration, icon: Clock, statusColor: 'text-amber-500' },
    { label: '总成本', value: totalCost, subText: costSubText, icon: DollarSign, statusColor: 'text-blue-500' },
    { label: '总 Token', value: totalTokens, subText: tokenSubText, icon: FileText, statusColor: 'text-indigo-500' },
    { label: '缓存命中率', value: cacheHitRate, subText: cacheSubText, icon: Database, statusColor: 'text-sky-500' },
    { label: '失败请求', value: failedRequests, icon: XCircle, statusColor: 'text-rose-500' }
  ];

  return (
    <dl aria-label="请求统计" className="grid shrink-0 grid-cols-7 divide-x divide-slate-200/70 border-b border-slate-200/80 bg-slate-50/40 py-3">
      {kpis.map(({ label, value, subText, icon: Icon, statusColor }) => (
        <div key={label} className="min-w-0 px-3 xl:px-4">
          <dt className="flex items-center gap-1.5 whitespace-nowrap text-[11px] font-medium leading-4 text-slate-500">
            <Icon aria-hidden="true" className={`h-3.5 w-3.5 shrink-0 ${statusColor}`} />
            {label}
          </dt>
          <dd className="mt-1 text-base font-semibold leading-6 tabular-nums text-slate-900 xl:text-lg">
            <Tooltip content={[value, subText].filter(item => item !== undefined).join('\n')} className="block min-w-0" contentClassName="whitespace-pre-line">
              <span tabIndex={0} className="block truncate rounded-sm focus-visible:outline-2 focus-visible:outline-indigo-500">{value}</span>
            </Tooltip>
          </dd>
        </div>
      ))}
    </dl>
  );
};

export default StatsOverview;
