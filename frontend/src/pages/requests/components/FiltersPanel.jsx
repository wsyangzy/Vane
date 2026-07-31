// ============================================
// FiltersPanel - 弹出式高级筛选面板
// 2025-12-01 11:29:41
// ============================================

import { useEffect, useMemo, useRef, useState } from 'react';
import { Filter, X, Check, ChevronDown } from 'lucide-react';
import { CustomSelect } from '@components/ui';
import { DEFAULT_FILTERS, STATUS_SELECT_OPTIONS } from '../utils/constants.js';
import { getTodayTimeRange } from '../hooks/useFilters.js';
import DateTimeRangePicker from './DateTimePicker.jsx';

const createDefaultDraftFilters = () => ({
  ...DEFAULT_FILTERS,
  ...getTodayTimeRange()
});

const normalizeChannelGroups = (channels) => {
  if (Array.isArray(channels) && channels.length > 0 && channels[0]?.provider) {
    return channels
      .map(group => ({
        provider: group.provider,
        label: group.label || group.provider,
        channels: (group.channels || []).filter(Boolean)
      }))
      .filter(group => group.provider && group.channels.length > 0);
  }

  const flatChannels = (channels || [])
    .map(channel => (typeof channel === 'string' ? channel : (channel?.name || channel?.channel || '')))
    .filter(Boolean);

  return flatChannels.length > 0
    ? [{ provider: 'default', label: '渠道', channels: flatChannels }]
    : [];
};

