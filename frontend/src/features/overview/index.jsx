// ============================================
// Overview 页面 - 监控面板
// 2025-11-28
// ============================================

import { LayoutDashboard } from 'lucide-react';
import { LoadingSpinner } from '@/shared/ui/index.js';
import useOverviewData from '@/features/overview/hooks/useOverviewData.js';

// 子组件
import KPICardsGrid from './components/KPICardsGrid.jsx';
import ConnectionStats from './components/ConnectionStats.jsx';
import RequestTrendChart from './components/RequestTrendChart.jsx';
import TokenDistributionChart from './components/TokenDistributionChart.jsx';
import EndpointHealthChart from './components/EndpointHealthChart.jsx';
import TokenCostChart from './components/TokenCostChart.jsx';
import ResponseTimeChart from './components/ResponseTimeChart.jsx';
import ConnectionActivityChart from './components/ConnectionActivityChart.jsx';

// ============================================
// Overview 主页面
// ============================================

const OverviewPage = () => {
  const { data, isInitialized } = useOverviewData();
  const uptimeSeconds = data?.status?.uptime_seconds || 0;

  if (data.loading && !isInitialized) {
    return <LoadingSpinner text="加载监控数据..." />;
  }

  return (
    <div className="animate-fade-in">
      {/* 页面标题 */}
      <div className="mb-3 flex min-h-9 flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="shrink-0 rounded-lg bg-slate-900 p-1.5 text-white shadow-sm">
            <LayoutDashboard className="h-5 w-5" />
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            <h1 className="text-xl font-bold text-slate-900">概览</h1>
            <p className="text-xs text-slate-500">API 转发与用量监控</p>
          </div>
        </div>
      </div>

      {/* KPI 卡片 */}
      <KPICardsGrid data={data} />

      {/* 图表区域 */}
      <div className="space-y-4">
        {/* 请求趋势 - 全宽 */}
        <RequestTrendChart uptimeSeconds={uptimeSeconds} />

        {/* 性能指标 - 响应时间 & 连接活动 */}
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <ResponseTimeChart uptimeSeconds={uptimeSeconds} />
          <ConnectionActivityChart uptimeSeconds={uptimeSeconds} />
        </div>

        {/* Token 成本 - 全宽 */}
        <TokenCostChart />

        {/* 资源状- Token 分布 & 端点健康 */}
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <TokenDistributionChart />
          <EndpointHealthChart />
        </div>

        {/* 连接统计 */}
        <ConnectionStats connections={data.connections} />
      </div>
    </div>
  );
};

export default OverviewPage;
