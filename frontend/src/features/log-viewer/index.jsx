import { useState, useMemo, useRef, useEffect, useDeferredValue, useCallback } from 'react';
import { FixedSizeList as List } from 'react-window';
import { AlertCircle, Check, Copy, FileSearch, FilterX, RefreshCw, ScrollText, X } from 'lucide-react';
import { useWailsLogs } from './hooks/useWailsLogs.js';
import { LogEntry, LogControls, LogFilters } from './components/index.js';
import LogDetailPanel from './components/LogDetailPanel.jsx';
import { escapeRegex, formatLogText } from './logPresentation.js';
import { saveLogFile } from '@/services/wails/index.js';
import { SaveResultDialog, useToast } from '@/shared/ui/index.js';
import './systemLogs.css';

const LOG_ITEM_HEIGHT = 42;
const MAX_LOGS = 1000;

function LogRow({ index, style, data }) {
  const log = data.logs[index];
  return <div style={style}><LogEntry log={log} index={index} searchQuery={data.searchQuery} highlightRegex={data.highlightRegex}
    isSelected={data.selectedLogs.has(log)} isInspected={data.inspectedLog === log} copied={data.copiedLog === log}
    onToggleSelect={data.onToggleSelect} onInspect={data.onInspect} onCopy={data.onCopy} /></div>;
}