const ChannelCascadeSelect = ({ groups = [], value = '', provider = '', onChange, className = '' }) => {
  const [isOpen, setIsOpen] = useState(false);
  const rootRef = useRef(null);

  const getFilterProvider = (group) => (group?.provider === 'default' ? '' : group?.provider || '');
  const selectedGroup = groups.find(group => getFilterProvider(group) === provider && group.channels.includes(value))
    || groups.find(group => group.channels.includes(value));
  const activeProvider = groups.find(group => getFilterProvider(group) === provider)?.provider || selectedGroup?.provider || groups[0]?.provider || '';
  const [hoverProvider, setHoverProvider] = useState(activeProvider);

  useEffect(() => {
    const handleClickOutside = (event) => {
      if (rootRef.current && !rootRef.current.contains(event.target)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const currentGroup = groups.find(group => group.provider === hoverProvider) || groups[0];
  const displayGroup = selectedGroup || groups.find(group => getFilterProvider(group) === provider);
  const displayLabel = value
    ? `${displayGroup?.label || '渠道'} / ${value}`
    : provider
      ? `${displayGroup?.label || provider} / 全部`
      : '所有渠道';

  const selectAll = () => {
    onChange?.({ provider: '', channel: '' });
    setIsOpen(false);
  };

  const selectProvider = (group) => {
    onChange?.({ provider: getFilterProvider(group), channel: '' });
    setIsOpen(false);
  };

  const selectChannel = (group, channel) => {
    onChange?.({ provider: getFilterProvider(group), channel });
    setIsOpen(false);
  };

  return (
    <div ref={rootRef} className={`relative ${className}`}>
      <button
        type="button"
        onClick={() => {
          const nextOpen = !isOpen;
          if (nextOpen) setHoverProvider(activeProvider);
          setIsOpen(nextOpen);
        }}
        className={`
          group inline-flex w-full min-w-[132px] items-center justify-between rounded-lg border bg-white
          px-2.5 py-1.5 text-xs font-medium transition-all duration-200
          ${isOpen
            ? 'border-indigo-500 text-indigo-600 ring-2 ring-indigo-100'
            : 'border-slate-200 text-slate-600 hover:border-slate-300 hover:text-slate-700'
          }
        `}
      >
        <span className="truncate">{displayLabel}</span>
        <ChevronDown
          size={14}
          className={`ml-1.5 flex-shrink-0 text-slate-400 transition-transform duration-200 ${isOpen ? 'rotate-180' : ''}`}
        />
      </button>

      {isOpen && (
        <div className="absolute left-0 top-full z-[60] mt-1 flex w-[360px] overflow-hidden rounded-xl border border-slate-200 bg-white text-xs shadow-xl ring-1 ring-black/5">
          <div className="w-[116px] border-r border-slate-100 bg-slate-50/80 py-1">
            <button
              type="button"
              onClick={selectAll}
              className={`
                flex w-full items-center justify-between px-3 py-2 text-left font-medium transition-colors
                ${!provider && !value ? 'bg-indigo-50 text-indigo-600' : 'text-slate-600 hover:bg-white'}
              `}
            >
              <span>所有</span>
              {!provider && !value && <Check size={13} />}
            </button>
            {groups.map(group => (
              <button
                key={group.provider}
                type="button"
                onMouseEnter={() => setHoverProvider(group.provider)}
                onClick={() => setHoverProvider(group.provider)}
                className={`
                  flex w-full items-center justify-between px-3 py-2 text-left transition-colors
                  ${hoverProvider === group.provider ? 'bg-white text-slate-900' : 'text-slate-600 hover:bg-white/80'}
                `}
              >
                <span className="truncate">{group.label}</span>
                <ChevronDown size={12} className="-rotate-90 text-slate-400" />
              </button>
            ))}
          </div>

          <div className="max-h-[260px] min-w-0 flex-1 overflow-y-auto py-1">
            {currentGroup ? (
              <>
                <button
                  type="button"
                  onClick={() => selectProvider(currentGroup)}
                  className={`
                    flex w-full items-center justify-between px-3 py-2 text-left transition-colors
                    ${provider === getFilterProvider(currentGroup) && !value
                      ? 'bg-indigo-50 text-indigo-600'
                      : 'text-slate-600 hover:bg-slate-50'
                    }
                  `}
                >
                  <span className="truncate">{currentGroup.label} / 全部</span>
                  {provider === getFilterProvider(currentGroup) && !value && <Check size={13} />}
                </button>
                {currentGroup.channels.map(channel => {
                  const isSelected = provider === getFilterProvider(currentGroup) && value === channel;
                  return (
                    <button
                      key={`${currentGroup.provider}-${channel}`}
                      type="button"
                      onClick={() => selectChannel(currentGroup, channel)}
                      className={`
                        flex w-full items-center justify-between px-3 py-2 text-left transition-colors
                        ${isSelected ? 'bg-indigo-50 text-indigo-600' : 'text-slate-600 hover:bg-slate-50'}
                      `}
                    >
                      <span className="truncate">{channel}</span>
                      {isSelected && <Check size={13} />}
                    </button>
                  );
                })}
              </>
            ) : (
              <div className="px-3 py-6 text-center text-slate-400">暂无渠道</div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

/**
 * FiltersPanel - 弹出式高级筛选面板组件
 * @param {Object} props
 * @param {boolean} props.isOpen - 是否打开
 * @param {Function} props.onClose - 关闭回调
 * @param {Object} props.filters - 筛选器状态
 * @param {Function} props.onApply - 应用筛选回调
 * @param {Array} props.models - 模型列表
 * @param {Array} props.channels - 渠道列表（支持扁平数组或分组结构 [{provider, label, channels}]）
 * @param {Array} props.endpoints - 端点列表
 */
const FiltersPanelContent = ({
  onClose,
  filters,
  onApply,
  models = [],
  channels = [],
  endpoints = []
}) => {
  const panelRef = useRef(null);
  const [draftFilters, setDraftFilters] = useState(filters);
  const [horizontalOffset, setHorizontalOffset] = useState(0);

  useEffect(() => {
    const handleClickOutside = (event) => {
      if (panelRef.current && !panelRef.current.contains(event.target)) {
        onClose?.();
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
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
    };

    const frameId = window.requestAnimationFrame(updatePosition);
    window.addEventListener('resize', updatePosition);
    return () => {
      window.cancelAnimationFrame(frameId);
      window.removeEventListener('resize', updatePosition);
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

  const channelGroups = useMemo(() => normalizeChannelGroups(channels), [channels]);

  // 端点选项 - 根据当前选中渠道过滤，未选渠道或选"全部"时显示所有端点
  const endpointOptions = useMemo(() => {
    const selectedChannel = draftFilters.channel === 'all' ? '' : (draftFilters.channel || '');
    const selectedProvider = draftFilters.provider || '';
    const filtered = endpoints.filter(endpoint => {
      if (typeof endpoint === 'string') return true;
      if (selectedProvider && endpoint.provider && endpoint.provider !== selectedProvider) return false;
      if (!selectedChannel) return true; // 未选渠道，显示当前 provider 下的全部端点
      const epChannel = endpoint.channel || endpoint.Channel || endpoint.group || endpoint.group_name;
      return epChannel === selectedChannel;
    });
    const options = filtered.map(endpoint => {
      const endpointName = typeof endpoint === 'string' ? endpoint : (endpoint.name || endpoint.endpoint_name || '');
      return { value: endpointName, label: endpointName };
    }).filter(opt => opt.value);
    return [{ value: '', label: '所有端点' }, ...options];
  }, [draftFilters.channel, draftFilters.provider, endpoints]);

  // 快捷时间范围按钮（暂时移除，太占空间）
  // const quickTimeRanges = ['今天', '昨天', '近1小时', '近24小时', '本周'];

  // 渠道筛选当前值（空字符串视为"全部"）
  const currentChannelValue = draftFilters.channel === 'all' ? '' : (draftFilters.channel || '');
  const currentProviderValue = draftFilters.provider || '';

  const handleChannelChange = ({ provider, channel }) => {
    setDraftFilters(prev => ({
      ...prev,
      provider: provider || '',
      channel: channel || 'all',
      endpoint: 'all'
    }));
  };

  return (
    <div
      ref={panelRef}
      className="absolute right-0 top-12 z-50 w-[920px] max-w-[calc(100vw-2rem)] rounded-xl border border-gray-200 bg-white p-5 shadow-xl ring-1 ring-black/5 animate-in fade-in slide-in-from-top-2 duration-200 origin-top-right"
      style={{ translate: `${horizontalOffset}px 0` }}
    >
      {/* 头部 */}
      <div className="flex justify-between items-start mb-5">
        <h3 className="text-sm font-semibold text-gray-900 flex items-center gap-2">
          <Filter className="w-4 h-4 text-indigo-500" /> 高级筛选
        </h3>
        <button onClick={onClose} className="text-gray-400 hover:text-gray-600 transition-colors">
          <X className="w-4 h-4" />
        </button>
      </div>

      {/* 筛选内容 */}
      <div className="space-y-5">
        {/* 时间范围 */}
        <div className="grid grid-cols-[72px_auto] items-center gap-3">
          <label className="text-sm font-medium text-gray-700">时间范围:</label>
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
            inputClassName="h-9 w-[440px] max-w-full rounded-lg border border-gray-200 bg-white px-3 text-gray-600 hover:border-gray-300 focus:outline-none focus-visible:border-indigo-500 focus-visible:ring-2 focus-visible:ring-indigo-500/20"
          />
        </div>

        {/* 筛选条件 */}
        <div className="grid grid-cols-1 gap-x-5 gap-y-3 sm:grid-cols-2 xl:grid-cols-[180px_220px_180px_180px]">
          <div className="grid min-w-0 grid-cols-[44px_minmax(0,1fr)] items-center gap-2">
            <label className="text-sm font-medium text-gray-700">状态:</label>
            <CustomSelect
              options={STATUS_SELECT_OPTIONS}
              value={draftFilters.status}
              onChange={(value) => updateDraftFilter('status', value)}
              size="sm"
              placeholder="全部"
              fullWidth
            />
          </div>
          <div className="grid min-w-0 grid-cols-[44px_minmax(0,1fr)] items-center gap-2">
            <label className="text-sm font-medium text-gray-700">渠道:</label>
            <ChannelCascadeSelect
              groups={channelGroups}
              value={currentChannelValue}
              provider={currentProviderValue}
              onChange={handleChannelChange}
              className="min-w-0"
            />
          </div>
          <div className="grid min-w-0 grid-cols-[44px_minmax(0,1fr)] items-center gap-2">
            <label className="text-sm font-medium text-gray-700">端点:</label>
            <CustomSelect
              options={endpointOptions}
              value={draftFilters.endpoint === 'all' ? '' : draftFilters.endpoint}
              onChange={(value) => updateDraftFilter('endpoint', value || 'all')}
              size="sm"
              placeholder="全部"
              fullWidth
            />
          </div>
          <div className="grid min-w-0 grid-cols-[44px_minmax(0,1fr)] items-center gap-2">
            <label className="text-sm font-medium text-gray-700">模型:</label>
            <CustomSelect
              options={modelOptions}
              value={draftFilters.model}
              onChange={(value) => updateDraftFilter('model', value)}
              size="sm"
              placeholder="全部"
              fullWidth
            />
          </div>
        </div>
      </div>

      {/* 底部按钮 */}
      <div className="flex items-center justify-between pt-4 mt-4 border-t border-gray-100">
        <button
          onClick={resetDraftFilters}
          className="text-sm text-gray-500 hover:text-gray-800 transition-colors"
        >
          重置所有条件
        </button>
        <div className="flex gap-2">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm text-gray-600 bg-white border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors"
          >
            取消
          </button>
          <button
            onClick={() => {
              onApply?.(draftFilters);
              onClose?.();
            }}
            className="px-4 py-2 text-sm text-white bg-slate-900 rounded-lg hover:bg-slate-800 shadow-md shadow-slate-200 flex items-center gap-2 transition-all hover:shadow-lg"
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
