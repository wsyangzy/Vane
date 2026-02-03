// LogControls.jsx - 日志控制按钮组件
// v6.2.16: 添加批量复制按钮
import React from 'react';
import {
  Pause,
  Play,
  RefreshCw,
  Trash2,
  Download,
  Copy,
  Check,
} from 'lucide-react';

/**
 * 日志控制按钮组
 * @param {boolean} isStreaming 是否正在流式传输
 * @param {boolean} loading 是否加载中
 * @param {number} logsCount 日志数量
 * @param {Function} onToggleStream 切换流状态
 * @param {Function} onRefresh 刷新
 * @param {Function} onClear 清空
 * @param {Function} onExport 导出
 * @param {number} selectedCount 选中数量
 * @param {Function} onCopySelected 复制选中
 * @param {boolean} copySuccess 复制成功状态
 */
function LogControls({
  isStreaming,
  loading,
  logsCount,
  onToggleStream,
  onRefresh,
  onClear,
  onExport,
  selectedCount = 0,
  onCopySelected,
  copySuccess,
}) {
  return (
    <div className="flex items-center gap-2">
      {/* 实时状态指示器 */}
      <div className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-medium bg-slate-100 text-slate-700">
        <span className={`w-2 h-2 rounded-full ${
          isStreaming ? 'bg-emerald-400 animate-pulse' : 'bg-slate-300'
        }`} />
        {isStreaming ? '实时' : '已暂停'}
      </div>

      {/* v6.2.16: 复制选中按钮 - 放在暂停按钮左边 */}
      {selectedCount > 0 && (
        <button
          onClick={onCopySelected}
          className="p-2 rounded-lg bg-blue-100 text-blue-700 hover:bg-blue-200 transition-all"
          title={`复制选中 ${selectedCount} 条`}
        >
          <Copy size={16} />
        </button>
      )}

      {/* v6.2.16: 复制成功提示 */}
      {copySuccess && (
        <div className="flex items-center gap-1 p-2 rounded-lg bg-green-100 text-green-700">
          <Check size={16} />
        </div>
      )}

      {/* 暂停/开始按钮 */}
      <button
        onClick={onToggleStream}
        className={`p-2 rounded-lg transition-all ${
          isStreaming
            ? 'bg-amber-100 text-amber-700 hover:bg-amber-200'
            : 'bg-emerald-100 text-emerald-700 hover:bg-emerald-200'
        }`}
        title={isStreaming ? '暂停' : '开始'}
      >
        {isStreaming ? <Pause size={16} /> : <Play size={16} />}
      </button>

      {/* 刷新按钮 */}
      <button
        onClick={onRefresh}
        disabled={loading}
        className="p-2 rounded-lg bg-slate-100 text-slate-700 hover:bg-slate-200 transition-all disabled:opacity-50"
        title="刷新"
      >
        <RefreshCw size={16} className={loading ? 'animate-spin' : ''} />
      </button>

      {/* 清空按钮 */}
      <button
        onClick={onClear}
        className="p-2 rounded-lg bg-slate-100 text-slate-700 hover:bg-slate-200 transition-all"
        title="清空"
      >
        <Trash2 size={16} />
      </button>

      {/* 导出按钮 */}
      <button
        onClick={onExport}
        disabled={logsCount === 0}
        className="p-2 rounded-lg bg-slate-100 text-slate-700 hover:bg-slate-200 transition-all disabled:opacity-50"
        title="导出"
      >
        <Download size={16} />
      </button>
    </div>
  );
}

export default LogControls;