export default function LogsPage() {
  const toast = useToast();
  const { logs, loading, error, isStreaming, start, stop, clear, refresh } = useWailsLogs({ maxLogs: MAX_LOGS, autoStart: true });
  const [searchQuery, setSearchQuery] = useState('');
  const [levelFilter, setLevelFilter] = useState('ALL');
  const [autoScroll, setAutoScroll] = useState(true);
  const [selectedLogs, setSelectedLogs] = useState(new Set());
  const [inspectedLog, setInspectedLog] = useState(null);
  const [saveResult, setSaveResult] = useState(null);
  const [exporting, setExporting] = useState(false);
  const [copyFeedback, setCopyFeedback] = useState(null);
  const [actionError, setActionError] = useState('');
  const copyTimer = useRef(null);
  const listRef = useRef(null);
  const tableHeaderRef = useRef(null);
  const scrollbarObserverRef = useRef(null);
  const containerRef = useRef(null);
  const [listHeight, setListHeight] = useState(400);
  const deferredQuery = useDeferredValue(searchQuery);
  const validLogs = useMemo(() => (Array.isArray(logs) ? logs : []).filter((log) => log && log.message != null), [logs]);
  const filteredLogs = useMemo(() => {
    const query = deferredQuery.toLowerCase();
    return validLogs.filter((log) => (levelFilter === 'ALL' || log.level === levelFilter)
      && (!query || String(log.message).toLowerCase().includes(query) || JSON.stringify(log.attrs || {}).toLowerCase().includes(query)));
  }, [validLogs, levelFilter, deferredQuery]);
  const highlightRegex = useMemo(() => deferredQuery ? new RegExp(`(${escapeRegex(deferredQuery)})`, 'gi') : null, [deferredQuery]);
  const selected = useMemo(() => filteredLogs.filter((log) => selectedLogs.has(log)), [filteredLogs, selectedLogs]);
  const stats = useMemo(() => {
    const counts = { DEBUG: 0, INFO: 0, WARN: 0, ERROR: 0 };
    validLogs.forEach((log) => { if (counts[log.level] !== undefined) counts[log.level]++; });
    return counts;
  }, [validLogs]);

  const setContainerRef = useCallback((node) => {
    containerRef.current = node;
    if (node) setListHeight(node.clientHeight || 400);
  }, []);
  const setListOuterRef = useCallback((node) => {
    scrollbarObserverRef.current?.disconnect();
    const measure = () => tableHeaderRef.current?.style.setProperty('--system-log-scrollbar-width', `${node ? node.offsetWidth - node.clientWidth : 0}px`);
    measure();
    if (node) {
      scrollbarObserverRef.current = new ResizeObserver(measure);
      scrollbarObserverRef.current.observe(node);
    }
  }, []);
  useEffect(() => {
    const container = containerRef.current;
    const observer = new ResizeObserver(() => setListHeight(Math.max(1, container.clientHeight)));
    observer.observe(container);
    return () => observer.disconnect();
  }, []);
  useEffect(() => () => clearTimeout(copyTimer.current), []);
  useEffect(() => () => scrollbarObserverRef.current?.disconnect(), []);
  // Depend on the records themselves: the live buffer stays at 1000 entries as it rolls.
  useEffect(() => {
    if (autoScroll && !loading && filteredLogs.length) listRef.current?.scrollToItem(filteredLogs.length - 1, 'end');
  }, [filteredLogs, autoScroll, loading, listHeight]);

  const handleLevelChange = (level) => { setLevelFilter(level); setSelectedLogs(new Set()); setInspectedLog(null); };
  const handleSearchChange = (query) => { setSearchQuery(query); setSelectedLogs(new Set()); setInspectedLog(null); };
  const toggleSelect = useCallback((log) => {
    // Record identity keeps a selection attached to its log when the live buffer rolls.
    setSelectedLogs((previous) => {
      const next = new Set(filteredLogs.filter((entry) => previous.has(entry)));
      if (next.has(log)) next.delete(log);
      else next.add(log);
      return next;
    });
  }, [filteredLogs]);
  const inspect = useCallback((log) => setInspectedLog(log), []);
  const copyLogs = useCallback(async (entries, kind) => {
    if (!entries.length) return false;
    try {
      await navigator.clipboard.writeText(entries.map(formatLogText).join('\n'));
      setActionError('');
      setCopyFeedback({ kind, log: entries.length === 1 ? entries[0] : null });
      clearTimeout(copyTimer.current);
      copyTimer.current = setTimeout(() => setCopyFeedback(null), 2000);
      return true;
    } catch {
      setActionError('复制日志失败，请重试。');
      return false;
    }
  }, []);
  const copyOne = useCallback((log) => copyLogs([log], 'single'), [copyLogs]);
  const copySelected = async () => { if (await copyLogs(selected, 'selection')) setSelectedLogs(new Set()); };
  const handleClear = () => {
    clear();
    setSelectedLogs(new Set());
    setInspectedLog(null);
    setActionError('');
    setCopyFeedback(null);
  };
  const handleRefresh = () => { setActionError(''); setSelectedLogs(new Set()); return refresh(); };
  const resetFilters = () => { handleLevelChange('ALL'); setSearchQuery(''); };
  const handleExport = async () => {
    if (exporting) return;
    setExporting(true);
    setSaveResult(null);
    const now = new Date();
    const fileName = `logs-${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}-${String(now.getHours()).padStart(2, '0')}${String(now.getMinutes()).padStart(2, '0')}.txt`;
    try {
      const result = await saveLogFile(filteredLogs.map(formatLogText).join('\n'), fileName);
      if (!result.success && result.error === 'cancelled') return;
      if (result.success) toast.success(`已保存 ${result.file_name || fileName}`, '日志已导出');
      else setSaveResult({ success: false, error: result.error });
    } catch (err) {
      setSaveResult({ success: false, error: err.message || '保存失败' });
    } finally {
      setExporting(false);
    }
  };

  const itemData = useMemo(() => ({
    logs: filteredLogs, searchQuery: deferredQuery, highlightRegex, selectedLogs, inspectedLog,
    copiedLog: copyFeedback?.log, onToggleSelect: toggleSelect, onInspect: inspect, onCopy: copyOne
  }), [filteredLogs, deferredQuery, highlightRegex, selectedLogs, inspectedLog, copyFeedback, toggleSelect, inspect, copyOne]);
  const copySuccess = copyFeedback?.kind === 'selection';
  const hasFilters = Boolean(searchQuery || levelFilter !== 'ALL');

  return <div className="system-logs-page">
    <header className="system-logs-page-header">
      <div className="system-logs-heading"><span className="system-logs-emblem"><ScrollText size={20} /></span><h1>系统日志</h1><span className="system-logs-caption">运行事件与诊断信息</span></div>
      <LogControls isStreaming={isStreaming} loading={loading} exporting={exporting} logsCount={validLogs.length} exportCount={filteredLogs.length}
        onToggleStream={isStreaming ? stop : start} onRefresh={handleRefresh} onClear={handleClear} onExport={handleExport} />
    </header>
    <section className="system-log-workspace" aria-label="系统日志工作区">
      <LogFilters stats={stats} totalLogs={validLogs.length} levelFilter={levelFilter} onLevelFilterChange={handleLevelChange}
        searchQuery={searchQuery} onSearchChange={handleSearchChange} autoScroll={autoScroll} onAutoScrollChange={setAutoScroll} />
      {(error || actionError) && <div role="alert" className="system-log-error"><AlertCircle size={14} /><span>{error || actionError}</span>
        {error ? <button type="button" className="system-log-action" onClick={handleRefresh} disabled={loading}><RefreshCw size={12} />重试</button> : <button type="button" className="system-log-icon" aria-label="关闭错误提示" onClick={() => setActionError('')}><X size={13} /></button>}
      </div>}
      <div className="system-log-workspace-body">
        <div className="system-log-table" role="table" aria-label="系统日志记录" aria-rowcount={filteredLogs.length + 1}>
          <div ref={tableHeaderRef} role="row" className="system-log-table-header system-log-columns">
            <div role="columnheader" className="system-log-selection"><input type="checkbox" className="system-log-checkbox" aria-label="全选筛选结果" checked={filteredLogs.length > 0 && selected.length === filteredLogs.length}
              ref={(node) => { if (node) node.indeterminate = selected.length > 0 && selected.length < filteredLogs.length; }} disabled={!filteredLogs.length}
              onChange={() => setSelectedLogs(selected.length === filteredLogs.length ? new Set() : new Set(filteredLogs))} /></div>
            <div role="columnheader">时间</div><div role="columnheader">级别</div><div role="columnheader">日志消息</div><div role="columnheader" className="system-log-fields">附加字段</div><div role="columnheader" className="system-log-actions-heading">操作</div>
          </div>
          <div ref={setContainerRef} className="system-log-list-region" role="rowgroup">
            {loading && !validLogs.length ? <div role="status" className="system-log-empty"><span className="system-log-empty-icon"><RefreshCw size={22} className="animate-spin" /></span><h2>正在读取日志</h2><p>加载最近的运行记录</p></div>
              : !filteredLogs.length ? <div className="system-log-empty"><span className="system-log-empty-icon"><FileSearch size={24} /></span><h2>{error ? '暂时无法读取日志' : hasFilters ? '没有匹配的日志' : '暂无日志记录'}</h2><p>{error ? '请重试或刷新日志' : hasFilters ? '调整级别或关键词后重试' : isStreaming ? '新的运行事件将在这里实时显示' : '开始接收后查看新的运行事件'}</p>
                {hasFilters && <button type="button" className="system-log-action" onClick={resetFilters}><FilterX size={13} />清除筛选</button>}</div>
                : <List ref={listRef} outerRef={setListOuterRef} className="system-log-virtual-list scrollbar-visible" style={{ overflowX: 'hidden' }} height={listHeight} itemCount={filteredLogs.length} itemSize={LOG_ITEM_HEIGHT} width="100%" itemData={itemData}>{LogRow}</List>}
          </div>
        </div>
        {inspectedLog && <LogDetailPanel log={inspectedLog} copied={copyFeedback?.log === inspectedLog} onCopy={copyOne} onClose={() => setInspectedLog(null)} />}
      </div>
      <footer className="system-log-footer">
        <div className="system-log-footer-summary"><span>显示 <strong>{filteredLogs.length}</strong> / {validLogs.length} 条</span><span className="system-log-retention">最多显示 {MAX_LOGS} 条</span>{selected.length > 0 && <span className="system-log-selected-count">已选 {selected.length} 条</span>}</div>
        <div className="system-log-footer-actions">{selected.length > 0 && <button type="button" className="system-log-icon" aria-label="取消选择日志" onClick={() => setSelectedLogs(new Set())}><X size={13} /></button>}
          <button type="button" className="system-log-action system-log-copy-selection" disabled={!selected.length} onClick={copySelected}>{copySuccess ? <Check size={13} /> : <Copy size={13} />}{copySuccess ? '已复制' : '复制所选'}</button>
        </div>
      </footer>
    </section>
    <SaveResultDialog isOpen={Boolean(saveResult)} result={saveResult} onClose={() => setSaveResult(null)} />
  </div>;
}
