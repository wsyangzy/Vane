// ============================================
// Requests 页面 - 请求追踪（重构版）
// 简化端点切换（移除 Token 级联）
// 获取渠道间故障转移状态，控制渠道切换器显示
// ============================================

import { useState, useEffect, useCallback, useMemo } from 'react';
import { BarChart3 } from 'lucide-react';
import { ErrorMessage } from '@components/ui';
import { fetchRequests, fetchModels, fetchUsageStats, fetchEndpoints, fetchGroups, activateGroup, fetchInterChannelFailoverEnabled } from '@utils/api.js';
import { getCodexChannels, activateCodexChannel } from '@utils/wailsApi.js';
import { useFilters } from './hooks/useFilters.js';
import { useColumnConfig } from './hooks/useColumnConfig.js';
import { useTimeRange } from './hooks/useTimeRange.js';
import { useAutoRefresh } from './hooks/useAutoRefresh.js';
import { FiltersPanel, StatsOverview, RequestsTable, Toolbar, RequestDetailModal } from './components';
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
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  // 面板状态
  const [isFilterOpen, setIsFilterOpen] = useState(false);
  const [isViewConfigOpen, setIsViewConfigOpen] = useState(false);

  // 详情模态框状态
  const [selectedRequest, setSelectedRequest] = useState(null);
  const [isDetailModalOpen, setIsDetailModalOpen] = useState(false);

  // 筛选器 Hook
  const {
    filters,
    updateFilter,
    updateFilters,
    resetFilters,
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
  const { activeRange, selectRange } = useTimeRange((timeRange) => {
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
    const claudeGroups = (groups || []).map(g => ({ ...g, provider: 'claude' }))
    const codexGroups = (codexChannels || []).map(ch => ({
      name: ch.name,
      provider: 'codex',
      is_active: Boolean(ch.isActive || ch.is_active),
      priority: ch.priority ?? 999
    }))
    return [...claudeGroups, ...codexGroups]
  }, [groups, codexChannels])

  // 从端点列表 + 渠道组 + Codex 渠道构造分组结构（按 Provider 分两级）
  const channels = useMemo(() => {
    const claudeSet = new Set();
    // 从端点数据中提取渠道名
    endpoints.forEach(ep => {
      const channel = ep.channel || ep.Channel || ep.group || ep.group_name;
      if (channel) claudeSet.add(channel);
    });
    // 从渠道组数据中补充（确保即使端点未加载也能显示渠道）
    groups.forEach(g => {
      const name = g.name || g.Name;
      if (name) claudeSet.add(name);
    });
    const codexNames = (codexChannels || [])
      .map(ch => ch?.name)
      .filter(Boolean);

    return [
      { provider: 'claude', label: 'Claude', channels: Array.from(claudeSet).sort() },
      { provider: 'codex', label: 'Codex', channels: codexNames.sort() }
    ];
  }, [endpoints, codexChannels, groups]);

  // ==================== 数据加载 ====================

  const loadData = useCallback(async (silent = false) => {
    try {
      // 静默刷新时不改变 loading 状态，避免闪屏
      if (!silent) {
        setLoading(true);
      }
      setError(null);

      const queryParams = buildQueryParams();

      // 为stats API添加默认时间范围（30天），避免无数据问题
      const statsParams = {
        ...queryParams,
        period: '30d'
      };

      // 简化数据获取，移除 keysData
      // 添加渠道间故障转移状态获取 + Codex 渠道列表 + 按 Provider 分组统计
      const [requestsData, statsData, claudeStatsData, codexStatsData, modelsData, endpointsData, groupsData, failoverEnabled, codexChannelsData] = await Promise.all([
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
        getCodexChannels().catch(() => []) // Codex 渠道获取失败不影响主流程
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
    } catch (err) {
      setError(err.message);
    } finally {
      // 只有手动刷新才会改变 loading 状态
      if (!silent) {
        setLoading(false);
      }
    }
  }, [buildQueryParams, pagination.page, pagination.pageSize]);

  // 自动刷新 Hook (必须在 loadData 定义之后)
  const autoRefresh = useAutoRefresh(loadData);

  // ==================== 事件处理 ====================

  // 筛选面板切换
  const handleFilterToggle = () => {
    setIsFilterOpen(!isFilterOpen);
    setIsViewConfigOpen(false); // 关闭列配置
  };

  // 列配置面板切换
  const handleViewConfigToggle = () => {
    setIsViewConfigOpen(!isViewConfigOpen);
    setIsFilterOpen(false); // 关闭筛选面板
  };

  // 应用筛选
  const handleApplyFilters = () => {
    setPagination(prev => ({ ...prev, page: 1 }));
    loadData();
  };

  // 重置筛选
  const handleResetFilters = () => {
    resetFilters();
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

  // 快捷时间选择（筛选面板内）
  const handleQuickTimeSelect = () => {
    // 这里可以实现快捷时间选择的逻辑
    // 简化实现：直接更新到"今天"
    const todayRange = {
      startDate: filters.startDate,
      endDate: filters.endDate
    };
    updateFilters(todayRange);
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

  if (error) {
    return (
      <ErrorMessage
        title="请求数据加载失败"
        message={error}
        onRetry={loadData}
      />
    );
  }

  return (
    <div className="h-full flex flex-col gap-6 pb-4 animate-in fade-in slide-in-from-bottom-2 duration-500 relative min-h-0">
      {/* 页面标题 & 工具栏 */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 relative z-30">
        {/* 页面标题 */}
        <div className="flex items-center gap-3">
          <div className="p-2 bg-indigo-600 rounded-lg text-white shadow-lg shadow-indigo-200/50">
            <BarChart3 className="w-6 h-6" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-gray-900 tracking-tight">请求追踪</h1>
            <p className="text-sm text-gray-500">实时监控所有转发请求的状态与详情</p>
          </div>
        </div>

        {/* 工具栏 */}
        <Toolbar
          activeTimeRange={activeRange}
          onTimeRangeChange={selectRange}
          isFilterOpen={isFilterOpen}
          onFilterToggle={handleFilterToggle}
          isViewConfigOpen={isViewConfigOpen}
          onViewConfigToggle={handleViewConfigToggle}
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
        />

        {/* 筛选面板（弹出式） */}
        <div className="absolute top-full left-0 right-0 z-10">
          <FiltersPanel
            isOpen={isFilterOpen}
            onClose={() => setIsFilterOpen(false)}
            filters={filters}
            updateFilter={updateFilter}
            onApply={handleApplyFilters}
            onReset={handleResetFilters}
            models={models}
            channels={channels}
            endpoints={allEndpoints}
            onQuickTimeSelect={handleQuickTimeSelect}
          />
        </div>
      </div>

      {/* 统计概览 - 面板打开时blur */}
      <div className="flex-none">
        <StatsOverview
          stats={stats}
          claudeStats={claudeStats}
          codexStats={codexStats}
          total={pagination.total}
          isBlurred={isFilterOpen || isViewConfigOpen}
        />
      </div>

      {/* 请求列表表格 - 面板打开时blur */}
      <div className={`flex-1 min-h-0 transition-all duration-300 ${isFilterOpen || isViewConfigOpen ? 'opacity-40 pointer-events-none blur-[1px]' : ''}`}>
        <RequestsTable
          requests={requests}
          loading={loading}
          pagination={pagination}
          onPageChange={handlePageChange}
          onPageSizeChange={handlePageSizeChange}
          visibleColumns={visibleColumns}
          columnConfigs={columnConfigs}
          onRowDoubleClick={handleRowDoubleClick}
        />
      </div>

      {/* 请求详情模态框 */}
      <RequestDetailModal
        isOpen={isDetailModalOpen}
        onClose={handleCloseDetailModal}
        request={selectedRequest}
      />
    </div>
  );
};

export default RequestsPage;
