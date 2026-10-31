import { memo } from 'react';
import { Check, Copy, ScanText } from 'lucide-react';
import Tooltip from '@/shared/ui/Tooltip.jsx';
import { LOG_LEVELS } from '../constants.js';
import { formatLogTime, formatLogValue } from '../logPresentation.js';

function LogEntry({ log, index, searchQuery, highlightRegex, isSelected, isInspected, copied, onToggleSelect, onInspect, onCopy }) {
  if (!log || log.message == null) return null;
  const level = LOG_LEVELS[log.level] ? log.level : 'INFO';
  const fields = Object.entries(log.attrs || {});
  const highlight = (text) => {
    if (!searchQuery || !highlightRegex) return text;
    return text.split(highlightRegex).map((part, i) => part.toLowerCase() === searchQuery.toLowerCase() ? <mark key={i}>{part}</mark> : part);
  };

  return <div role="row" aria-rowindex={index + 2} aria-selected={isSelected} data-level={level} data-selected={isSelected} data-inspected={isInspected}
    className="system-log-row system-log-columns" onClick={() => onToggleSelect(log)}>
    <div role="cell" className="system-log-selection"><input type="checkbox" className="system-log-checkbox" aria-label={`选择日志 ${index + 1}`} checked={isSelected} onClick={(event) => event.stopPropagation()} onChange={() => onToggleSelect(log)} /></div>
    <div role="cell"><Tooltip asChild content={formatLogTime(log.timestamp, true)}><time dateTime={log.timestamp} className="system-log-time">{formatLogTime(log.timestamp)}</time></Tooltip></div>
    <div role="cell"><span className="system-log-level-badge" data-level={level}>{log.level || 'INFO'}</span></div>
    <div role="cell" className="system-log-message">{highlight(String(log.message))}</div>
    <div role="cell" className="system-log-fields">
      {fields.slice(0, 2).map(([key, value]) => <span key={key} className="system-log-field"><span>{key}</span><span>{highlight(formatLogValue(value))}</span></span>)}
      {fields.length > 2 && <span className="system-log-field-count">+{fields.length - 2}</span>}
      {!fields.length && <span className="system-log-no-fields">—</span>}
    </div>
    <div role="cell" className="system-log-row-actions" onClick={(event) => event.stopPropagation()}>
      <Tooltip asChild content="查看日志详情"><button type="button" className="system-log-icon" aria-label={`查看日志 ${index + 1} 详情`} aria-expanded={isInspected} onClick={() => onInspect(log)}><ScanText size={13} /></button></Tooltip>
      <Tooltip asChild content={copied ? '已复制' : '复制日志'}><button type="button" className="system-log-icon" aria-label={`复制日志 ${index + 1}`} onClick={() => onCopy(log)}>{copied ? <Check size={13} className="text-emerald-600" /> : <Copy size={13} />}</button></Tooltip>
    </div>
  </div>;
}

export default memo(LogEntry);
