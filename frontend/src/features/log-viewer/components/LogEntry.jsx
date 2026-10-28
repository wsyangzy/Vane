// LogEntry.jsx - 单条日志显示组件
// 支持多选复制
import { useState, memo } from 'react';
import { Copy, Check } from 'lucide-react';
import { Tooltip } from '@/shared/ui/index.js';
import { LOG_LEVELS } from '../constants.js';

// 转义正则表达式特殊字符
const escapeRegex = (str) => str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/**
 * 单条日志组件（使用 React.memo 优化渲染）
 * @param {Object} log 日志对象
 * @param {number} index 日志索引（用于多选）
 * @param {string} searchQuery 搜索关键词
 * @param {RegExp} highlightRegex 预编译的高亮正则（从父组件传入，避免重复编译）
 * @param {boolean} isSelected 是否被选中
 * @param {Function} onToggleSelect 切换选中状态的回调
 */
function LogEntry({ log, index, searchQuery, highlightRegex, isSelected, onToggleSelect }) {
  const [copied, setCopied] = useState(false);

  // 防御性检查：确保 log 对象有效
  if (!log || !log.message) {
    return null;
  }

  const levelConfig = LOG_LEVELS[log.level] || LOG_LEVELS.INFO;
  const Icon = levelConfig.icon;

  // 高亮搜索关键词（使用传入的预编译正则）
  const highlightText = (text) => {
    if (!searchQuery || !highlightRegex) return text;
    try {
      const parts = text.split(highlightRegex);
      return parts.map((part, i) =>
        part.toLowerCase() === searchQuery.toLowerCase() ? (
          <mark key={i} className="bg-yellow-200 text-gray-900">{part}</mark>
        ) : (
          part
        )
      );
    } catch {
      // 正则解析失败时返回原文本
      return text;
    }
  };

  // 复制日志内容
  const handleCopy = async (event) => {
    event.stopPropagation();
    const timestamp = new Date(log.timestamp).toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      fractionalSecondDigits: 3,
    });

    // 构建完整的日志文本
    let logText = `[${timestamp}] [${log.level}] ${log.message}`;

    // 添加附加属性
    if (log.attrs && Object.keys(log.attrs).length > 0) {
      const attrsText = Object.entries(log.attrs)
        .map(([key, value]) => `${key}=${value}`)
        .join(' ');
      logText += ` | ${attrsText}`;
    }

    try {
      await navigator.clipboard.writeText(logText);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      console.error('复制失败:', err);
    }
  };

  // 点击行选中（阻止冒泡避免触发外层清空）
  const handleRowClick = (e) => {
    e.stopPropagation();
    onToggleSelect?.(index);
  };

  return (
    <div
      onClick={handleRowClick}
      className={`group relative flex h-full min-w-0 items-center gap-2 border-b border-slate-100 px-3 font-mono text-xs cursor-pointer transition-colors lg:px-4 ${
        isSelected
          ? 'bg-blue-50 hover:bg-blue-100'
          : 'hover:bg-slate-50'
      }`}
    >
      {/* 时间戳 */}
      <Tooltip asChild content={new Date(log.timestamp).toLocaleString('zh-CN', { hour12: false })}><div className="w-24 shrink-0 whitespace-nowrap text-[11px] tabular-nums text-slate-400">
        {new Date(log.timestamp).toLocaleTimeString('zh-CN', {
          hour12: false,
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit',
          fractionalSecondDigits: 3,
        })}
      </div></Tooltip>

      {/* 日志级别 */}
      <div className={`flex w-14 shrink-0 items-center gap-1 ${levelConfig.color} whitespace-nowrap text-[11px]`}>
        <Icon size={12} />
        <span className="font-semibold">{log.level}</span>
      </div>

      {/* 日志消息 - 单行截断显示，完整内容通过悬浮提示和复制获取 */}
      <Tooltip content={log.message} className="block min-w-0 flex-1" contentClassName="whitespace-pre-wrap [overflow-wrap:anywhere]">
        <div className="text-slate-700 truncate">
          {highlightText(log.message)}
        </div>
      </Tooltip>

      {/* 附加属性 - 显示前3个，超长值截断 */}
      {log.attrs && Object.keys(log.attrs).length > 0 && (
        <Tooltip asChild content={Object.keys(log.attrs).length > 3 ? Object.entries(log.attrs).map(([key, value]) => `${key}=${typeof value === 'object' ? JSON.stringify(value) : String(value)}`).join(' · ') : undefined}><div className="min-w-0 max-w-[30%] shrink truncate whitespace-nowrap text-[10px] text-slate-400">
          {Object.entries(log.attrs).slice(0, 3).map(([key, value]) => (
            <span key={key} className="mr-2">
              {key}={typeof value === 'object' ? JSON.stringify(value) : String(value)}
            </span>
          ))}
          {Object.keys(log.attrs).length > 3 && (
            <span className="text-slate-300">+{Object.keys(log.attrs).length - 3}</span>
          )}
        </div></Tooltip>
      )}

      {/* 复制按钮 - 鼠标悬浮时显示 */}
      <Tooltip content={copied ? '已复制' : '复制日志'} className="shrink-0">
        <button
          onClick={handleCopy}
          className="shrink-0 rounded p-1 text-slate-400 opacity-0 transition-opacity hover:bg-slate-200 hover:text-slate-600 group-hover:opacity-100 focus-visible:opacity-100"
          aria-label={copied ? '已复制' : '复制日志'}
        >
          {copied ? (
            <Check size={14} className="text-green-500" />
          ) : (
            <Copy size={14} />
          )}
        </button>
      </Tooltip>
    </div>
  );
}

// 使用 React.memo 优化渲染
// log/searchQuery/isSelected 变化时才重新渲染
export default memo(LogEntry, (prevProps, nextProps) => {
  return (
    prevProps.log === nextProps.log &&
    prevProps.searchQuery === nextProps.searchQuery &&
    prevProps.isSelected === nextProps.isSelected
  );
});

// 导出 escapeRegex 供父组件使用
// eslint-disable-next-line react-refresh/only-export-components
export { escapeRegex };
