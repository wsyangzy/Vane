// ============================================
// FiltersPanel - 弹出式高级筛选面板
// 2025-12-01 11:29:41
// ============================================

import { useEffect, useMemo, useRef, useState } from 'react';
import { Filter, X, Check, RotateCcw } from 'lucide-react';
import { CustomSelect } from '@/shared/ui/index.js';
import { DEFAULT_FILTERS, STATUS_SELECT_OPTIONS } from '../utils/constants.js';
import { getTodayTimeRange } from '../hooks/useFilters.js';
import ChannelFilterSelect from './ChannelFilterSelect.jsx';
import DateTimeRangePicker from './DateTimePicker.jsx';
import './filtersPanel.css';

const createDefaultDraftFilters = () => ({
  ...DEFAULT_FILTERS,
  ...getTodayTimeRange()
});

/**
 * FiltersPanel - 弹出式高级筛选面板组件
 * @param {Object} props
 * @param {boolean} props.isOpen - 是否打开
 * @param {Function} props.onClose - 关闭回调
 * @param {Object} props.filters - 筛选器状态
 * @param {Function} props.onApply - 应用筛选回调
 * @param {Array} props.models - 模型列表
 * @param {Array} props.channels - 提供商 -> 渠道 -> 凭证分组资源树
 */
