// ============================================
// Requests 页面 - 请求追踪（重构版）
// 简化端点切换（移除 Token 级联）
// 获取渠道间故障转移状态，控制渠道切换器显示
// ============================================

import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { BarChart3 } from 'lucide-react';
import { useToast } from '@/shared/ui/index.js';
import { fetchRequests, fetchModels, fetchUsageStats, fetchEndpoints, fetchGroups, activateGroup, fetchInterChannelFailoverEnabled } from '@/services/http/api.js';
import { getCodexChannels, activateCodexChannel, getSettingsByScope } from '@/services/wails/index.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';
import { useFilters } from './hooks/useFilters.js';
import { useColumnConfig } from './hooks/useColumnConfig.js';
import { useTimeRange } from './hooks/useTimeRange.js';
import { useAutoRefresh } from './hooks/useAutoRefresh.js';
import { FiltersPanel, StatsOverview, RequestsTable, Toolbar, RequestDetailModal, TokenUsageHeatmapPanel } from './components/index.js';
import { PAGINATION_CONFIG } from './utils/constants.js';

// ============================================
// Requests 页面
// ============================================

const RequestsPage = () => {
  // ==================== 状态管理 ====================

  // 数据状态
  const [requests, setRequests] = useState([]);
  const [stats, setStats] = useState(null);
  const [claudeStats, setClaudeStats] = useState(null); // Claude 提供商统计
  const [codexStats, setCodexStats] = useState(null);   // Codex 提供商统计
  const [models, setModels] = useState([]);
  const [endpoints, setEndpoints] = useState([]);
  const [codexChannels, setCodexChannels] = useState([]); // Codex 渠道列表
  const [groups, setGroups] = useState([]); // 渠道列表（一个渠道=一个组）
  const [activeGroup, setActiveGroup] = useState(''); // 当前活跃渠道名称（沿用字段名兼容历史）
  const [interChannelFailoverEnabled, setInterChannelFailoverEnabled] = useState(false); // 渠道间故障转移是否启用
  const [codexInterChannelFailoverEnabled, setCodexInterChannelFailoverEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const toast = useToast();

  // 面板状态
  const [isFilterOpen, setIsFilterOpen] = useState(false);
  const [isViewConfigOpen, setIsViewConfigOpen] = useState(false);
  const [isHeatmapOpen, setIsHeatmapOpen] = useState(false);

  // 详情模态框状态
  const [selectedRequest, setSelectedRequest] = useState(null);
  const [isDetailModalOpen, setIsDetailModalOpen] = useState(false);
  const requestPageRef = useRef(null);

  // 筛选器 Hook
  const {
    filters,
    updateFilters,
    buildQueryParams
  } = useFilters();

  // 列配置 Hook
  const {
    visibleColumns,
    toggleColumn,
    resetColumns,
    allColumns: columnConfigs
  } = useColumnConfig();

  // 时间范围 Hook
  const { activeRange, selectRange, clearRange } = useTimeRange((timeRange) => {
    updateFilters(timeRange);
    setPagination(prev => ({ ...prev, page: 1 }));
  });

  // 分页状态
  const [pagination, setPagination] = useState({
    page: 1,
    pageSize: PAGINATION_CONFIG.DEFAULT_PAGE_SIZE,
    total: 0,
    totalPages: 1
  });

  const visibleClaudeGroups = useMemo(() => (
    interChannelFailoverEnabled ? groups.filter(group => !group.paused) : groups
  ), [groups, interChannelFailoverEnabled]);

  const visibleCodexChannels = useMemo(() => (
    codexInterChannelFailoverEnabled
      ? codexChannels.filter(channel => channel.failoverEnabled !== false)
      : codexChannels
  ), [codexChannels, codexInterChannelFailoverEnabled]);

  // 合并 Claude 端点 + Codex 渠道下的端点（每个端点附带渠道名，用于按渠道过滤）
  const allEndpoints = useMemo(() => {
    const claudeEndpoints = (endpoints || []).map(ep => ({ ...ep, provider: 'claude' }))
    const codexEndpoints = (codexChannels || []).flatMap(channel =>
      (channel.endpoints || []).map(ep => ({
        name: ep.name,
        channel: channel.name,
        provider: 'codex'
      }))
    )
    return [...claudeEndpoints, ...codexEndpoints]
  }, [endpoints, codexChannels])

  // 合并 Claude groups + Codex channels 为统一格式（用于 Toolbar 渠道切换器）
  const allGroups = useMemo(() => {
    const claudeGroups = visibleClaudeGroups.map(g => ({ ...g, provider: 'claude' }))
    const codexGroups = visibleCodexChannels.map(ch => ({
      name: ch.name,
      provider: 'codex',
      is_active: Boolean(ch.isActive || ch.is_active),
      priority: ch.priority ?? 999
    }))
    return [...claudeGroups, ...codexGroups]
  }, [visibleClaudeGroups, visibleCodexChannels])

  // 从端点列表 + 渠道组 + Codex 渠道构造分组结构（按 Provider 分两级）
  const channels = useMemo(() => {
    const claudeSet = new Set();
    const visibleClaudeNames = new Set(visibleClaudeGroups.map(group => group.name || group.Name).filter(Boolean));
    // 从端点数据中提取渠道名
    endpoints.forEach(ep => {
      const channel = ep.channel || ep.Channel || ep.group || ep.group_name;
      if (channel && (!interChannelFailoverEnabled || visibleClaudeNames.has(channel))) claudeSet.add(channel);
    });
    // 从渠道组数据中补充（确保即使端点未加载也能显示渠道）
    visibleClaudeGroups.forEach(g => {
      const name = g.name || g.Name;
      if (name) claudeSet.add(name);
    });
    const codexNames = visibleCodexChannels
      .map(ch => ch?.name)
      .filter(Boolean);

    return [
      { provider: 'claude', label: 'Claude', channels: Array.from(claudeSet).sort() },
      { provider: 'codex', label: 'Codex', channels: codexNames.sort() }
    ];
  }, [endpoints, interChannelFailoverEnabled, visibleClaudeGroups, visibleCodexChannels]);

  const heatmapQueryParams = useMemo(() => buildQueryParams(), [buildQueryParams]);

  // ==================== 数据加载 ====================

  const loadData = useCallback(async (silent = false) => {
    const isSilent = silent === true;
    try {
      // 静默刷新时不改变 loading 状态，避免闪屏
      if (!isSilent) {
        setLoading(true);
      }

      const queryParams = buildQueryParams();

      // 为stats API添加默认时间范围（30天），避免无数据问题
      const statsParams = {
        ...queryParams,
        period: '30d'
      };

      // 简化数据获取，移除 keysData
      // 添加渠道间故障转移状态获取 + Codex 渠道列表 + 按 Provider 分组统计
      const [requestsData, statsData, claudeStatsData, codexStatsData, modelsData, endpointsData, groupsData, failoverEnabled, codexChannelsData, codexFailoverSettings] = await Promise.all([
        fetchRequests({
          ...queryParams,
          page: pagination.page,
          pageSize: pagination.pageSize
        }),
        fetchUsageStats(statsParams),
        fetchUsageStats({ ...statsParams, provider: 'claude' }).catch(() => null),
        fetchUsageStats({ ...statsParams, provider: 'codex' }).catch(() => null),
        fetchModels(),
        fetchEndpoints(),
        fetchGroups(),
        fetchInterChannelFailoverEnabled(),
        getCodexChannels().catch(() => []), // Codex 渠道获取失败不影响主流程
        getSettingsByScope('codex', 'failover').catch(() => [])
      ]);

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

      const endpointsList = endpointsData.endpoints || endpointsData || [];
      setEndpoints(Array.isArray(endpointsList) ? endpointsList : []);

      // Codex 渠道列表（用于在渠道筛选下拉框中合并展示）
      setCodexChannels(Array.isArray(codexChannelsData) ? codexChannelsData : []);

      // 渠道列表（一个渠道=一个组）
      const groupsList = groupsData?.groups || [];
      setGroups(Array.isArray(groupsList) ? groupsList : []);

      // 从组数据中找到活跃渠道
      const activeGroupObj = groupsList.find(g => g.is_active);
      if (activeGroupObj) {
        setActiveGroup(activeGroupObj.name);
      }

      // 设置渠道间故障转移状态
      setInterChannelFailoverEnabled(failoverEnabled);
      const codexEnabledSetting = codexFailoverSettings.find(setting => setting.key === 'enabled');
      setCodexInterChannelFailoverEnabled(String(codexEnabledSetting?.value).toLowerCase() === 'true');
    } catch (err) {
      console.error('加载请求追踪数据失败:', err);
      if (!isSilent) {
        toast.error(getErrorMessage(err, '请求数据加载失败'), '请求数据加载失败');
      }
    } finally {
      // 只有手动刷新才会改变 loading 状态
      if (!isSilent) {
        setLoading(false);
      }
    }
  }, [buildQueryParams, pagination.page, pagination.pageSize, toast]);

  // 自动刷新 Hook (必须在 loadData 定义之后)
  const autoRefresh = useAutoRefresh(loadData);

  // ==================== 事件处理 ====================

  // 筛选面板切换
  const handleFilterToggle = () => {
    setIsFilterOpen(!isFilterOpen);
    setIsViewConfigOpen(false); // 关闭列配置
    setIsHeatmapOpen(false);
  };

  // 列配置面板切换
  const handleViewConfigToggle = (nextOpen) => {
    const resolvedOpen = typeof nextOpen === 'boolean' ? nextOpen : !isViewConfigOpen;
    setIsViewConfigOpen(resolvedOpen);
    if (resolvedOpen) {
      setIsFilterOpen(false); // 关闭筛选面板
      setIsHeatmapOpen(false);
    }
  };

  const handleHeatmapToggle = (nextOpen) => {
    const resolvedOpen = typeof nextOpen === 'boolean' ? nextOpen : !isHeatmapOpen;
    setIsHeatmapOpen(resolvedOpen);
    if (resolvedOpen) {
      setIsFilterOpen(false);
      setIsViewConfigOpen(false);
    }
  };

  // 应用筛选
  const handleApplyFilters = (nextFilters) => {
    if (nextFilters) {
      updateFilters(nextFilters);
      clearRange();
    }
    setPagination(prev => ({ ...prev, page: 1 }));
  };

  // 页码变更
  const handlePageChange = (newPage) => {
    setPagination(prev => ({ ...prev, page: newPage }));
  };

  // 每页条数变更
  const handlePageSizeChange = (newPageSize) => {
    setPagination(prev => ({
      ...prev,
      pageSize: newPageSize,
      page: 1 // 重置到第一页
    }));
  };

  // 双击行打开详情
  const handleRowDoubleClick = (request) => {
    setSelectedRequest(request);
    setIsDetailModalOpen(true);
  };

  // 关闭详情模态框
  const handleCloseDetailModal = () => {
    setIsDetailModalOpen(false);
    setSelectedRequest(null);
  };

  // 渠道切换回调
  const handleGroupSwitch = async (channelName, _endpoint, provider) => {
    try {
      if (provider === 'codex') {
        const ch = codexChannels.find(c => c.name === channelName)
        const channelId = ch?.id ?? ch?.ID
        if (!channelId) {
          throw new Error(`找不到 Codex 渠道: ${channelName}`)
        }

        const isActive = Boolean(ch.isActive || ch.is_active)
        if (!isActive) {
          console.log('🔄 切换 Codex 渠道:', channelName)
          await activateCodexChannel(channelId)
          setCodexChannels(prev => prev.map(item => {
            const itemId = item.id ?? item.ID
            const itemActive = itemId === channelId
            return { ...item, isActive: itemActive, is_active: itemActive }
          }))
        }

        await loadData(true)
        return
      }

      // Claude 渠道沿用 activeGroup；Codex 活跃状态独立来自 codexChannels
      if (channelName !== activeGroup) {
        console.log('🔄 切换 Claude 渠道:', channelName)
        await activateGroup(channelName)
        setActiveGroup(channelName)
      }

      // 切换后刷新数据
      await loadData(true)
    } catch (err) {
      console.error('❌ 切换失败:', err)
      throw err // 让 ActiveGroupSwitcher 组件知道切换失败
    }
  }

  // ==================== 生命周期 ====================

  useEffect(() => {
    loadData();
  }, [loadData]);

  // ==================== 渲染 ====================

  return (
    <div ref={requestPageRef} className="relative flex min-h-0 min-w-0 flex-1 flex-col gap-3 animate-in fade-in slide-in-from-bottom-2 duration-500">
      {/* 页面标题 & 工具栏 */}
      <div className="relative z-30 flex shrink-0 items-center gap-4">
        {/* 页面标题 */}
        <div className="flex shrink-0 items-center gap-2.5">
          <div className="rounded-lg bg-indigo-600 p-1.5 text-white shadow-sm shadow-indigo-200/50">
            <BarChart3 className="h-5 w-5" />
          </div>
          <h1 className="text-xl font-bold tracking-tight text-slate-900">请求追踪</h1>
        </div>

        {/* 工具栏 */}
        <Toolbar
          activeTimeRange={activeRange}
          onTimeRangeChange={selectRange}
          isFilterOpen={isFilterOpen}
          onFilterToggle={handleFilterToggle}
          isViewConfigOpen={isViewConfigOpen}
          onViewConfigToggle={handleViewConfigToggle}
          isHeatmapOpen={isHeatmapOpen}
          onHeatmapToggle={handleHeatmapToggle}
          onRefresh={loadData}
          columns={columnConfigs}
          visibleColumns={visibleColumns}
          onToggleColumn={toggleColumn}
          onResetColumns={resetColumns}
          autoRefresh={autoRefresh}
          groups={allGroups}
          activeGroup={activeGroup}
          onGroupSwitch={handleGroupSwitch}
          interChannelFailoverEnabled={interChannelFailoverEnabled}
          filterPanel={
            <FiltersPanel
              isOpen={isFilterOpen}
              onClose={() => setIsFilterOpen(false)}
              filters={filters}
              onApply={handleApplyFilters}
              models={models}
              channels={channels}
              endpoints={allEndpoints}
            />
          }
          heatmapPanel={
            <TokenUsageHeatmapPanel
              isOpen={isHeatmapOpen}
              onClose={() => setIsHeatmapOpen(false)}
              queryParams={heatmapQueryParams}
            />
          }
        />
      </div>

      {/* 统计与明细共用一个工作区，面板打开时一起淡化 */}
      <div className={`motion-content-dim flex min-h-[20rem] min-w-0 flex-1 flex-col ${isFilterOpen || isViewConfigOpen || isHeatmapOpen ? 'opacity-40 pointer-events-none blur-[1px]' : ''}`}>
        <RequestsTable
          summary={
            <StatsOverview
              stats={stats}
              claudeStats={claudeStats}
              codexStats={codexStats}
              total={pagination.total}
            />
          }
          requests={requests}
          loading={loading}
          pagination={pagination}
          onPageChange={handlePageChange}
          onPageSizeChange={handlePageSizeChange}
          visibleColumns={visibleColumns}
          columnConfigs={columnConfigs}
          onRowDoubleClick={handleRowDoubleClick}
          interChannelFailoverEnabled={interChannelFailoverEnabled}
        />
      </div>

      {/* 请求详情模态框 */}
      <RequestDetailModal
        isOpen={isDetailModalOpen}
        onClose={handleCloseDetailModal}
        request={selectedRequest}
        positioningRef={requestPageRef}
        interChannelFailoverEnabled={interChannelFailoverEnabled}
      />
    </div>
  );
};

export default RequestsPage;
