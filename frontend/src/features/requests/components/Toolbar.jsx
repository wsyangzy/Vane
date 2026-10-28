import Tooltip from '@/shared/ui/Tooltip.jsx';
// ============================================
// Toolbar - 页面工具栏
// 使用统一调度方案切换入口
// ============================================

import { useEffect, useRef } from 'react';
import { Filter, Settings2, ChevronDown } from 'lucide-react';
import { SegmentedTabs } from '@/shared/ui/index.js';
import { TIME_RANGE_OPTIONS } from '../utils/constants.js';
import ViewConfigPanel from './ViewConfigPanel.jsx';
import AutoRefreshControl from './AutoRefreshControl.jsx';
import RoutingProfileSwitcher from './RoutingProfileSwitcher.jsx';
import './toolbar.css';

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
 * @param {Function} props.onRefresh - 刷新回调
 * @param {Array} props.columns - 所有列配置
 * @param {Array} props.visibleColumns - 当前可见的列ID数组
 * @param {Function} props.onToggleColumn - 切换列显示回调
 * @param {Function} props.onResetColumns - 重置列配置回调
 * @param {Object} props.autoRefresh - 自动刷新状态和控制
 * @param {React.ReactNode} props.filterPanel - 筛选面板
 */
const Toolbar = ({
  activeTimeRange = 'today',
  onTimeRangeChange,
  isFilterOpen = false,
  onFilterToggle,
  isViewConfigOpen = false,
  onViewConfigToggle,
  onRefresh,
  columns = [],
  visibleColumns = [],
  onToggleColumn,
  onResetColumns,
  autoRefresh = null,
  filterPanel = null
}) => {
  const viewConfigRef = useRef(null);

  useEffect(() => {
    if (!isViewConfigOpen) return;

    const handlePointerDown = (event) => {
      if (viewConfigRef.current?.contains(event.target)) return;
      onViewConfigToggle?.(false);
    };

    document.addEventListener('pointerdown', handlePointerDown);
    return () => document.removeEventListener('pointerdown', handlePointerDown);
  }, [isViewConfigOpen, onViewConfigToggle]);

  return (
    <div role="group" aria-label="请求追踪工具栏" className="requests-toolbar">
      <RoutingProfileSwitcher />

      <div className="requests-command-group" role="group" aria-label="时间与筛选">
        {/* 快捷时间范围选择 */}
        <div className="shrink-0">
          <SegmentedTabs
            tabs={TIME_RANGE_TABS}
            activeTab={activeTimeRange}
            onChange={onTimeRangeChange}
            className="requests-time-range grid-cols-3"
            buttonPaddingClass="px-1.5 whitespace-nowrap"
          />
        </div>

        {/* 筛选按钮 */}
        <div className="relative shrink-0">
          <Tooltip asChild content="高级筛选"><button
            type="button"
            onClick={onFilterToggle}
            className="requests-tool-button motion-control"
            data-active={isFilterOpen}
            aria-label="筛选"
            aria-expanded={isFilterOpen}
          >
            <Filter className="w-4 h-4" />
            <span className="requests-tool-label">筛选</span>
            <ChevronDown
              className={`requests-tool-chevron transition-transform ${isFilterOpen ? 'rotate-180' : ''}`}
            />
          </button></Tooltip>
          {filterPanel}
        </div>
      </div>

      <div className="requests-command-group" role="group" aria-label="视图操作">
        {/* 列配置按钮 */}
        <div ref={viewConfigRef} className="relative shrink-0">
          <Tooltip asChild content="显示列配置"><button
            type="button"
            onClick={onViewConfigToggle}
            className="requests-tool-button motion-control"
            data-active={isViewConfigOpen}
            aria-expanded={isViewConfigOpen}
            aria-label="显示列配置"
          >
            <Settings2 className="w-4 h-4" />
            <span className="requests-tool-label">显示</span>
          </button></Tooltip>

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
