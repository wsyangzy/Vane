// ============================================
// 主 App 组件 - 概览入口
// 2025-11-28 (Updated 2026-02-09)
// ============================================

import { useState, Suspense, lazy, useCallback, useEffect, useRef } from 'react';
import Header from '@/app/layout/Header.jsx';
import { LoadingSpinner, ToastProvider, useToast, PortConflictDialog } from '@/shared/ui/index.js';
import useSSE from '@/shared/hooks/useSSE.js';
import { fetchRequests, isWailsEnvironment } from '@/services/http/api.js';
import { getSuspendedRequestsCount, getSuspendedEndpointCounts, getManualRetryRequestsCount, getManualRetryChannelCounts, getCodexSuspendedCount, getPortConflictStatus, resolvePortConflict, subscribeToEvent } from '@/services/wails/index.js';
import { buildFailureToast, buildSuspensionToast, shouldNotifyFailure } from '@/features/requests/utils/failureSummary.js';

// 懒加载页面组件
const OverviewPage = lazy(() => import('@/features/overview/index.jsx'));
const ChannelsPage = lazy(() => import('@/features/providers/claude/index.jsx'));
const CodexPage = lazy(() => import('@/features/providers/codex/index.jsx'));
// 组管理页面已移除，配置简化后不再需要独立的组管理功能
// const GroupsPage = lazy(() => import('@/features/groups/index.jsx'));
const RequestsPage = lazy(() => import('@/features/requests/index.jsx'));
const PrivacyPage = lazy(() => import('@/features/privacy/index.jsx'));
const PricingPage = lazy(() => import('@/features/pricing/index.jsx'));
// 配置页面改为设置页面（可编辑）
const SettingsPage = lazy(() => import('@/features/settings/index.jsx'));
// 保留只读配置页面用于调试
const ConfigPage = lazy(() => import('@/features/config/index.jsx'));
// 系统日志页面
const LogsPage = lazy(() => import('@/features/log-viewer/index.jsx'));

// ============================================
// App 组件
// ============================================

