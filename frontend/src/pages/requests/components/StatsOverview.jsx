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
 * @param {Object} props.stats - 统计数据
 * @param {number} props.total - 总记录数（来自分页）
 */
const StatsOverview = ({ stats, total = 0, isBlurred = false }) => {
  if (!stats) return null;

  // 格式化后端返回的原始数据
  const totalRequests = stats.total_requests || total || 0;
  const successRate = stats.success_rate ? `${stats.success_rate.toFixed(1)}%` : '-%';
  const avgDuration = formatDuration(stats.avg_duration_ms);  // 注意：字段是 avg_duration_ms
  const totalCost = formatCost(stats.total_cost_usd);
  const totalTokens = formatTokens(stats.total_tokens);
  const failedRequests = stats.failed_requests || 0;

  const kpis = [
    { label: '总请求数', value: totalRequests, icon: Zap, statusColor: 'bg-orange-50 text-orange-500' },
    { label: '成功率', value: successRate, icon: CheckCircle2, statusColor: 'bg-emerald-50 text-emerald-500' },
    { label: '平均耗时', value: avgDuration, icon: Clock, statusColor: 'bg-amber-50 text-amber-500' },
    { label: '总成本', value: totalCost, icon: DollarSign, statusColor: 'bg-blue-50 text-blue-500' },
    { label: '总Token数 (M)', value: totalTokens, icon: FileText, statusColor: 'bg-indigo-50 text-indigo-500' },
    { label: '失败请求', value: failedRequests, icon: XCircle, statusColor: 'bg-rose-50 text-rose-500' }
  ];

  return (
    <div className={`grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4 transition-all duration-300 ${isBlurred ? 'opacity-40 pointer-events-none blur-[1px]' : ''}`}>
      {kpis.map((kpi, idx) => (
        <KPICard
          key={idx}
          title={kpi.label}
          value={kpi.value}
          icon={kpi.icon}
          statusColor={kpi.statusColor}
        />
      ))}
    </div>
  );
};

export default StatsOverview;
