// ============================================
// 请求趋势图组件
// 2025-11-28
// ============================================

import { useState, useEffect, useCallback, useRef } from 'react';
import { Activity, RefreshCw } from 'lucide-react';
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer
} from 'recharts';
import { fetchRequestTrendData } from '@utils/api.js';
import { formatHHMM, padTimeSeries } from '@utils/chartTime.js';
import { CustomSelect, Tooltip as UiTooltip } from '@components/ui';

// 时间范围选项
const TIME_RANGE_OPTIONS = [
  { value: 30, label: '30分钟' },
  { value: 60, label: '1小时' },
  { value: 180, label: '3小时' },
  { value: 360, label: '6小时' },
  { value: 720, label: '12小时' },
  { value: 1440, label: '24小时' }
];

const RequestTrendChart = ({ uptimeSeconds = 0 }) => {
  const provider = ''
  const [chartData, setChartData] = useState([]);
  const [loading, setLoading] = useState(true);
  const [timeRange, setTimeRange] = useState(30);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const refreshIntervalRef = useRef(null);

  const toCumulative = (data) => {
    let total = 0;
    let success = 0;
    let fail = 0;

    return (data || []).map((point) => {
      total += point?.total || 0;
      success += point?.success || 0;
      fail += point?.fail || 0;
      return { ...point, total, success, fail };
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
      console.log('📊 [RequestTrendChart] 开始加载数据, timeRange:', timeRange, 'provider:', provider);
      const data = await fetchRequestTrendData(timeRange, provider);
      console.log('📊 [RequestTrendChart] 获取到原始数据:', data);
      const processedData = toCumulative(padTimeSeries(data, {
        minutes: timeRange,
        stepMs: 30 * 1000,
        defaults: { total: 0, success: 0, fail: 0 },
      }));
      console.log('📊 [RequestTrendChart] 处理后数据:', processedData?.length, '个点');
      setChartData(processedData);
    } catch (error) {
      console.error('加载请求趋势数据失败:', error);
    } finally {
      setLoading(false);
      setIsRefreshing(false);
    }
  }, [timeRange, provider]);

  // 初始加载和时间范围/provider 变化时重新加载
  useEffect(() => {
    setLoading(true);
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
      if (chart_type === 'request_trends' || chart_type === 'requestTrend') {
        // 使用统一的加载逻辑，确保时间范围选择器生效
        loadData(false);
        console.log('📊 [SSE] 请求趋势图已触发刷新');
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

  const xKey = chartData.some(d => d?.timestamp) ? 'timestamp' : 'time';

  return (
    <div className="bg-white p-6 rounded-2xl border border-slate-200/60 shadow-sm">
      <div className="flex justify-between items-center mb-6">
        <div className="flex items-center space-x-2">
          <div className="p-1.5 bg-indigo-50 text-indigo-600 rounded-md">
            <Activity size={18} />
          </div>
          <h3 className="font-semibold text-slate-900">
            请求趋势 (最近{TIME_RANGE_OPTIONS.find(o => o.value === timeRange)?.label || `${timeRange}分钟`})
          </h3>
        </div>
        <div className="flex items-center space-x-3">
          {/* 时间范围选择器 */}
          <CustomSelect
            options={options}
            value={timeRange}
            onChange={setTimeRange}
            size="sm"
          />

          {/* 刷新按钮 */}
          <UiTooltip content="刷新数据">
            <button
              onClick={handleRefresh}
              disabled={isRefreshing}
              className="p-1.5 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-md transition-colors disabled:opacity-50"
              aria-label="刷新数据"
            >
              <RefreshCw size={16} className={isRefreshing ? 'animate-spin' : ''} />
            </button>
          </UiTooltip>

          {/* 图例 */}
          <div className="flex space-x-4 text-xs font-medium">
            <div className="flex items-center text-indigo-600">
              <span className="w-2 h-2 rounded-full bg-indigo-500 mr-2"></span>总请求
            </div>
            <div className="flex items-center text-emerald-600">
              <span className="w-2 h-2 rounded-full bg-emerald-500 mr-2"></span>成功
            </div>
            <div className="flex items-center text-rose-500">
              <span className="w-2 h-2 rounded-full bg-rose-500 mr-2"></span>失败
            </div>
          </div>
        </div>
      </div>

      <div className="h-[280px] w-full">
        {loading ? (
          <div className="h-full flex items-center justify-center text-slate-400">
            <RefreshCw size={24} className="animate-spin mr-2" />
            加载中...
          </div>
        ) : chartData.length === 0 ? (
          <div className="h-full flex items-center justify-center text-slate-400">
            暂无数据
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={chartData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
              <defs>
                <linearGradient id="colorTotal" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="#6366f1" stopOpacity={0.1} />
                  <stop offset="95%" stopColor="#6366f1" stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="#f1f5f9" />
              <XAxis
                dataKey={xKey}
                axisLine={false}
                tickLine={false}
                tick={{ fill: '#94a3b8', fontSize: 12 }}
                dy={10}
                tickFormatter={formatHHMM}
              />
              <YAxis axisLine={false} tickLine={false} tick={{ fill: '#94a3b8', fontSize: 12 }} />
              <Tooltip
                labelFormatter={formatHHMM}
                contentStyle={{ borderRadius: '8px', border: 'none', boxShadow: '0 4px 12px rgba(0,0,0,0.1)' }}
              />
              <Area type="monotone" dataKey="total" stroke="#6366f1" strokeWidth={2} fillOpacity={1} fill="url(#colorTotal)" name="总请求" />
              <Area type="monotone" dataKey="success" stroke="#10b981" strokeWidth={2} fillOpacity={0} fill="transparent" name="成功" />
              <Area type="monotone" dataKey="fail" stroke="#f43f5e" strokeWidth={2} fillOpacity={0} fill="transparent" name="失败" />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </div>

      <div className="mt-3 text-[11px] text-slate-400">
        图表数据仅统计自启动以来的运行时历史（重启后清空），最多保留 24 小时。
      </div>
    </div>
  );
};

export default RequestTrendChart;
