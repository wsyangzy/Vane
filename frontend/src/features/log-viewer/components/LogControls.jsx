import { CirclePause, Radio, Pause, Play, RefreshCw, Trash2, Download, Loader2 } from 'lucide-react';
import Tooltip from '@/shared/ui/Tooltip.jsx';

export default function LogControls({ isStreaming, loading, exporting, logsCount, exportCount, onToggleStream, onRefresh, onClear, onExport }) {
  return <div className="system-log-controls" role="group" aria-label="日志操作">
    <div className="system-log-stream-control" data-streaming={isStreaming} role="group" aria-label="实时日志接收">
      <span className="system-log-stream" role="status">
        {isStreaming ? <Radio size={14} strokeWidth={1.75} aria-hidden="true" /> : <CirclePause size={14} strokeWidth={1.75} aria-hidden="true" />}
        <span>{isStreaming ? '实时接收' : '接收已暂停'}</span>
      </span>
      <Tooltip asChild content={isStreaming ? '暂停接收日志' : '开始接收日志'}>
        <button type="button" className="system-log-stream-toggle" aria-label={isStreaming ? '暂停接收日志' : '开始接收日志'} onClick={onToggleStream}>
          {isStreaming ? <Pause size={13} strokeWidth={1.75} aria-hidden="true" /> : <Play size={13} strokeWidth={1.75} aria-hidden="true" />}
        </button>
      </Tooltip>
    </div>
    <Tooltip asChild content="刷新日志"><button type="button" className="system-log-icon" aria-label="刷新日志" onClick={onRefresh} disabled={loading}><RefreshCw size={14} className={loading ? 'animate-spin' : ''} /></button></Tooltip>
    <span className="system-log-divider" />
    <button type="button" className="system-log-action" onClick={onExport} disabled={exporting || !exportCount}>{exporting ? <Loader2 size={13} className="animate-spin" /> : <Download size={13} />}{exporting ? '正在导出' : '导出日志'}</button>
    <Tooltip asChild content="清空当前列表"><button type="button" className="system-log-icon system-log-clear" aria-label="清空当前列表" onClick={onClear} disabled={loading || !logsCount}><Trash2 size={14} /></button></Tooltip>
  </div>;
}