const GlobalAlerts = () => {
  const toast = useToast();
  const initializedRef = useRef(false);
  const inFlightRef = useRef(false);
  const lastFailureIdRef = useRef('');
  const lastSuspendedCountRef = useRef(0);
  const lastManualRetryCountRef = useRef(0);
  // Codex 挂起数（独立于 Claude 挂起数，事件驱动）
  const lastCodexSuspendedCountRef = useRef(0);

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

      // 挂起 / 等待重试从 0 变为非 0 时提示一次，并带上具体端点或渠道
      const manualJustStarted = manualRetryCount > 0 && lastManualRetryCountRef.current === 0;
      const suspendJustStarted = manualRetryCount === 0 && suspendedCount > 0 && lastSuspendedCountRef.current === 0;
      if (manualJustStarted || suspendJustStarted) {
        const [endpointCounts, channelCounts] = await Promise.all([
          suspendJustStarted ? getSuspendedEndpointCounts().catch(() => ({})) : {},
          manualJustStarted ? getManualRetryChannelCounts().catch(() => ({})) : {}
        ]);
        const suspension = buildSuspensionToast({ suspendedCount, endpointCounts, manualRetryCount, channelCounts });
        if (suspension) {
          toast.show(suspension.message, { type: suspension.type, title: suspension.title, duration: 6000 });
        }
      }

      if (latestFailureId && latestFailureId !== lastFailureIdRef.current) {
        if (shouldNotifyFailure(failureReason)) {
          const failure = buildFailureToast(latestFailed);
          toast.show(failure.message, { type: failure.type, title: failure.title, duration: 8000 });
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

    // Codex 挂起数：初始化 + 事件订阅（事件驱动比轮询更及时）
    getCodexSuspendedCount()
      .then((count) => {
        lastCodexSuspendedCountRef.current = Number(count) || 0;
      })
      .catch(() => {});

    const handleCodexSuspension = (data) => {
      const count = Number(data?.suspended_count) || 0;
      const prev = lastCodexSuspendedCountRef.current;
      if (count > 0 && prev === 0) {
        toast.show([
          `${count} 个 Codex 请求已挂起，当前没有可承接的端点`,
          '请在 Codex 渠道页重新激活渠道或端点'
        ], { type: 'warning', title: 'Codex 请求等待激活', duration: 6000 });
      } else if (count === 0 && prev > 0) {
        toast.success('Codex 挂起请求已全部恢复处理', 'Codex 已恢复');
      }
      lastCodexSuspendedCountRef.current = count;
    };
    const unsubscribeCodexSuspension = subscribeToEvent('codex-provider:suspension', handleCodexSuspension);

    // 后端主动推送的通知（数据库占用、数据迁移、渠道间转移回滚等），此前从未被前端消费
    const handleNotification = (data) => {
      const level = ['success', 'error', 'warning', 'info'].includes(data?.level) ? data.level : 'info';
      const message = data?.message || '';
      const title = data?.title || '';
      if (!message && !title) return;
      toast.show(message || title, { type: level, title: message ? title : undefined, duration: level === 'error' ? 8000 : 6000 });
    };
    const unsubscribeNotification = subscribeToEvent('notification', handleNotification);
    const unsubscribeError = subscribeToEvent('error', (data) => handleNotification({ ...data, level: 'error' }));

    return () => {
      clearInterval(timer);
      if (typeof unsubscribeCodexSuspension === 'function') unsubscribeCodexSuspension();
      if (typeof unsubscribeNotification === 'function') unsubscribeNotification();
      if (typeof unsubscribeError === 'function') unsubscribeError();
    };
  }, [pollAlerts, toast]);

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

  // 端口冲突状态
  const [portConflict, setPortConflict] = useState({
    hasConflict: false,
    conflictPort: 0,
    errorMessage: ''
  });

  // 独立的对话框显示状态（与冲突状态分离，避免切换成功后被自动关闭）
  const [showPortConflictDialog, setShowPortConflictDialog] = useState(false);

  // 检查端口冲突状态
  const checkPortConflict = useCallback(async () => {
    if (!isWailsEnvironment()) return;

    try {
      const status = await getPortConflictStatus();
      const hasConflict = status.has_conflict || false;
      setPortConflict({
        hasConflict,
        conflictPort: status.conflict_port || 0,
        errorMessage: status.error_message || ''
      });
      // 只有存在冲突时才显示对话框
      if (hasConflict) {
        setShowPortConflictDialog(true);
      }
    } catch (error) {
      console.warn('检查端口冲突状态失败:', error);
    }
  }, []);

  // 初始化时检查端口冲突
  /* eslint-disable react-hooks/set-state-in-effect -- 初始化数据加载是标准模式 */
  useEffect(() => {
    checkPortConflict();
  }, [checkPortConflict]);
  /* eslint-enable react-hooks/set-state-in-effect */

  // 处理端口冲突解决
  const handlePortConflictRetry = useCallback(async () => {
    return await resolvePortConflict('retry');
  }, []);

  const handlePortConflictSwitch = useCallback(async () => {
    return await resolvePortConflict('switch');
  }, []);

  // 用户手动关闭对话框
  const handlePortConflictDismiss = useCallback(() => {
    setShowPortConflictDialog(false);
    setPortConflict(prev => ({ ...prev, hasConflict: false }));
  }, []);

  // 处理 system:status 事件
  const handleStatusUpdate = useCallback((data) => {
    if (data?.proxy_running !== undefined) {
      setProxyStatus({
        running: data.proxy_running,
        port: data.proxy_port || 0,
        host: data.proxy_host || '127.0.0.1'
      });
      // 注意：不再自动关闭对话框，让用户有时间复制地址
    }
  }, []);

  // SSE 连接状态（用于全局状态指示）
  const { connectionStatus } = useSSE(handleStatusUpdate, { events: 'status' });

  // 渲染当前页面
  const renderPage = () => {
    const pages = {
      overview: <OverviewPage />,
      channels: <ChannelsPage />,
      codex: <CodexPage />,
      // 组管理页面已移除
      // groups: <GroupsPage />,
      requests: <RequestsPage />,
      privacy: <PrivacyPage />,
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

  // Only the area below Header scrolls; request/log pages own their inner scrolling.
  const contentClass = isRequestsPage || isLogsPage
    ? 'app-content relative z-10 flex min-h-0 flex-1 flex-col overflow-hidden'
    : 'app-content relative z-10 min-h-0 flex-1 overflow-y-auto';

  const mainClass = isRequestsPage || isLogsPage
    ? 'flex min-h-0 flex-1 flex-col w-full px-6 pt-6 pb-4'
    : 'max-w-7xl mx-auto px-6 pt-8 pb-20';

  return (
    <ToastProvider>
      <GlobalAlerts />

      {/* 端口冲突对话框 - 使用独立的显示状态，避免成功后被自动关闭 */}
      <PortConflictDialog
        isOpen={showPortConflictDialog}
        conflictPort={portConflict.conflictPort}
        errorMessage={portConflict.errorMessage}
        onRetry={handlePortConflictRetry}
        onSwitch={handlePortConflictSwitch}
        onDismiss={handlePortConflictDismiss}
      />

      <div className="flex h-dvh flex-col overflow-hidden bg-[#FAFAFA] font-sans text-slate-900">
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
        <div className={contentClass}>
          <main className={mainClass}>
            <Suspense fallback={<LoadingSpinner text="加载页面..." />}>
              {renderPage()}
            </Suspense>
          </main>
        </div>
      </div>
    </ToastProvider>
  );
}

export default App;
