// ============================================
// Requests 页面 - 请求追踪（重构版）
// 请求追踪与统一调度方案入口
// ============================================

import { useState, useEffect, useEffectEvent, useCallback, useMemo, useRef } from 'react';
import { ListTree } from 'lucide-react';
import { useToast } from '@/shared/ui/index.js';
import { fetchRequests, fetchModels, fetchUsageStats, fetchGroups } from '@/services/http/api.js';
import { getChannels, getCodexChannels, isWailsEnvironment } from '@/services/wails/index.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';
import { useFilters } from './hooks/useFilters.js';
import { useColumnConfig } from './hooks/useColumnConfig.js';
import { useTimeRange } from './hooks/useTimeRange.js';
import { useAutoRefresh } from './hooks/useAutoRefresh.js';
import { FiltersPanel, StatsOverview, RequestsTable, Toolbar, RequestDetailModal } from './components/index.js';
import RequestRecoveryDialog from './components/RequestRecoveryDialog.jsx';
import { DEFAULT_FILTERS, PAGINATION_CONFIG } from './utils/constants.js';
import { buildRequestFilterResources } from './utils/filterResources.js';
import { readRequestDispatchTrace } from './utils/dispatchTrace.js';
import { isRequestInProgress } from './utils/requestOutcome.js';
import { collectRecoveryGroups } from './utils/requestRecovery.js';

// ============================================
// Requests 页面
// ============================================

