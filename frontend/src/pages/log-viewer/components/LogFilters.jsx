// LogFilters.jsx - 日志过滤器组件
import { Search, X } from 'lucide-react';
import { LOG_LEVELS, LOG_LEVEL_LIST } from '../constants';

/**
 * 日志过滤器栏
 * @param {Object} stats 日志级别统计 { DEBUG: 10, INFO: 20, ... }
 * @param {string} levelFilter 当前级别过滤
 * @param {Function} onLevelFilterChange 级别过滤变化
 * @param {string} searchQuery 搜索关键词
 * @param {Function} onSearchChange 搜索变化
 * @param {boolean} autoScroll 自动滚动开关
 * @param {Function} onAutoScrollChange 自动滚动变化
 * @param {number} totalLogs 总日志数
 */
function LogFilters({
  stats,
  levelFilter,
  onLevelFilterChange,
  searchQuery,
  onSearchChange,
  autoScroll,
  onAutoScrollChange,
  totalLogs,
}) {
  const renderLevelButton = (level) => {
    const isAll = level === 'ALL';
    const isActive = levelFilter === level;
    const count = isAll ? totalLogs : (stats?.[level] || 0);

    const config = isAll
      ? { bg: 'bg-slate-50', color: 'text-slate-700', ring: 'ring-slate-200', icon: null }
      : LOG_LEVELS[level];

    const Icon = config?.icon || null;
    const label = isAll ? '全部' : level;

    const base =
      'inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl text-xs font-semibold transition-all border select-none';
    const active = `bg-white ${config?.color || 'text-slate-700'} border-slate-200 shadow-sm ring-1 ring-black/5`;
    const inactive = `bg-slate-50 text-slate-600 border-transparent hover:bg-slate-100`;

    return (
      <button
        key={level}
        type="button"
        onClick={() => onLevelFilterChange(level)}
        className={`${base} ${isActive ? active : inactive}`}
        aria-pressed={isActive}
      >
        {Icon ? <Icon size={12} className={config?.color || 'text-slate-500'} /> : null}
        <span className="whitespace-nowrap">{label}</span>
        <span
          className={`ml-0.5 inline-flex items-center justify-center px-1.5 py-0.5 rounded-lg font-mono text-[10px] leading-none ${
            isActive ? `${config?.bg || 'bg-slate-50'} ${config?.color || 'text-slate-700'} ring-1 ${config?.ring || 'ring-slate-200'}` : 'bg-white text-slate-500 ring-1 ring-slate-200'
          }`}
          title={`${label} 数量`}
        >
          {count}
        </span>
      </button>
    );
  };

  return (
    <div className="mt-4 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
      {/* 日志级别 */}
      <div className="flex flex-wrap items-center gap-2">
        {renderLevelButton('ALL')}
        {LOG_LEVEL_LIST.map(renderLevelButton)}
      </div>

      {/* 搜索框 */}
      <div className="flex flex-1 items-center gap-3">
        <div className="relative w-full max-w-md">
          <Search
            size={16}
            className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 pointer-events-none"
          />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder="搜索日志内容..."
            className="w-full pl-10 pr-10 py-2 text-sm text-slate-900 bg-white border border-slate-200 rounded-xl placeholder:text-slate-400 focus:outline-none focus:ring-2 focus:ring-indigo-500/30 focus:border-indigo-400 shadow-sm"
          />
          {searchQuery ? (
            <button
              type="button"
              onClick={() => onSearchChange('')}
              className="absolute right-2 top-1/2 -translate-y-1/2 p-1 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-lg transition-colors"
              title="清空搜索"
            >
              <X size={14} />
            </button>
          ) : null}
        </div>
      </div>

      {/* 自动滚动开关 */}
      <div className="flex items-center justify-between lg:justify-end gap-2">
        <span className="text-sm text-slate-600 select-none">自动滚动</span>
        <button
          type="button"
          role="switch"
          aria-checked={autoScroll}
          onClick={() => onAutoScrollChange(!autoScroll)}
          className={`relative inline-flex h-6 w-11 flex-shrink-0 items-center rounded-full border transition-colors focus:outline-none focus:ring-2 focus:ring-indigo-500/30 ${
            autoScroll ? 'bg-indigo-500 border-indigo-500' : 'bg-slate-200 border-slate-200'
          }`}
          title={autoScroll ? '已开启自动滚动' : '已关闭自动滚动'}
        >
          <span
            className={`inline-block h-5 w-5 rounded-full bg-white shadow-sm ring-1 ring-black/5 transition-transform ${
              autoScroll ? 'translate-x-5' : 'translate-x-0.5'
            }`}
          />
        </button>
      </div>
    </div>
  );
}

export default LogFilters;
