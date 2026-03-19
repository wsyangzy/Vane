// LogEntry.jsx - 单条日志显示组件
// 支持多选复制
import { useState, memo } from 'react';
import { Copy, Check } from 'lucide-react';
import { LOG_LEVELS } from '../constants';

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
  const handleCopy = async () => {
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
      className={`group flex items-center gap-3 px-4 py-2 border-b border-slate-100 font-mono text-xs relative cursor-pointer transition-colors ${
        isSelected
          ? 'bg-blue-50 hover:bg-blue-100'
          : 'hover:bg-slate-50'
      }`}
    >
      {/* 时间戳 */}
      <div className="text-slate-400 whitespace-nowrap w-32 flex-shrink-0">
        {new Date(log.timestamp).toLocaleTimeString('zh-CN', {
          hour12: false,
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit',
          fractionalSecondDigits: 3,
        })}
      </div>

      {/* 日志级别 */}
      <div className={`flex items-center gap-1 ${levelConfig.color} whitespace-nowrap w-16 flex-shrink-0`}>
        <Icon size={12} />
        <span className="font-semibold">{log.level}</span>
      </div>

      {/* 日志消息 - 单行截断显示，完整内容通过悬浮提示和复制获取 */}
      <div className="flex-1 min-w-0 text-slate-700 truncate" title={log.message}>
        {highlightText(log.message)}
      </div>

      {/* 附加属性 - 显示前3个，超长值截断 */}
      {log.attrs && Object.keys(log.attrs).length > 0 && (
        <div className="text-slate-400 text-[10px] whitespace-nowrap flex-shrink-0 max-w-[400px] truncate">
          {Object.entries(log.attrs).slice(0, 3).map(([key, value]) => (
            <span key={key} className="mr-2">
              {key}={String(value).length > 40 ? String(value).slice(0, 40) + '...' : value}
            </span>
          ))}
          {Object.keys(log.attrs).length > 3 && (
            <span className="text-slate-300">+{Object.keys(log.attrs).length - 3}</span>
          )}
        </div>
      )}

      {/* 复制按钮 - 鼠标悬浮时显示 */}
      <button
        onClick={handleCopy}
        className="opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0 p-1 rounded hover:bg-slate-200 text-slate-400 hover:text-slate-600"
        title="复制日志"
      >
        {copied ? (
          <Check size={14} className="text-green-500" />
        ) : (
          <Copy size={14} />
        )}
      </button>
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