const RequestsPage = ({ initialProvider = '' }) => {
  // ==================== 状态管理 ====================

  // 数据状态
  const [requests, setRequests] = useState([]);
  const [stats, setStats] = useState(null);
  const [claudeStats, setClaudeStats] = useState(null); // Claude 提供商统计
  const [codexStats, setCodexStats] = useState(null);   // Codex 提供商统计
  const [models, setModels] = useState([]);
  const [claudeChannels, setClaudeChannels] = useState([]);
  const [codexChannels, setCodexChannels] = useState([]); // Codex 渠道列表
  const [loading, setLoading] = useState(true);
  const toast = useToast();

  // 面板状态
  const [isFilterOpen, setIsFilterOpen] = useState(false);
  const [isViewConfigOpen, setIsViewConfigOpen] = useState(false);

  // 详情模态框状态
  const [selectedRequest, setSelectedRequest] = useState(null);
  const [isDetailModalOpen, setIsDetailModalOpen] = useState(false);
  const [detailInitialTab, setDetailInitialTab] = useState('overview');
  const detailReturnFocusRef = useRef(null);
  const requestPageRef = useRef(null);
  const [openRecoveryGroups, setOpenRecoveryGroups] = useState(null);

  // 筛选器 Hook
  const {
    filters,
    updateFilters,
    resetFilters,
    hasActiveFilters,
    buildQueryParams
  } = useFilters({ provider: initialProvider });

  // 列配置 Hook
  const {
    visibleColumns,
    toggleColumn,
    resetColumns,
    allColumns: columnConfigs
  } = useColumnConfig();

  // 分页状态在使用它的筛选回调之前声明。
  const [pagination, setPagination] = useState({
    page: 1,
    pageSize: PAGINATION_CONFIG.DEFAULT_PAGE_SIZE,
    total: 0,
    totalPages: 1
  });

  // 时间范围 Hook
  const { activeRange, selectRange, clearRange } = useTimeRange((timeRange) => {
    setLoading(true);
    updateFilters(timeRange);
    setPagination(prev => ({ ...prev, page: 1 }));
  });

  // 快捷时间档位单独展示；高级筛选中的自定义时间仍可一键清除。
  const hasAdvancedFilters = hasActiveFilters && (
    !activeRange || Object.entries(DEFAULT_FILTERS).some(([key, value]) => (
      key !== 'startDate' && key !== 'endDate' && (Array.isArray(value) ? filters[key]?.length > 0 : filters[key] !== value)
    ))
  );

  const filterResources = useMemo(
    () => buildRequestFilterResources(claudeChannels, codexChannels),
    [claudeChannels, codexChannels]
  );
  const dispatchResources = useMemo(() => [
    ...claudeChannels.map(channel => ({ ...channel, provider: 'claude' })),
    ...codexChannels.map(channel => ({ ...channel, provider: 'codex' })),
  ], [claudeChannels, codexChannels]);
  const displayRequests = useMemo(() => requests.map(request => ({
    ...request, dispatchTrace: readRequestDispatchTrace(request, dispatchResources)
  })), [requests, dispatchResources]);
  const detailRequestId = selectedRequest?.rawRequestId || selectedRequest?.requestId;
  const detailIsActive = isDetailModalOpen && isRequestInProgress(selectedRequest);
  const updateDetailRequest = useEffectEvent(next => {
    setSelectedRequest({ ...next, dispatchTrace: readRequestDispatchTrace(next, dispatchResources) });
  });
  const recoveryGroups = useMemo(() => isWailsEnvironment() ? collectRecoveryGroups(displayRequests) : [], [displayRequests]);

  // ==================== 数据加载 ====================

  const loadData = useCallback((silent = false) => {
    const isSilent = silent === true;
    const queryParams = buildQueryParams();

    // 为stats API添加默认时间范围（30天），避免无数据问题
    const statsParams = {
      ...queryParams,
      period: '30d'
    };

    // 渠道资源仅用于请求筛选，默认方案由独立入口管理。
    return Promise.all([
      fetchRequests({
        ...queryParams,
        page: pagination.page,
        pageSize: pagination.pageSize
      }),
      fetchUsageStats(statsParams),
      fetchUsageStats({ ...statsParams, provider: 'claude' }).catch(() => null),
      fetchUsageStats({ ...statsParams, provider: 'codex' }).catch(() => null),
      fetchModels(),
      getCodexChannels().catch(() => []), // Codex 渠道获取失败不影响主流程
      isWailsEnvironment() ? getChannels() : fetchGroups().then((data) => data?.groups || [])
    ]).then(([requestsData, statsData, claudeStatsData, codexStatsData, modelsData, codexChannelsData, claudeChannelsData]) => {
      setRequests(requestsData.requests);
      setPagination(prev => ({
        ...prev,
        total: requestsData.total,
        totalPages: requestsData.totalPages
      }));

      // 解包stats数据：后端返回 {success: true, data: {...}}
      const statsDataUnpacked = statsData?.data || statsData;
      setStats(statsDataUnpacked);

      // 按 Provider 分组统计
      const claudeUnpacked = claudeStatsData?.data || claudeStatsData;
      setClaudeStats(claudeUnpacked || null);
      const codexUnpacked = codexStatsData?.data || codexStatsData;
      setCodexStats(codexUnpacked || null);

      setModels(Array.isArray(modelsData) ? modelsData : []);

      // Codex 渠道列表（用于在渠道筛选下拉框中合并展示）
      setCodexChannels(Array.isArray(codexChannelsData) ? codexChannelsData : []);

      setClaudeChannels(Array.isArray(claudeChannelsData) ? claudeChannelsData : []);
    }).catch((err) => {
      console.error('加载请求追踪数据失败:', err);
      if (!isSilent) {
        toast.error(getErrorMessage(err, '请求数据加载失败'), '请求数据加载失败');
      }
    }).finally(() => {
      // 只有手动刷新才会改变 loading 状态
      if (!isSilent) {
        setLoading(false);
      }
    });
  }, [buildQueryParams, pagination.page, pagination.pageSize, toast]);

  const reloadData = useCallback((silent = false) => {
    if (silent !== true) setLoading(true);
    return loadData(silent);
  }, [loadData]);

  // 自动刷新 Hook (必须在 loadData 定义之后)
  const autoRefresh = useAutoRefresh(reloadData);

  // ==================== 事件处理 ====================

  // 筛选面板切换
  const handleFilterToggle = () => {
    setIsFilterOpen(!isFilterOpen);
    setIsViewConfigOpen(false); // 关闭列配置
  };

  // 列配置面板切换
  const handleViewConfigToggle = (nextOpen) => {
    const resolvedOpen = typeof nextOpen === 'boolean' ? nextOpen : !isViewConfigOpen;
    setIsViewConfigOpen(resolvedOpen);
    if (resolvedOpen) {
      setIsFilterOpen(false); // 关闭筛选面板
    }
  };

  // 应用筛选
  const handleApplyFilters = (nextFilters) => {
    setLoading(true);
    if (nextFilters) {
      updateFilters(nextFilters);
      clearRange();
    }
    setPagination(prev => ({ ...prev, page: 1 }));
  };

  const handleClearFilters = () => {
    resetFilters();
    selectRange('today');
    setIsFilterOpen(false);
  };

  // 页码变更
  const handlePageChange = (newPage) => {
    if (newPage === pagination.page) return;
    setLoading(true);
    setPagination(prev => ({ ...prev, page: newPage }));
  };

  // 每页条数变更
  const handlePageSizeChange = (newPageSize) => {
    if (newPageSize === pagination.pageSize) return;
    setLoading(true);
    setPagination(prev => ({
      ...prev,
      pageSize: newPageSize,
      page: 1 // 重置到第一页
    }));
  };

  // 调度入口直接打开链路页签，普通请求详情从概览开始。
  const handleOpenDetails = (request, initialTab = 'overview') => {
    detailReturnFocusRef.current = document.activeElement;
    setDetailInitialTab(initialTab);
    setSelectedRequest(request);
    setIsDetailModalOpen(true);
  };

  // 关闭详情模态框
  const handleCloseDetailModal = useCallback(() => {
    setIsDetailModalOpen(false);
    setSelectedRequest(null);
    if (detailReturnFocusRef.current?.isConnected) detailReturnFocusRef.current.focus();
  }, []);

  // ==================== 生命周期 ====================

  useEffect(() => {
    loadData();
  }, [loadData]);

  useEffect(() => {
    if (!detailIsActive || !detailRequestId) return undefined;
    let stopped = false;
    let reading = false;
    let timer;
    let lastSnapshot;

    const poll = async () => {
      if (stopped || reading || document.hidden) return;
      reading = true;
      try {
        const { requests } = await fetchRequests({ request_id: detailRequestId, pageSize: 1 });
        const next = requests.find(row => row.rawRequestId === detailRequestId);
        if (stopped || !next) return;
        const snapshot = JSON.stringify(next);
        if (snapshot !== lastSnapshot) {
          lastSnapshot = snapshot;
          updateDetailRequest(next);
        }
        if (!isRequestInProgress(next)) stopped = true;
      } catch {
        // Keep the last snapshot while the connection or archive catches up.
      } finally {
        reading = false;
        if (!stopped && !document.hidden) timer = window.setTimeout(poll, 400);
      }
    };
    const onVisibilityChange = () => {
      window.clearTimeout(timer);
      if (!document.hidden) void poll();
    };
    document.addEventListener('visibilitychange', onVisibilityChange);
    void poll();
    return () => {
      stopped = true;
      window.clearTimeout(timer);
      document.removeEventListener('visibilitychange', onVisibilityChange);
    };
  }, [detailIsActive, detailRequestId]);

  // ==================== 渲染 ====================

  return (
    <div ref={requestPageRef} className="relative flex min-h-0 min-w-0 flex-1 flex-col gap-3 animate-in fade-in slide-in-from-bottom-2 duration-500">
      {/* 页面标题 & 工具栏 */}
      <div className="relative z-30 flex min-h-9 shrink-0 items-center gap-4">
        {/* 页面标题 */}
        <div className="flex min-h-9 shrink-0 self-start items-center gap-2.5">
          <div className="shrink-0 rounded-lg bg-slate-900 p-1.5 text-white shadow-sm">
            <ListTree className="h-5 w-5" />
          </div>
          <h1 className="flex items-center gap-2 text-xl font-bold tracking-tight text-slate-900">请求追踪{filters.provider && <span className="rounded-md border border-indigo-100 bg-indigo-50 px-1.5 py-0.5 text-[11px] font-medium tracking-normal text-indigo-600">{filters.provider === 'codex' ? 'Codex' : 'Claude'}</span>}</h1>
        </div>

        {/* 工具栏 */}
        <Toolbar
          activeTimeRange={activeRange}
          onTimeRangeChange={selectRange}
          isFilterOpen={isFilterOpen}
          onFilterToggle={handleFilterToggle}
          hasActiveFilters={hasAdvancedFilters}
          onClearFilters={handleClearFilters}
          isViewConfigOpen={isViewConfigOpen}
          onViewConfigToggle={handleViewConfigToggle}
          onRefresh={loadData}
          onOpenRecovery={recoveryGroups.length > 0 ? () => setOpenRecoveryGroups(recoveryGroups) : undefined}
          columns={columnConfigs}
          visibleColumns={visibleColumns}
          onToggleColumn={toggleColumn}
          onResetColumns={resetColumns}
          autoRefresh={autoRefresh}
          filterPanel={
            <FiltersPanel
              isOpen={isFilterOpen}
              onClose={() => setIsFilterOpen(false)}
              filters={filters}
              onApply={handleApplyFilters}
              models={models}
              channels={filterResources.channels}
            />
          }
        />
      </div>

      {/* 统计与明细共用一个工作区，面板打开时一起淡化 */}
      <div className={`motion-content-dim flex min-h-[20rem] min-w-0 flex-1 flex-col ${isFilterOpen || isViewConfigOpen ? 'opacity-40 pointer-events-none blur-[1px]' : ''}`}>
        <RequestsTable
          summary={
            <StatsOverview
              stats={stats}
              claudeStats={claudeStats}
              codexStats={codexStats}
              total={pagination.total}
            />
          }
          requests={displayRequests}
          showDate={activeRange !== 'today'}
          loading={loading}
          pagination={pagination}
          onPageChange={handlePageChange}
          onPageSizeChange={handlePageSizeChange}
          visibleColumns={visibleColumns}
          columnConfigs={columnConfigs}
          onRowDoubleClick={handleOpenDetails}
          onViewDispatch={(request) => handleOpenDetails(request, 'chain')}
        />
      </div>

      {/* 请求详情模态框 */}
      <RequestDetailModal
        key={`${isDetailModalOpen}/${detailRequestId || ''}/${detailInitialTab}`}
        isOpen={isDetailModalOpen}
        onClose={handleCloseDetailModal}
        request={selectedRequest}
        initialTab={detailInitialTab}
        positioningRef={requestPageRef}
      />
      {openRecoveryGroups && <RequestRecoveryDialog groups={openRecoveryGroups} onClose={() => setOpenRecoveryGroups(null)} />}
    </div>
  );
};

export default RequestsPage;
