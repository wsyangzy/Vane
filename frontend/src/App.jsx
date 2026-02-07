// ============================================
// 主 App 组件 - 概览入口
// 2025-11-28 (Updated 2025-12-12)
// ============================================

import { useState, Suspense, lazy, useCallback, useEffect, useRef } from 'react';
import Header from '@components/layout/Header.jsx';
import { LoadingSpinner, ToastProvider, useToast } from '@components/ui';
import useSSE from '@hooks/useSSE.js';
import { fetchRequests, isWailsEnvironment } from '@utils/api.js';
import { getSuspendedRequestsCount, getManualRetryRequestsCount } from '@utils/wailsApi.js';

// 懒加载页面组件
const OverviewPage = lazy(() => import('@pages/overview/index.jsx'));
const ChannelsPage = lazy(() => import('@pages/channels/index.jsx'));
// 组管理页面已移除，配置简化后不再需要独立的组管理功能
// const GroupsPage = lazy(() => import('@pages/groups/index.jsx'));
const RequestsPage = lazy(() => import('@pages/requests/index.jsx'));
const PricingPage = lazy(() => import('@pages/pricing/index.jsx'));
// 配置页面改为设置页面（可编辑）
const SettingsPage = lazy(() => import('@pages/settings/index.jsx'));
// 保留只读配置页面用于调试
const ConfigPage = lazy(() => import('@pages/config/index.jsx'));
// 系统日志页面
const LogsPage = lazy(() => import('@pages/log-viewer/index.jsx'));

// ============================================
// App 组件
// ============================================

const shouldNotifyFailoverFailure = (failureReason) => {
  if (!failureReason || typeof failureReason !== 'string') {
    return false;
  }
  const raw = failureReason.trim();
  if (!raw) return false;

  const lower = raw.toLowerCase();
  const keywords = [
    'all_channels_unavailable',
    'endpoint_exhausted',
    'max_loops_exceeded',
    'max_restarts_exceeded'
  ];
  const hints = [
    '所有渠道均已尝试',
    '没有可用的故障转移渠道',
    '当前渠道所有端点均不可用',
    '最大循环轮数',
    '最大重启次数',
    'exceeded maximum restart attempts'
  ];
  return keywords.some(keyword => lower.includes(keyword)) || hints.some(hint => raw.includes(hint));
};

const buildFailoverFailureMessage = (failureReason) => {
  if (!failureReason || typeof failureReason !== 'string') {
    return '服务暂时不可用，请稍后重试';
  }

  if (failureReason.includes('all_channels_unavailable') || failureReason.includes('所有渠道均已尝试')) {
    return '所有渠道均已尝试，服务暂时不可用';
  }
  if (failureReason.includes('endpoint_exhausted') || failureReason.includes('当前渠道所有端点均不可用')) {
    return '当前渠道所有端点均不可用';
  }
  if (failureReason.includes('max_loops_exceeded') || failureReason.includes('最大循环轮数')) {
    return '已达到最大循环轮数，等待手动处理';
  }
  if (failureReason.includes('max_restarts_exceeded') || failureReason.includes('最大重启次数')) {
    return '已达到最大重启次数，请求终止';
  }

  const trimmed = failureReason.trim();
  return trimmed.length > 120 ? `${trimmed.slice(0, 120)}...` : trimmed;
};

