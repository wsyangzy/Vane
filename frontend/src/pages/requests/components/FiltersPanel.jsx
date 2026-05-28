// ============================================
// FiltersPanel - 弹出式高级筛选面板
// 2025-12-01 11:29:41
// ============================================

import { useMemo } from 'react';
import { Filter, X, Check, ChevronDown } from 'lucide-react';
import { CustomSelect } from '@components/ui';
import { STATUS_SELECT_OPTIONS } from '../utils/constants.js';
import DateTimePicker from './DateTimePicker.jsx';

/**
 * FiltersPanel - 弹出式高级筛选面板组件
 * @param {Object} props
 * @param {boolean} props.isOpen - 是否打开
 * @param {Function} props.onClose - 关闭回调
 * @param {Object} props.filters - 筛选器状态
 * @param {Function} props.updateFilter - 更新筛选器
 * @param {Function} props.onApply - 应用筛选回调
 * @param {Function} props.onReset - 重置回调
 * @param {Array} props.models - 模型列表
 * @param {Array} props.channels - 渠道列表（支持扁平数组或分组结构 [{provider, label, channels}]）
 * @param {Array} props.endpoints - 端点列表
 * @param {Function} props.onQuickTimeSelect - 快捷时间选择回调
 */
const FiltersPanel = ({
  isOpen,
  onClose,
  filters,
  updateFilter,
  onApply,
  onReset,
  models = [],
  channels = [],
  endpoints = [],
  onQuickTimeSelect: _onQuickTimeSelect
}) => {
  // 模型选项（格式化为 CustomSelect 需要的格式）
  const modelOptions = useMemo(() => {
    const options = models.map(model => {
      const modelName = typeof model === 'string' ? model : (model.model_name || model.name || '');
      return { value: modelName, label: modelName };
    }).filter(opt => opt.value);
    return [{ value: '', label: '所有模型' }, ...options];
  }, [models]);

  // 渠道选项 - 统一扁平化为 CustomSelect 格式，带 provider 前缀
  const channelOptions = useMemo(() => {
    const options = [{ value: '', label: '所有渠道' }];
    if (Array.isArray(channels) && channels.length > 0 && channels[0]?.provider) {
      // 分组结构: [{provider, label, channels: [name, ...]}, ...]
      channels.forEach(group => {
        const providerLabel = group.label || group.provider;
        (group.channels || []).filter(Boolean).forEach(name => {
          options.push({ value: name, label: `${providerLabel} / ${name}` });
        });
      });
    } else {
      // 扁平结构（向后兼容）
      (channels || []).forEach(channel => {
        const channelName = typeof channel === 'string' ? channel : (channel.name || channel.channel || '');
        if (channelName) options.push({ value: channelName, label: channelName });
      });
    }
    return options;
  }, [channels]);

  // 端点选项 - 根据当前选中渠道过滤，未选渠道或选"全部"时显示所有端点
  const endpointOptions = useMemo(() => {
    const selectedChannel = filters.channel === 'all' ? '' : (filters.channel || '');
    const filtered = endpoints.filter(endpoint => {
      if (!selectedChannel) return true; // 未选渠道，显示全部端点
      if (typeof endpoint === 'string') return true;
      const epChannel = endpoint.channel || endpoint.Channel || endpoint.group || endpoint.group_name;
      return epChannel === selectedChannel;
    });
    const options = filtered.map(endpoint => {
      const endpointName = typeof endpoint === 'string' ? endpoint : (endpoint.name || endpoint.endpoint_name || '');
      return { value: endpointName, label: endpointName };
    }).filter(opt => opt.value);
    return [{ value: '', label: '所有端点' }, ...options];
  }, [endpoints, filters.channel]);

  if (!isOpen) return null;

  // 快捷时间范围按钮（暂时移除，太占空间）
  // const quickTimeRanges = ['今天', '昨天', '近1小时', '近24小时', '本周'];

  // 渠道筛选当前值（空字符串视为"全部"）
  const currentChannelValue = filters.channel === 'all' ? '' : (filters.channel || '');

  return (
    <div className="bg-white rounded-xl border border-gray-200 shadow-xl mt-3 p-5 animate-in fade-in slide-in-from-top-2 duration-200 absolute left-0 right-0 z-20 ring-1 ring-black/5">
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
      <div className="space-y-4">
        {/* 时间范围 */}
        <div className="flex items-center gap-3">
          <label className="text-sm font-medium text-gray-700 min-w-[80px]">时间范围:</label>
          <div className="flex items-center gap-2">
            <DateTimePicker
              value={filters.startDate}
              onChange={(value) => updateFilter('startDate', value)}
              defaultTime={{ hour: 0, minute: 0 }}
              inputClassName="w-[200px] pl-9 pr-2 py-1.5 text-sm border border-gray-200 rounded-lg focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 text-gray-600"
            />
            <span className="text-gray-400 text-sm">至</span>
            <DateTimePicker
              value={filters.endDate}
              onChange={(value) => updateFilter('endDate', value)}
              defaultTime={{ hour: 23, minute: 59 }}
              inputClassName="w-[200px] pl-9 pr-2 py-1.5 text-sm border border-gray-200 rounded-lg focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 text-gray-600"
            />
          </div>
        </div>

        {/* 筛选条件 */}
        <div className="flex items-center gap-4 flex-wrap">
          <div className="flex items-center gap-2 flex-1 basis-[140px]">
            <label className="text-sm font-medium text-gray-700 shrink-0">状态:</label>
            <CustomSelect
              options={STATUS_SELECT_OPTIONS}
              value={filters.status}
              onChange={(value) => updateFilter('status', value)}
              size="sm"
              placeholder="全部"
              className="flex-1"
            />
          </div>
          <div className="flex items-center gap-2 flex-1 basis-[140px]">
            <label className="text-sm font-medium text-gray-700 shrink-0">提供商:</label>
            <CustomSelect
              options={[
                { value: '', label: '全部' },
                { value: 'claude', label: 'Claude' },
                { value: 'codex', label: 'Codex' }
              ]}
              value={filters.provider || ''}
              onChange={(value) => updateFilter('provider', value)}
              size="sm"
              placeholder="全部"
              className="flex-1"
            />
          </div>
          <div className="flex items-center gap-2 flex-1 basis-[140px]">
            <label className="text-sm font-medium text-gray-700 shrink-0">模型:</label>
            <CustomSelect
              options={modelOptions}
              value={filters.model}
              onChange={(value) => updateFilter('model', value)}
              size="sm"
              placeholder="全部"
              className="flex-1"
            />
          </div>
          <div className="flex items-center gap-2 flex-1 basis-[140px]">
            <label className="text-sm font-medium text-gray-700 shrink-0">渠道:</label>
            <CustomSelect
              options={channelOptions}
              value={currentChannelValue}
              onChange={(value) => updateFilter('channel', value || 'all')}
              size="sm"
              placeholder="全部"
              className="flex-1"
            />
          </div>
          <div className="flex items-center gap-2 flex-1 basis-[140px]">
            <label className="text-sm font-medium text-gray-700 shrink-0">端点:</label>
            <CustomSelect
              options={endpointOptions}
              value={filters.endpoint === 'all' ? '' : filters.endpoint}
              onChange={(value) => updateFilter('endpoint', value || 'all')}
              size="sm"
              placeholder="全部"
              className="flex-1"
            />
          </div>
        </div>
      </div>

      {/* 底部按钮 */}
      <div className="flex items-center justify-between pt-4 mt-4 border-t border-gray-100">
        <button
          onClick={onReset}
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
              onApply?.();
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

export default FiltersPanel;
