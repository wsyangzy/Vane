// ============================================
// 端点健康状态图组件
// 简化分类，跳过检测视为健康
// 支持 Claude / Codex 分 Provider 切换查看
// ============================================

import { useState, useEffect, useCallback, useRef } from 'react';
import { RefreshCw, Activity, CheckCircle2, EyeOff } from 'lucide-react';
import {
  PieChart,
  Pie,
  Cell,
  ResponsiveContainer
} from 'recharts';
import { fetchEndpointHealthData } from '@/services/http/api.js';
import { getCodexEndpointHealthChart } from '@/services/wails/index.js';
import { Tooltip as UiTooltip } from '@/shared/ui/index.js';

// 健康状态配置
const HEALTH_CONFIG = {
  healthy: { name: '健康', color: '#10b981' },      // 绿色：检测通过
  skipped: { name: '跳过检测', color: '#a855f7' },  // 紫色：跳过检测（假定健康）
  unhealthy: { name: '异常', color: '#ef4444' },    // 红色：检测失败
  excluded: { name: '不参与', color: '#cbd5e1' }    // 灰色：不参与检测
};

const EndpointHealthChart = () => {
  const [provider, setProvider] = useState('claude');
  const [healthData, setHealthData] = useState({ healthy: 0, unhealthy: 0, skipped: 0, excluded: 0 });
  const [loading, setLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const refreshIntervalRef = useRef(null);

  // 加载数据
  const loadData = useCallback(async (showRefreshing = false) => {
    if (showRefreshing) {
      setIsRefreshing(true);
    }
    try {
      const data = provider === 'codex'
        ? await getCodexEndpointHealthChart()
        : await fetchEndpointHealthData();
      setHealthData({
        healthy: data.healthy || 0,
        unhealthy: data.unhealthy || 0,
        skipped: data.skipped || 0,
        excluded: data.excluded || 0
      });
    } catch (error) {
      console.error('加载端点健康数据失败:', error);
      setHealthData({ healthy: 0, unhealthy: 0, skipped: 0, excluded: 0 });
    } finally {
      setLoading(false);
      setIsRefreshing(false);
    }
  }, [provider]);

  // 切换 Provider 或初始加载时重新加载
  useEffect(() => {
    setLoading(true);
    loadData();
  }, [provider]); // eslint-disable-line react-hooks/exhaustive-deps

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

  // 监听 SSE 图表更新事件（仅 Claude 端有 SSE 广播）
  useEffect(() => {
    const handleChartUpdate = (event) => {
      if (provider !== 'claude') return;
      const { chart_type, data } = event.detail || {};
        if (chart_type === 'endpoint_health' || chart_type === 'endpointHealth') {
          if (data) {
            if (data.healthy !== undefined) {
              setHealthData({
                healthy: data.healthy || 0,
                unhealthy: data.unhealthy || 0,
                skipped: data.skipped || 0,
                excluded: data.excluded || 0
              });
            } else if (data.labels && data.datasets) {
              // Chart.js 格式兼容
              const [healthy, unhealthy] = data.datasets[0]?.data || [0, 0];
              setHealthData({ healthy, unhealthy, skipped: 0, excluded: 0 });
            }
            console.log('📊 [SSE] 端点健康图已更新');
          }
        }
    };

    document.addEventListener('chartUpdate', handleChartUpdate);
    return () => {
      document.removeEventListener('chartUpdate', handleChartUpdate);
    };
  }, [provider]);

  // 手动刷新
  const handleRefresh = () => {
    loadData(true);
  };

  // 计算统计数据
  // healthy 已包含跳过检测的端点（后端处理）
  // inScopeTotal: 参与检测的端点数（健康 + 异常），不含 excluded
  const inScopeTotal = healthData.healthy + healthData.unhealthy;
  // 健康率：基于参与检测的端点
  const healthPercent = inScopeTotal > 0 ? Math.round((healthData.healthy / inScopeTotal) * 100) : 0;

  // 半圆图数据：健康（绿色）中包含跳过（紫色）、异常（红色）
  // 为了在图中区分跳过检测的部分，将其单独显示
  const actualHealthy = healthData.healthy - healthData.skipped; // 真正检测通过的
  const chartData = [
    { name: '健康', value: actualHealthy, color: HEALTH_CONFIG.healthy.color },
    { name: '跳过检测', value: healthData.skipped, color: HEALTH_CONFIG.skipped.color },
    { name: '异常', value: healthData.unhealthy, color: HEALTH_CONFIG.unhealthy.color }
  ];

  // 确定健康状态的显示样式
  const getHealthStatus = () => {
    if (inScopeTotal === 0) return { text: '无数据', color: 'text-slate-400', bg: 'bg-slate-50' };
    if (healthPercent >= 90) return { text: '优秀', color: 'text-emerald-600', bg: 'bg-emerald-50' };
    if (healthPercent >= 70) return { text: '良好', color: 'text-amber-600', bg: 'bg-amber-50' };
    return { text: '警告', color: 'text-rose-600', bg: 'bg-rose-50' };
  };

  const status = getHealthStatus();

  return (
    <div className="bg-white p-6 rounded-2xl border border-slate-200/60 shadow-sm flex flex-col h-full">
      <div className="flex justify-between items-start mb-1">
        <div className="flex items-center space-x-2">
          <div className="p-1.5 bg-emerald-50 text-emerald-500 rounded-md">
            <Activity size={16} />
          </div>
          <h3 className="font-semibold text-slate-900">端点健康状态</h3>
        </div>
        <div className="flex items-center space-x-2">
          {/* Provider Tab 切换 */}
          <div className="flex items-center gap-1 bg-slate-100 rounded-lg p-0.5">
            <button
              onClick={() => setProvider('claude')}
              className={`px-2.5 py-1 text-xs font-medium rounded-md transition-colors ${
                provider === 'claude'
                  ? 'bg-white text-orange-700 shadow-sm'
                  : 'text-slate-500 hover:text-slate-700'
              }`}
            >
              Claude
            </button>
            <button
              onClick={() => setProvider('codex')}
              className={`px-2.5 py-1 text-xs font-medium rounded-md transition-colors ${
                provider === 'codex'
                  ? 'bg-white text-blue-700 shadow-sm'
                  : 'text-slate-500 hover:text-slate-700'
              }`}
            >
              Codex
            </button>
          </div>
          <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${status.bg} ${status.color}`}>
            {inScopeTotal > 0 ? `${healthPercent}%` : '-'} {status.text}
          </span>
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
      <p className="text-xs text-slate-500 mb-4">实时端点连通性监控</p>

      <div className="flex-1 min-h-[180px] flex items-center justify-center relative">
        {loading ? (
          <div className="flex items-center text-slate-400">
            <RefreshCw size={20} className="animate-spin mr-2" />
            加载中...
          </div>
        ) : (
          <>
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Pie
                  data={chartData}
                  startAngle={180}
                  endAngle={0}
                  innerRadius={55}
                  outerRadius={80}
                  paddingAngle={2}
                  dataKey="value"
                  animationBegin={0}
                  animationDuration={500}
                >
                  {chartData.map((entry, index) => (
                    <Cell
                      key={`cell-${index}`}
                      fill={entry.color}
                      stroke="none"
                    />
                  ))}
                </Pie>
              </PieChart>
            </ResponsiveContainer>
            <div className="absolute inset-0 top-8 flex flex-col items-center justify-center pointer-events-none">
              {inScopeTotal === 0 ? (
                <EyeOff size={28} className="text-slate-400" />
              ) : (
                <CheckCircle2
                  size={28}
                  className={healthPercent >= 70 ? 'text-emerald-500' : 'text-rose-500'}
                />
              )}
              <span className="text-2xl font-bold text-slate-900 mt-1">
                {healthData.healthy}/{inScopeTotal}
              </span>
              <span className="text-xs text-slate-400">健康/参与检测</span>
              {healthData.skipped > 0 && (
                <span className="text-[11px] mt-1" style={{ color: HEALTH_CONFIG.skipped.color }}>
                  含跳过 {healthData.skipped}
                </span>
              )}
            </div>
          </>
        )}
      </div>

      {/* 图例和详情 - 4列 */}
      {!loading && (
        <div className="grid grid-cols-4 gap-3 mt-2 pt-3 border-t border-slate-100">
          <div className="flex items-center justify-between">
            <div className="flex items-center text-xs text-slate-600">
              <span className="w-2.5 h-2.5 rounded-full bg-emerald-500 mr-2" />
              健康
            </div>
            <span className="font-mono text-sm font-semibold text-emerald-600">
              {healthData.healthy}
            </span>
          </div>
          <div className="flex items-center justify-between">
            <div className="flex items-center text-xs text-slate-600">
              <span className="w-2.5 h-2.5 rounded-full bg-rose-500 mr-2" />
              异常
            </div>
            <span className="font-mono text-sm font-semibold text-rose-600">
              {healthData.unhealthy}
            </span>
          </div>
          <div className="flex items-center justify-between">
            <div className="flex items-center text-xs text-slate-600">
              <span className="w-2.5 h-2.5 rounded-full mr-2" style={{ backgroundColor: HEALTH_CONFIG.skipped.color }} />
              跳过
            </div>
            <span className="font-mono text-sm font-semibold" style={{ color: HEALTH_CONFIG.skipped.color }}>
              {healthData.skipped}
            </span>
          </div>
          <div className="flex items-center justify-between">
            <div className="flex items-center text-xs text-slate-600">
              <span className="w-2.5 h-2.5 rounded-full mr-2" style={{ backgroundColor: HEALTH_CONFIG.excluded.color }} />
              不参与
            </div>
            <span className="font-mono text-sm font-semibold text-slate-500">
              {healthData.excluded}
            </span>
          </div>
        </div>
      )}
    </div>
  );
};

export default EndpointHealthChart;