const FiltersPanelContent = ({
  onClose,
  filters,
  onApply,
  models = [],
  channels = []
}) => {
  const panelRef = useRef(null);
  const [draftFilters, setDraftFilters] = useState(() => {
    const draft = { ...createDefaultDraftFilters(), ...filters };
    delete draft.endpoint;
    if (!draft.provider) draft.channel = 'all';
    if (!draft.channel || draft.channel === 'all') draft.group = 'all';
    return draft;
  });
  const [horizontalOffset, setHorizontalOffset] = useState(0);

  useEffect(() => {
    const handleClickOutside = (event) => {
      if (panelRef.current && !panelRef.current.contains(event.target)) {
        onClose?.();
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    const handleKeyDown = (event) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return;
      // Child selectors close first, including menus rendered outside this panel.
      if (panelRef.current?.querySelector('[aria-expanded="true"]') || document.querySelector('[role="listbox"]')) return;
      onClose?.();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [onClose]);

  useEffect(() => {
    const updatePosition = () => {
      const panel = panelRef.current;
      const anchor = panel?.offsetParent;
      if (!panel || !anchor) return;

      const viewportMargin = 16;
      const anchorRight = anchor.getBoundingClientRect().right;
      const panelLeft = anchorRight - panel.offsetWidth;
      const nextOffset = panelLeft < viewportMargin
        ? viewportMargin - panelLeft
        : Math.min(window.innerWidth - viewportMargin - anchorRight, 0);
      setHorizontalOffset(nextOffset);
      panel.style.maxHeight = `${Math.max(0, window.innerHeight - panel.getBoundingClientRect().top - viewportMargin)}px`;
    };

    let frameId;
    const refreshPosition = () => {
      window.cancelAnimationFrame(frameId);
      frameId = window.requestAnimationFrame(updatePosition);
    };
    const observer = new ResizeObserver(refreshPosition);
    observer.observe(panelRef.current);
    refreshPosition();
    window.addEventListener('resize', refreshPosition);
    window.addEventListener('scroll', refreshPosition, true);
    return () => {
      window.cancelAnimationFrame(frameId);
      observer.disconnect();
      window.removeEventListener('resize', refreshPosition);
      window.removeEventListener('scroll', refreshPosition, true);
    };
  }, []);

  const updateDraftFilter = (key, value) => {
    setDraftFilters(prev => ({ ...prev, [key]: value }));
  };

  const resetDraftFilters = () => {
    setDraftFilters(createDefaultDraftFilters());
  };

  // 模型选项（格式化为 CustomSelect 需要的格式）
  const modelOptions = useMemo(() => {
    const options = models.map(model => {
      const modelName = typeof model === 'string' ? model : (model.model_name || model.name || '');
      return { value: modelName, label: modelName };
    }).filter(opt => opt.value);
    return [{ value: '', label: '所有模型' }, ...options];
  }, [models]);

  const handleChannelChange = (selection) => setDraftFilters(previous => ({ ...previous, ...selection }));

  return (
    <div
      ref={panelRef}
      role="dialog"
      aria-label="高级筛选"
      className="request-filter-panel absolute right-0 top-12 z-50 rounded-xl border border-slate-200 bg-white p-4 shadow-xl ring-1 ring-black/5 animate-in fade-in slide-in-from-top-2 origin-top-right"
      style={{ translate: `${horizontalOffset}px 0` }}
    >
      {/* 头部 */}
      <div className="mb-4 flex shrink-0 items-center justify-between gap-3">
        <h3 className="flex items-center gap-2 text-sm font-semibold text-slate-900">
          <Filter className="w-4 h-4 text-indigo-500" /> 高级筛选
        </h3>
        <button type="button" onClick={onClose} aria-label="关闭高级筛选" className="flex h-7 w-7 items-center justify-center rounded-md text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-600 focus-visible:outline-2 focus-visible:outline-indigo-500">
          <X className="w-4 h-4" />
        </button>
      </div>

      {/* 筛选内容 */}
      <div className="request-filter-body space-y-3.5">
        {/* 时间范围 */}
        <div className="request-filter-time min-w-0">
          <span className="request-filter-label">时间范围</span>
          <DateTimeRangePicker
            startValue={draftFilters.startDate}
            endValue={draftFilters.endDate}
            onChange={({ startValue, endValue }) => {
              setDraftFilters((previous) => ({
                ...previous,
                startDate: startValue,
                endDate: endValue
              }));
            }}
            inputClassName="request-filter-range w-full rounded-lg border border-slate-200 bg-white px-3 py-2 text-slate-600 hover:border-slate-300 focus:outline-none focus-visible:border-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500/20"
          />
        </div>

        {/* 筛选条件 */}
        <div className="request-filter-conditions">
          <div className="request-filter-resource min-w-0 space-y-1.5">
            <span className="request-filter-label">渠道</span>
            <ChannelFilterSelect
              channels={channels}
              provider={draftFilters.provider}
              channel={draftFilters.channel}
              group={draftFilters.group}
              onChange={handleChannelChange}
            />
          </div>
          <div className="min-w-0 space-y-1.5">
            <span className="request-filter-label">状态</span>
            <CustomSelect
              options={STATUS_SELECT_OPTIONS}
              value={draftFilters.status}
              onChange={(value) => updateDraftFilter('status', value)}
              size="sm"
              placeholder="全部"
              fullWidth
              ariaLabel="筛选状态"
              className="request-filter-select"
            />
          </div>
          <div className="min-w-0 space-y-1.5">
            <span className="request-filter-label">模型</span>
            <CustomSelect
              options={modelOptions}
              value={draftFilters.model}
              onChange={(value) => updateDraftFilter('model', value)}
              size="sm"
              placeholder="全部"
              fullWidth
              ariaLabel="筛选模型"
              className="request-filter-select"
              wrapOptions
            />
          </div>
        </div>
      </div>

      {/* 底部按钮 */}
      <div className="request-filter-actions mt-4 flex shrink-0 items-center justify-between gap-2 border-t border-slate-100 pt-3">
        <button
          type="button"
          onClick={resetDraftFilters}
          className="inline-flex h-9 items-center gap-1.5 rounded-lg px-2 text-xs text-slate-500 transition-colors hover:bg-slate-50 hover:text-slate-800 focus-visible:outline-2 focus-visible:outline-indigo-500"
        >
          <RotateCcw size={14} /> 重置条件
        </button>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={onClose}
            className="h-9 rounded-lg border border-slate-200 bg-white px-3 text-xs font-medium text-slate-600 transition-colors hover:bg-slate-50 focus-visible:outline-2 focus-visible:outline-indigo-500"
          >
            取消
          </button>
          <button
            type="button"
            onClick={() => {
              onApply?.(draftFilters);
              onClose?.();
            }}
            className="flex h-9 items-center gap-1.5 rounded-lg bg-slate-900 px-3 text-xs font-medium text-white shadow-sm transition-colors hover:bg-slate-800 focus-visible:outline-2 focus-visible:outline-indigo-500"
          >
            <Check className="w-3.5 h-3.5" /> 应用筛选
          </button>
        </div>
      </div>
    </div>
  );
};

const FiltersPanel = ({ isOpen, ...props }) => {
  if (!isOpen) return null;
  return <FiltersPanelContent {...props} />;
};

export default FiltersPanel;
