// ============================================
// Token 成本图组件
// 2025-11-28
// ============================================

import { useState, useEffect, useCallback, useRef } from 'react';
import { RefreshCw, DollarSign } from 'lucide-react';
import {
  ComposedChart,
  Bar,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer
} from 'recharts';
import { fetchEndpointCostsData } from '@/services/http/api.js';
import { Tooltip as UiTooltip, useToast } from '@/shared/ui/index.js';
import { formatCacheHitRate } from '@/features/requests/utils/tokenDisplay.js';
import { prepareGroupCosts } from '../credentialGroupStats.js';

// 格式化 Token 数量
const formatTokens = (value) => {
  if (value >= 1000000) return `${(value / 1000000).toFixed(1)}M`;
  if (value >= 1000) return `${(value / 1000).toFixed(1)}K`;
  return value.toString();
};

// 自定义 Tooltip
const CustomTooltip = ({ active, payload }) => {
  if (!active || !payload || !payload.length) return null;

  const item = payload[0]?.payload
  const providerLabel = item?.provider === 'codex' ? 'Codex' : 'Claude'
  const channelName = item?.channel || item?.channelName || '-'
  const groupName = item?.group || item?.groupName || '-'

  return (
    <div className="min-w-[240px] max-w-[320px] bg-white p-3 rounded-lg shadow-lg border border-slate-100 text-sm">
      <div className="flex items-center gap-2 mb-2">
        <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${
          item?.provider === 'codex'
            ? 'bg-blue-50 text-blue-600'
            : 'bg-orange-50 text-orange-600'
        }`}>
          {providerLabel}
        </span>
        <span className="min-w-0 flex-1 break-words font-medium text-slate-900">{channelName}</span>
      </div>
      <div className="mb-2 space-y-1 border-b border-slate-100 pb-2">
        <div className="flex items-start justify-between gap-4 text-xs">
          <span className="text-slate-400">凭证分组</span>
          <span className="max-w-[190px] break-words text-right font-medium text-slate-700">{groupName}</span>
        </div>
      </div>
      {payload.map((entry, index) => (
        <div key={index} className="flex items-center justify-between gap-4">
          <span className="text-slate-500">{entry.name}:</span>
          <span className="font-mono font-medium" style={{ color: entry.color }}>
            {entry.dataKey === 'tokens' ? formatTokens(entry.value) : `$${entry.value.toFixed(2)}`}
          </span>
        </div>
      ))}
      <div className="flex items-center justify-between gap-4">
        <span className="text-slate-500">缓存命中率:</span>
        <span className="font-mono font-medium text-sky-600">{formatCacheHitRate(item?.cache_hit_rate)}</span>
      </div>
    </div>
  )
};

const TokenCostChart = () => {
  const provider = ''
  const [chartData, setChartData] = useState([]);
  const [loading, setLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const refreshIntervalRef = useRef(null);
  const lastErrorRef = useRef('');
  const toast = useToast();

  // 加载数据
  const loadData = useCallback(async (showRefreshing = false) => {
    if (showRefreshing) {
      setIsRefreshing(true);
    }
    try {
      const data = await fetchEndpointCostsData(provider);
      setChartData(prepareGroupCosts(data));
      lastErrorRef.current = '';
    } catch (error) {
      console.error('加载凭证分组成本数据失败:', error);
      const message = error?.message || '凭证分组成本数据加载失败';
      if (showRefreshing || lastErrorRef.current !== message) {
        toast.error(message, '凭证分组成本数据加载失败');
      }
      lastErrorRef.current = message;
    } finally {
      setLoading(false);
      setIsRefreshing(false);
    }
  }, [provider, toast]);

  // 初始加载和 provider 变化时重新加载
  useEffect(() => {
    loadData();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // 定时刷新（每 60 秒）
  useEffect(() => {
    refreshIntervalRef.current = setInterval(() => {
      loadData(false);
    }, 60000);

    return () => {
      if (refreshIntervalRef.current) {
        clearInterval(refreshIntervalRef.current);
      }
    };
  }, [loadData]);

  // 监听 SSE 图表更新事件
  useEffect(() => {
    const handleChartUpdate = (event) => {
      const { chart_type } = event.detail || {};
      if (chart_type === 'endpoint_costs' || chart_type === 'endpointCosts') {
        // Legacy endpoint broadcasts only invalidate; reload group aggregates.
        loadData();
      }
    };

    document.addEventListener('chartUpdate', handleChartUpdate);
    return () => {
      document.removeEventListener('chartUpdate', handleChartUpdate);
    };
  }, [loadData]);

  // 手动刷新
  const handleRefresh = () => {
    loadData(true);
  };

  return (
    <div className="bg-white p-6 rounded-2xl border border-slate-200/60 shadow-sm">
      <div className="flex justify-between items-start mb-1">
        <div className="flex items-center space-x-2">
          <div className="p-1.5 bg-rose-50 text-rose-500 rounded-md">
            <DollarSign size={16} />
          </div>
          <h3 className="font-semibold text-slate-900">当日凭证分组 Token 成本</h3>
        </div>
        <UiTooltip content="刷新数据">
          <button
            onClick={handleRefresh}
            disabled={isRefreshing}
            className="p-1.5 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-md transition-colors disabled:opacity-50"
            aria-label="刷新数据"
          >
            <RefreshCw size={14} className={isRefreshing ? 'animate-spin' : ''} />
          </button>
        </UiTooltip>
      </div>
      <p className="text-xs text-slate-500 mb-4">按提供商、渠道与凭证分组汇总，合并同一分组下全部端点的用量与成本。</p>

      <div className="h-[280px] w-full">
        {loading ? (
          <div className="h-full flex items-center justify-center text-slate-400">
            <RefreshCw size={20} className="animate-spin mr-2" />
            加载中...
          </div>
        ) : chartData.length === 0 ? (
          <div className="h-full flex items-center justify-center text-slate-400 text-sm">
            暂无数据
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <ComposedChart data={chartData} margin={{ top: 10, right: 50, left: 0, bottom: 5 }}>
              <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="#f1f5f9" />
              <XAxis
                dataKey="name"
                axisLine={false}
                tickLine={false}
                tick={({ x, y, payload }) => {
                  const maxLen = chartData.length > 5 ? 8 : 14
                  const text = payload.value.length > maxLen
                    ? payload.value.slice(0, maxLen) + '…'
                    : payload.value
                  return (
                    <text x={x} y={y + 12} textAnchor="middle" fill="#64748b" fontSize={11}>
                      {text}
                    </text>
                  )
                }}
                interval={0}
              />
              <YAxis
                yAxisId="left"
                axisLine={false}
                tickLine={false}
                tick={{ fill: '#94a3b8', fontSize: 11 }}
                tickFormatter={formatTokens}
                width={50}
              />
              <YAxis
                yAxisId="right"
                orientation="right"
                axisLine={false}
                tickLine={false}
                tick={{ fill: '#f43f5e', fontSize: 11 }}
                tickFormatter={(v) => `$${v.toFixed(2)}`}
                width={55}
              />
              <Tooltip content={<CustomTooltip />} cursor={{ fill: '#f8fafc' }} />
              <Bar
                yAxisId="left"
                dataKey="tokens"
                fill="#818cf8"
                barSize={32}
                radius={[4, 4, 0, 0]}
                name="Token 量"
              />
              <Line
                yAxisId="right"
                type="monotone"
                dataKey="cost"
                stroke="#f43f5e"
                strokeWidth={2}
                dot={{ r: 4, fill: '#f43f5e', strokeWidth: 0 }}
                activeDot={{ r: 6, fill: '#f43f5e', strokeWidth: 2, stroke: '#fff' }}
                name="成本 (USD)"
              />
            </ComposedChart>
          </ResponsiveContainer>
        )}
      </div>

      {/* 图例 */}
      {!loading && chartData.length > 0 && (
        <div className="flex justify-center space-x-6 mt-4 pt-3 border-t border-slate-100">
          <div className="flex items-center text-xs text-slate-500">
            <span className="w-3 h-3 rounded bg-indigo-400 mr-2"></span>
            Token 使用量
          </div>
          <div className="flex items-center text-xs text-slate-500">
            <span className="w-3 h-3 rounded-full bg-rose-500 mr-2"></span>
            成本 (USD)
          </div>
        </div>
      )}
    </div>
  );
};

export default TokenCostChart;
