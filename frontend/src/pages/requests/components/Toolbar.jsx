// ============================================
// Toolbar - 页面工具栏
// 简化端点切换（移除 Token 级联）
// 渠道切换器始终显示，渠道间转移不影响手动渠道选择
// ============================================

import { useEffect, useRef } from 'react';
import { Activity, Filter, Settings2, ChevronDown } from 'lucide-react';
import { SegmentedTabs } from '@components/ui';
import { TIME_RANGE_OPTIONS } from '../utils/constants.js';
import ViewConfigPanel from './ViewConfigPanel.jsx';
import AutoRefreshControl from './AutoRefreshControl.jsx';
import ActiveGroupSwitcher from './ActiveGroupSwitcher.jsx';

const TIME_RANGE_TABS = TIME_RANGE_OPTIONS.map(({ value, label }) => ({
  key: value,
  label
}));

/**
 * Toolbar - 请求追踪页面工具栏
 * @param {Object} props
 * @param {string} props.activeTimeRange - 当前激活的时间范围
 * @param {Function} props.onTimeRangeChange - 时间范围变更回调
 * @param {boolean} props.isFilterOpen - 筛选面板是否打开
 * @param {Function} props.onFilterToggle - 筛选面板切换回调
 * @param {boolean} props.isViewConfigOpen - 列配置面板是否打开
 * @param {Function} props.onViewConfigToggle - 列配置面板切换回调
 * @param {boolean} props.isHeatmapOpen - Token 分布面板是否打开
 * @param {Function} props.onHeatmapToggle - Token 分布面板切换回调
 * @param {Function} props.onRefresh - 刷新回调
 * @param {Array} props.columns - 所有列配置
 * @param {Array} props.visibleColumns - 当前可见的列ID数组
 * @param {Function} props.onToggleColumn - 切换列显示回调
 * @param {Function} props.onResetColumns - 重置列配置回调
 * @param {Object} props.autoRefresh - 自动刷新状态和控制
 * @param {Array} props.groups - 所有渠道列表（一个渠道=一个组）
 * @param {string} props.activeGroup - 当前活跃渠道名称
 * @param {Function} props.onGroupSwitch - 渠道切换回调 (channelName) => void
 * @param {React.ReactNode} props.filterPanel - 筛选面板
 * @param {React.ReactNode} props.heatmapPanel - Token 分布面板
 */
