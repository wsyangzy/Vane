// ============================================
// 连接活动图表组件
// 2025-11-28
// ============================================

import { useState, useEffect, useCallback, useRef } from 'react';
import { RefreshCw, Wifi } from 'lucide-react';
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer
} from 'recharts';
import { fetchConnectionActivityData } from '@utils/api.js';
import { formatHHMM, padTimeSeries } from '@utils/chartTime.js';
import { CustomSelect, Tooltip as UiTooltip } from '@components/ui';

// 时间范围选项
const TIME_RANGE_OPTIONS = [
  { value: 30, label: '30 分钟' },
  { value: 60, label: '1 小时' },
  { value: 120, label: '2 小时' }
];

// 自定义 Tooltip
const CustomTooltip = ({ active, payload, label }) => {
  if (!active || !payload || !payload.length) return null;

  return (
    <div className="bg-white p-3 rounded-lg shadow-lg border border-slate-100 text-sm">
      <p className="font-medium text-slate-900 mb-2">{formatHHMM(label)}</p>
      {payload.map((entry, index) => (
        <div key={index} className="flex items-center justify-between gap-4">
          <span className="text-slate-500">{entry.name}:</span>
          <span className="font-mono font-medium" style={{ color: entry.color }}>
            {entry.value}
          </span>
        </div>
      ))}
    </div>
  );
};

const ConnectionActivityChart = ({ uptimeSeconds = 0 }) => {
  const provider = ''
  const [chartData, setChartData] = useState([]);
  const [loading, setLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [timeRange, setTimeRange] = useState(60);
  const refreshIntervalRef = useRef(null);

  const toCumulative = (data) => {
    let connections = 0;
    return (data || []).map((point) => {
      connections += point?.connections || 0;
      return { ...point, connections };
    });
  };

  const maxMinutes = Math.max(1, Math.floor((uptimeSeconds || 0) / 60));
  const availableOptions = TIME_RANGE_OPTIONS.filter(o => o.value <= maxMinutes);
  const options = availableOptions.length > 0 ? availableOptions : TIME_RANGE_OPTIONS.slice(0, 1);

  useEffect(() => {
    if (timeRange > maxMinutes && options.length > 0) {
      setTimeRange(options[options.length - 1].value);
    }
  }, [maxMinutes, options, timeRange]);

  // 加载数据
  const loadData = useCallback(async (showRefreshing = false) => {
    if (showRefreshing) {
      setIsRefreshing(true);
    }
    try {
      const data = await fetchConnectionActivityData(timeRange, provider);
      setChartData(
        toCumulative(padTimeSeries(data, {
          minutes: timeRange,
          stepMs: 30 * 1000,
          defaults: { connections: 0 },
        }))
      );
    } catch (error) {
      console.error('加载连接活动数据失败:', error);
    } finally {
      setLoading(false);
      setIsRefreshing(false);
    }
  }, [timeRange, provider]);

  // 初始加载
  useEffect(() => {
    loadData();
  }, [timeRange]); // eslint-disable-line react-hooks/exhaustive-deps

  // 定时刷新（每 30 秒）
  useEffect(() => {
    refreshIntervalRef.current = setInterval(() => {
      loadData(false);
    }, 30000);

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
      if (chart_type === 'connection_activity' || chart_type === 'connectionActivity') {
        // 使用统一的加载逻辑，确保时间范围选择器生效
        loadData(false);
        console.log('📊 [SSE] 连接活动图已触发刷新');
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

  // 计算当前连接数（最后一个数据点）
  const currentConnections = chartData.length > 0
    ? chartData[chartData.length - 1]?.connections || 0
    : 0;

  // 计算峰值连接数
  const peakConnections = chartData.length > 0
    ? Math.max(...chartData.map(d => d.connections || 0))
    : 0;

  const xKey = chartData.some(d => d?.timestamp) ? 'timestamp' : 'time';

  return (
    <div className="bg-white p-6 rounded-2xl border border-slate-200/60 shadow-sm flex flex-col h-full">
      <div className="flex justify-between items-start mb-1">
        <div className="flex items-center space-x-2">
          <div className="p-1.5 bg-violet-50 text-violet-500 rounded-md">
            <Wifi size={16} />
          </div>
          <h3 className="font-semibold text-slate-900">连接活动</h3>
        </div>
        <div className="flex items-center space-x-2">
          <CustomSelect
            options={options}
            value={timeRange}
            onChange={setTimeRange}
            size="xs"
          />
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
      </div>
      <p className="text-xs text-slate-500 mb-4">
        当前: <span className="font-mono font-medium text-violet-600">{currentConnections}</span>
        <span className="mx-2 text-slate-300">|</span>
        峰值: <span className="font-mono font-medium text-slate-600">{peakConnections}</span>
      </p>

      <div className="flex-1 min-h-[200px]">
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
            <AreaChart data={chartData} margin={{ top: 10, right: 10, left: 0, bottom: 0 }}>
              <defs>
                <linearGradient id="colorConnections" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="#8b5cf6" stopOpacity={0.3} />
                  <stop offset="95%" stopColor="#8b5cf6" stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="#f1f5f9" />
              <XAxis
                dataKey={xKey}
                axisLine={false}
                tickLine={false}
                tick={{ fill: '#94a3b8', fontSize: 10 }}
                interval="preserveStartEnd"
                tickFormatter={formatHHMM}
              />
              <YAxis
                axisLine={false}
                tickLine={false}
                tick={{ fill: '#94a3b8', fontSize: 10 }}
                width={30}
                allowDecimals={false}
              />
              <Tooltip content={<CustomTooltip />} />
              <Area
                type="monotone"
                dataKey="connections"
                stroke="#8b5cf6"
                strokeWidth={2}
                fill="url(#colorConnections)"
                name="累计连接数"
                dot={false}
                activeDot={{ r: 4, fill: '#8b5cf6', strokeWidth: 0 }}
              />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </div>

      {/* 图例 */}
      {!loading && chartData.length > 0 && (
        <div className="flex justify-center space-x-6 mt-3 pt-3 border-t border-slate-100">
          <div className="flex items-center text-xs text-slate-500">
            <span className="w-3 h-3 rounded bg-violet-400 mr-2" />
            累计连接数
          </div>
        </div>
      )}
    </div>
  );
};

export default ConnectionActivityChart;
