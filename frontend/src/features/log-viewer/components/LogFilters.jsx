import { createLucideIcon, Search, X } from 'lucide-react';
import { SegmentedTabs } from '@/shared/ui/SegmentedTabs.jsx';
import Tooltip from '@/shared/ui/Tooltip.jsx';
import { LOG_LEVEL_LIST } from '../constants.js';

const AutoScroll = createLucideIcon('AutoScroll', [
  ['path', { d: 'M3 5h9M3 12h7M3 19h7', key: 'lines' }],
  ['path', { d: 'M18 5v14', key: 'scroll' }],
  ['path', { d: 'm14 15 4 4 4-4', key: 'direction' }],
]);

export default function LogFilters({ stats, levelFilter, onLevelFilterChange, searchQuery, onSearchChange, autoScroll, onAutoScrollChange, totalLogs }) {
  const tabs = ['ALL', ...LOG_LEVEL_LIST].map((level) => ({
    key: level,
    label: <span className="system-log-level-label" data-level={level}>
      {level !== 'ALL' && <i aria-hidden="true" />}<span>{level === 'ALL' ? '全部' : level}</span>
      <span className="system-log-level-count">{level === 'ALL' ? totalLogs : stats[level] || 0}</span>
    </span>
  }));

  return <div className="system-log-filters">
    <div className="system-log-level-scroll" role="group" aria-label="日志级别筛选">
      <SegmentedTabs tabs={tabs} activeTab={levelFilter} onChange={onLevelFilterChange} className="system-log-levels grid-cols-5" buttonPaddingClass="px-2" />
    </div>
    <div className="system-log-query-tools">
      <div className="system-log-search">
        <Search size={14} aria-hidden="true" />
        <input type="search" value={searchQuery} onChange={(event) => onSearchChange(event.target.value)} aria-label="搜索日志" placeholder="搜索消息或附加字段" />
        {searchQuery && <Tooltip asChild content="清空搜索"><button type="button" aria-label="清空搜索" onClick={() => onSearchChange('')}><X size={13} /></button></Tooltip>}
      </div>
      <Tooltip asChild content={autoScroll ? '自动滚动已开启，点击关闭' : '自动滚动已关闭，点击开启'}>
        <button type="button" aria-pressed={autoScroll} aria-label="自动滚动日志" className="system-log-follow" onClick={() => onAutoScrollChange(!autoScroll)}>
          <AutoScroll size={16} strokeWidth={1.75} aria-hidden="true" />
        </button>
      </Tooltip>
    </div>
  </div>;
}