const Toolbar = ({
  activeTimeRange = 'today',
  onTimeRangeChange,
  isFilterOpen = false,
  onFilterToggle,
  isViewConfigOpen = false,
  onViewConfigToggle,
  isHeatmapOpen = false,
  onHeatmapToggle,
  onRefresh,
  columns = [],
  visibleColumns = [],
  onToggleColumn,
  onResetColumns,
  autoRefresh = null,
  groups = [],
  activeGroup = '',
  onGroupSwitch,
  filterPanel = null,
  heatmapPanel = null
}) => {
  const viewConfigRef = useRef(null);
  const heatmapRef = useRef(null);

  useEffect(() => {
    if (!isViewConfigOpen) return;

    const handlePointerDown = (event) => {
      if (viewConfigRef.current?.contains(event.target)) return;
      onViewConfigToggle?.(false);
    };

    document.addEventListener('pointerdown', handlePointerDown);
    return () => document.removeEventListener('pointerdown', handlePointerDown);
  }, [isViewConfigOpen, onViewConfigToggle]);

  useEffect(() => {
    if (!isHeatmapOpen) return;

    const handlePointerDown = (event) => {
      if (heatmapRef.current?.contains(event.target)) return;
      onHeatmapToggle?.(false);
    };

    document.addEventListener('pointerdown', handlePointerDown);
    return () => document.removeEventListener('pointerdown', handlePointerDown);
  }, [isHeatmapOpen, onHeatmapToggle]);

  return (
    <div className="flex flex-wrap items-center gap-3">
      {/* 渠道快捷切换器 */}
      <ActiveGroupSwitcher
        groups={groups}
        activeGroup={activeGroup}
        onSwitch={onGroupSwitch}
      />

      <div className="h-6 w-px bg-gray-300 mx-1 hidden sm:block"></div>

      {/* 快捷时间范围选择 */}
      <div className="hidden sm:block">
        <SegmentedTabs
          tabs={TIME_RANGE_TABS}
          activeTab={activeTimeRange}
          onChange={onTimeRangeChange}
          className="min-w-[168px] grid-cols-3 border border-slate-200 bg-slate-100/80 shadow-sm"
          buttonPaddingClass="px-2.5"
        />
      </div>

      <div className="h-6 w-px bg-gray-300 mx-1 hidden sm:block"></div>

      {/* 筛选按钮 */}
      <div className="relative">
        <button
          type="button"
          onClick={onFilterToggle}
          className={`motion-control flex items-center gap-2 px-3 py-2 rounded-lg text-sm font-medium shadow-sm border ${
            isFilterOpen
              ? 'bg-indigo-50 text-indigo-700 border-indigo-200 ring-2 ring-indigo-100'
              : 'bg-white text-gray-700 border-gray-200 hover:border-indigo-300 hover:text-indigo-600'
          }`}
          aria-expanded={isFilterOpen}
        >
          <Filter className="w-4 h-4" /> 筛选
          <ChevronDown
            className={`w-3.5 h-3.5 transition-transform ${isFilterOpen ? 'rotate-180' : ''}`}
          />
        </button>
        {filterPanel}
      </div>

      <div className="h-6 w-px bg-gray-300 mx-1 hidden sm:block"></div>

      {/* Token 分布按钮 */}
      <div ref={heatmapRef} className="relative">
        <button
          type="button"
          onClick={() => onHeatmapToggle?.()}
          className={`motion-control flex items-center gap-2 px-3 py-2 rounded-lg text-sm font-medium shadow-sm border ${
            isHeatmapOpen
              ? 'bg-indigo-50 text-indigo-700 border-indigo-200 ring-2 ring-indigo-100'
              : 'bg-white text-gray-700 border-gray-200 hover:border-indigo-300 hover:text-indigo-600'
          }`}
          aria-expanded={isHeatmapOpen}
          aria-controls="token-usage-heatmap-panel"
          aria-label="Token 活动分布"
        >
          <Activity className="w-4 h-4" />
          <span className="hidden sm:inline">分布</span>
        </button>
        {heatmapPanel}
      </div>

      <div className="h-6 w-px bg-gray-300 mx-1 hidden sm:block"></div>

      {/* 列配置按钮 */}
      <div ref={viewConfigRef} className="relative">
        <button
          type="button"
          onClick={onViewConfigToggle}
          className={`motion-control flex items-center gap-2 px-3 py-2 rounded-lg text-sm shadow-sm border ${
            isViewConfigOpen
              ? 'bg-indigo-50 text-indigo-700 border-indigo-200 ring-2 ring-indigo-100'
              : 'bg-white text-gray-600 border-gray-200 hover:bg-gray-50 hover:text-indigo-600 hover:border-indigo-200'
          }`}
          aria-expanded={isViewConfigOpen}
          aria-label="显示列配置"
        >
          <Settings2 className="w-4 h-4" />
          <span className="hidden sm:inline font-medium">显示</span>
        </button>

        {/* 列配置面板 */}
        <ViewConfigPanel
          isOpen={isViewConfigOpen}
          onClose={() => onViewConfigToggle?.(false)}
          columns={columns}
          visibleColumns={visibleColumns}
          onToggleColumn={onToggleColumn}
          onReset={onResetColumns}
        />
      </div>

      {/* 自动刷新控制（包含手动刷新） */}
      {autoRefresh && (
        <AutoRefreshControl
          isEnabled={autoRefresh.isEnabled}
          interval={autoRefresh.interval}
          onIntervalChange={autoRefresh.changeInterval}
          onManualRefresh={onRefresh}
        />
      )}
    </div>
  );
};

export default Toolbar;