const GlobalAlerts = () => {
  const toast = useToast();
  const initializedRef = useRef(false);
  const inFlightRef = useRef(false);
  const lastFailureIdRef = useRef('');
  const lastSuspendedCountRef = useRef(0);
  const lastManualRetryCountRef = useRef(0);

  const pollAlerts = useCallback(async () => {
    if (!isWailsEnvironment() || inFlightRef.current) {
      return;
    }
    inFlightRef.current = true;

    try {
      const [suspendedCount, manualRetryCount, failedData] = await Promise.all([
        getSuspendedRequestsCount().catch(() => 0),
        getManualRetryRequestsCount().catch(() => 0),
        fetchRequests({ status: 'failed', page: 1, pageSize: 1 }).catch(() => null)
      ]);

      const latestFailed = failedData?.requests?.[0];
      const latestFailureId = latestFailed?.rawRequestId || latestFailed?.requestId || latestFailed?.id || '';
      const failureReason = latestFailed?.failure_reason || latestFailed?.failureReason || '';

      if (!initializedRef.current) {
        lastSuspendedCountRef.current = suspendedCount || 0;
        lastManualRetryCountRef.current = manualRetryCount || 0;
        if (latestFailureId) {
          lastFailureIdRef.current = latestFailureId;
        }
        initializedRef.current = true;
        return;
      }

      if (manualRetryCount > 0 && lastManualRetryCountRef.current === 0) {
        toast.warning(`已有 ${manualRetryCount} 个请求达到最大循环轮数，等待手动重试`, '请求等待重试');
      } else if (manualRetryCount === 0 && suspendedCount > 0 && lastSuspendedCountRef.current === 0) {
        toast.info(`已有 ${suspendedCount} 个请求挂起，等待端点恢复或渠道切换`, '请求已挂起');
      }

      if (latestFailureId && latestFailureId !== lastFailureIdRef.current) {
        if (shouldNotifyFailoverFailure(failureReason)) {
          // 根据失败类型选择不同的提示级别
          // max_loops_exceeded 和 max_restarts_exceeded 需要用户手动处理，使用 warning 级别
          // 避免与真正的错误混淆（如并发场景下其他请求还在正常运行）
          const isManualInterventionNeeded =
            failureReason.includes('max_loops_exceeded') ||
            failureReason.includes('max_restarts_exceeded') ||
            failureReason.includes('最大循环轮数') ||
            failureReason.includes('最大重启次数');

          if (isManualInterventionNeeded) {
            toast.warning(buildFailoverFailureMessage(failureReason), '需要处理');
          } else {
            toast.error(buildFailoverFailureMessage(failureReason), '请求失败');
          }
        }
        lastFailureIdRef.current = latestFailureId;
      }

      lastSuspendedCountRef.current = suspendedCount || 0;
      lastManualRetryCountRef.current = manualRetryCount || 0;
    } finally {
      inFlightRef.current = false;
    }
  }, [toast]);

  useEffect(() => {
    if (!isWailsEnvironment()) {
      return;
    }
    pollAlerts();
    const timer = setInterval(pollAlerts, 4000);
    return () => clearInterval(timer);
  }, [pollAlerts]);

  return null;
};

function App() {
  const [activeTab, setActiveTab] = useState('overview');
  // 代理网关真实状态（来自后端 system:status 事件）
  const [proxyStatus, setProxyStatus] = useState({
    running: false,
    port: 0,
    host: '127.0.0.1'
  });

  // 处理 system:status 事件
  const handleStatusUpdate = useCallback((data) => {
    if (data?.proxy_running !== undefined) {
      setProxyStatus({
        running: data.proxy_running,
        port: data.proxy_port || 0,
        host: data.proxy_host || '127.0.0.1'
      });
    }
  }, []);

  // SSE 连接状态（用于全局状态指示）
  const { connectionStatus } = useSSE(handleStatusUpdate, { events: 'status' });

  // 渲染当前页面
  const renderPage = () => {
    const pages = {
      overview: <OverviewPage />,
      channels: <ChannelsPage />,
      // 组管理页面已移除
      // groups: <GroupsPage />,
      requests: <RequestsPage />,
      pricing: <PricingPage />,
      // 设置页面（可编辑）
      settings: <SettingsPage />,
      // 保留只读配置页面
      config: <ConfigPage />,
      // 系统日志页面
      logs: <LogsPage />
    };

    return pages[activeTab] || <OverviewPage />;
  };

  const isLogsPage = activeTab === 'logs';
  const isRequestsPage = activeTab === 'requests';

  // 日志/请求页面使用全高布局，避免外部滚动
  const containerClass = isLogsPage
    ? 'h-screen bg-[#FAFAFA] font-sans text-slate-900 overflow-hidden flex flex-col'
    : isRequestsPage
      ? 'h-screen bg-[#FAFAFA] font-sans text-slate-900 overflow-hidden flex flex-col'
      : 'min-h-screen bg-[#FAFAFA] font-sans text-slate-900 pb-20';

  const mainClass = isRequestsPage || isLogsPage
    ? 'flex-1 min-h-0 w-full px-6 pt-6 relative z-10'
    : 'max-w-7xl mx-auto px-6 pt-8 relative z-10';

  return (
    <ToastProvider>
      <GlobalAlerts />
      <div className={containerClass}>
        {/* 背景纹理 */}
        <div
          className="fixed inset-0 pointer-events-none opacity-[0.4]"
          style={{
            backgroundImage: 'radial-gradient(#cbd5e1 1px, transparent 1px)',
            backgroundSize: '24px 24px'
          }}
        />

        {/* 顶部导航 */}
        <Header
          activeTab={activeTab}
          onTabChange={setActiveTab}
          connectionStatus={connectionStatus}
          proxyStatus={proxyStatus}
        />

        {/* 主内容区 */}
        <main className={mainClass}>
          <Suspense fallback={<LoadingSpinner text="加载页面..." />}>
            {renderPage()}
          </Suspense>
        </main>
      </div>
    </ToastProvider>
  );
}

export default App;
