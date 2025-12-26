// ============================================
// 端点健康状态图组件
// 2025-11-28
// ============================================

import { useState, useEffect, useCallback, useRef } from 'react';
import { RefreshCw, Activity, CheckCircle2, XCircle, Clock, ShieldOff } from 'lucide-react';
import {
  PieChart,
  Pie,
  Cell,
  ResponsiveContainer
} from 'recharts';
import { fetchEndpointHealthData } from '@utils/api.js';

// 健康状态配置
const HEALTH_CONFIG = {
  healthy: { name: '健康', color: '#10b981', icon: CheckCircle2 },
  unhealthy: { name: '异常', color: '#ef4444', icon: XCircle },
  unchecked: { name: '未检测', color: '#94a3b8', icon: Clock },
  skipped: { name: '跳过检测', color: '#a855f7', icon: ShieldOff }
};

const EndpointHealthChart = () => {
  const [healthData, setHealthData] = useState({ healthy: 0, unhealthy: 0, unchecked: 0, skipped: 0 });
  const [loading, setLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const refreshIntervalRef = useRef(null);

  // 加载数据
  const loadData = useCallback(async (showRefreshing = false) => {
    if (showRefreshing) {
      setIsRefreshing(true);
    }
    try {
      const data = await fetchEndpointHealthData();
      setHealthData({
        healthy: data.healthy || 0,
        unhealthy: data.unhealthy || 0,
        unchecked: data.unchecked || 0,
        skipped: data.skipped || 0
      });
    } catch (error) {
      console.error('加载端点健康数据失败:', error);
    } finally {
      setLoading(false);
      setIsRefreshing(false);
    }
  }, []);

  // 初始加载
  useEffect(() => {
    loadData();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // 定时刷新（每 30 秒，健康状态需要更频繁更新）
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
      const { chart_type, data } = event.detail || {};
        if (chart_type === 'endpoint_health' || chart_type === 'endpointHealth') {
          if (data) {
            // 处理不同格式的数据
            if (data.healthy !== undefined) {
              setHealthData({
                healthy: data.healthy || 0,
                unhealthy: data.unhealthy || 0,
                unchecked: data.unchecked || 0,
                skipped: data.skipped || 0
              });
            } else if (data.labels && data.datasets) {
              // Chart.js 格式
              const [healthy, unhealthy] = data.datasets[0]?.data || [0, 0];
              setHealthData({ healthy, unhealthy, unchecked: 0, skipped: 0 });
            }
            console.log('📊 [SSE] 端点健康图已更新');
          }
        }
    };

    document.addEventListener('chartUpdate', handleChartUpdate);
    return () => {
      document.removeEventListener('chartUpdate', handleChartUpdate);
    };
  }, []);

  // 手动刷新
  const handleRefresh = () => {
    loadData(true);
  };

  // 计算统计数据
  const checkedTotal = healthData.healthy + healthData.unhealthy;
  const total = healthData.healthy + healthData.unhealthy + healthData.unchecked + healthData.skipped;
  const healthPercent = checkedTotal > 0 ? Math.round((healthData.healthy / checkedTotal) * 100) : 0;

  // 图表数据（半圆仪表盘）
  const chartData = [
    { name: '健康', value: healthData.healthy, color: HEALTH_CONFIG.healthy.color },
    { name: '异常', value: healthData.unhealthy, color: HEALTH_CONFIG.unhealthy.color },
    { name: '未检测', value: healthData.unchecked, color: HEALTH_CONFIG.unchecked.color },
    { name: '跳过检测', value: healthData.skipped, color: HEALTH_CONFIG.skipped.color }
  ];

  // 确定健康状态的显示样式
  const getHealthStatus = () => {
    if (total === 0) return { text: '无数据', color: 'text-slate-400', bg: 'bg-slate-50' };
    if (checkedTotal === 0) return { text: '未检测', color: 'text-slate-600', bg: 'bg-slate-50' };
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
          <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${status.bg} ${status.color}`}>
            {checkedTotal > 0 ? `${healthPercent}%` : '-'} {status.text}
          </span>
          <button
            onClick={handleRefresh}
            disabled={isRefreshing}
            className="p-1.5 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-md transition-colors disabled:opacity-50"
            title="刷新数据"
          >
            <RefreshCw size={14} className={isRefreshing ? 'animate-spin' : ''} />
          </button>
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
              {checkedTotal === 0 ? (
                <Clock size={28} className="text-slate-400" />
              ) : (
                <CheckCircle2
                  size={28}
                  className={healthPercent >= 70 ? 'text-emerald-500' : 'text-rose-500'}
                />
              )}
              <span className="text-2xl font-bold text-slate-900 mt-1">
                {healthData.healthy}/{checkedTotal}
              </span>
              <span className="text-xs text-slate-400">已检测端点在线</span>
              {healthData.unchecked > 0 && (
                <span className="text-[11px] text-slate-400 mt-1">未检测 {healthData.unchecked}</span>
              )}
              {healthData.skipped > 0 && (
                <span className="text-[11px] text-slate-400 mt-1">跳过检测 {healthData.skipped}</span>
              )}
            </div>
          </>
        )}
      </div>

      {/* 图例和详情 */}
      {!loading && (
        <div className="grid grid-cols-4 gap-4 mt-2 pt-3 border-t border-slate-100">
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
              <span className="w-2.5 h-2.5 rounded-full bg-slate-400 mr-2" />
              未检测
            </div>
            <span className="font-mono text-sm font-semibold text-slate-500">
              {healthData.unchecked}
            </span>
          </div>
          <div className="flex items-center justify-between">
            <div className="flex items-center text-xs text-slate-600">
              <span className="w-2.5 h-2.5 rounded-full mr-2" style={{ backgroundColor: HEALTH_CONFIG.skipped.color }} />
              跳过检测
            </div>
            <span className="font-mono text-sm font-semibold" style={{ color: HEALTH_CONFIG.skipped.color }}>
              {healthData.skipped}
            </span>
          </div>
        </div>
      )}
    </div>
  );
};

export default EndpointHealthChart;
