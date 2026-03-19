// ============================================
// ChannelRow - 渠道分组行组件
// ============================================

import { ChevronDown, ChevronRight } from 'lucide-react';

const ChannelRow = ({ channel, endpoints, expanded, onToggle, storageMode }) => {
  const isSqliteMode = storageMode === 'sqlite';

  // 统计数据
  const totalCount = endpoints.length;
  const enabledCount = endpoints.filter(e => isSqliteMode ? e.enabled : e.group_is_active).length;

  // 参与故障转移：已启用 且 failoverEnabled 不为 false
  const failoverCount = endpoints.filter(e => {
    const isEnabled = isSqliteMode ? e.enabled : e.group_is_active;
    return isEnabled && e.failoverEnabled !== false;
  }).length;

  // 健康统计：healthy 或 skipHealthCheck 均视为健康
  const healthyOrSkippedCount = endpoints.filter(e =>
    e.healthy || e.skipHealthCheck || e.skip_health_check
  ).length;

  // 全部启用故障转移
  const allFailover = failoverCount === totalCount && totalCount > 0;
  // 全部健康（含跳过检测）
  const allHealthy = healthyOrSkippedCount === totalCount && totalCount > 0;

  return (
    <tr
      className="bg-slate-100/80 hover:bg-slate-100 cursor-pointer transition-colors border-b border-slate-200"
      onClick={onToggle}
    >
      <td className="px-6 py-3" colSpan={9}>
        <div className="flex items-center justify-between">
          <div className="flex items-center space-x-3">
            {/* 展开/折叠图标 */}
            {expanded ? (
              <ChevronDown size={18} className="text-slate-500" />
            ) : (
              <ChevronRight size={18} className="text-slate-500" />
            )}

            {/* 渠道名称 */}
            <span className="font-semibold text-slate-700 text-sm">
              {channel}
            </span>

            {/* 端点数量 */}
            <span className={`text-xs px-2 py-0.5 rounded-full border ${
              allFailover
                ? 'text-emerald-600 bg-emerald-50 border-emerald-200'
                : 'text-slate-500 bg-slate-200 border-slate-300'
            }`}>
              {allFailover
                ? `${totalCount} 端点`
                : `${failoverCount}/${totalCount} 故障转移`
              }
            </span>
          </div>

          {/* 状态汇总 */}
          <div className="flex items-center space-x-3 text-xs">
            {/* 启用标签 */}
            <span className={`px-2 py-0.5 rounded border ${
              enabledCount === totalCount
                ? 'text-emerald-600 bg-emerald-50 border-emerald-100'
                : 'text-amber-600 bg-amber-50 border-amber-100'
            }`}>
              {enabledCount === totalCount ? '全部启用' : `${enabledCount} 启用`}
            </span>

            {/* 健康标签 */}
            <span className={`px-2 py-0.5 rounded border ${
              allHealthy
                ? 'text-emerald-600 bg-emerald-50 border-emerald-100'
                : healthyOrSkippedCount > 0
                  ? 'text-amber-600 bg-amber-50 border-amber-100'
                  : 'text-slate-400 bg-slate-50 border-slate-200'
            }`}>
              {allHealthy ? '全部健康' : `${healthyOrSkippedCount}/${totalCount} 健康`}
            </span>
          </div>
        </div>
      </td>
    </tr>
  );
};

export default ChannelRow;
