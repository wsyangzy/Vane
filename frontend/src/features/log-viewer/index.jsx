// LogsPage - 系统日志查看页面（主组件）
// 点击行选中 + 批量复制
import { useState, useMemo, useRef, useEffect, useDeferredValue, useCallback } from 'react';
import { FixedSizeList as List } from 'react-window';
import { useWailsLogs } from '@/features/log-viewer/hooks/useWailsLogs.js';
import { FileText, RefreshCw, AlertCircle } from 'lucide-react';
import { LogEntry, LogControls, LogFilters } from './components/index.js';
import { escapeRegex } from './components/LogEntry.jsx';
import { SaveResultDialog } from '@/shared/ui/index.js';

// 日志项固定高度（用于虚拟滚动）
const LOG_ITEM_HEIGHT = 40;

import { saveLogFile } from '@/services/wails/index.js';

// 检测组件挂载状态
function useComponentVisibility() {
  return true;
}

// 主页面组件
function LogsPage() {
  // 组件可见性
  const isPageVisible = useComponentVisibility();

  // 日志数据和操作
  const {
    logs,
    loading,
    error,
    isStreaming,
    start,
    stop,
    clear,
    refresh,
  } = useWailsLogs({
    maxLogs: 1000,
    autoStart: true,
    isActive: isPageVisible,
  });

  // 本地状态
  const [searchQuery, setSearchQuery] = useState('');
  const [levelFilter, setLevelFilter] = useState('ALL');
  const [autoScroll, setAutoScroll] = useState(true);
  const isInitialLoadRef = useRef(true);
  const prevLogsLengthRef = useRef(0);

  // 多选状态
  const [selectedIndices, setSelectedIndices] = useState(new Set());

  // 过滤器变更时清空选中
  const handleLevelFilterChange = useCallback((nextLevel) => {
    setLevelFilter(nextLevel);
    setSelectedIndices(new Set());
  }, []);

  const handleSearchChange = useCallback((nextQuery) => {
    setSearchQuery(nextQuery);
    setSelectedIndices(new Set());
  }, []);

  // 切换单条日志选择
  const handleToggleSelect = useCallback((index) => {
    setSelectedIndices(prev => {
      const next = new Set(prev);
      if (next.has(index)) {
        next.delete(index);
      } else {
        next.add(index);
      }
      return next;
    });
  }, []);

  // 虚拟滚动相关 refs
  const listRef = useRef(null);
  const containerRef = useRef(null);
  const [listHeight, setListHeight] = useState(400);
  const setContainerRef = useCallback((node) => {
    if (!node) return;
    containerRef.current = node;
    const nextHeight = node.clientHeight || 400;
    setListHeight((prev) => (prev === nextHeight ? prev : nextHeight));
  }, []);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    // The available space changes with window size and wrapped header controls.
    const observer = new ResizeObserver(() => {
      const nextHeight = Math.max(1, container.clientHeight);
      setListHeight((prev) => (prev === nextHeight ? prev : nextHeight));
    });
    observer.observe(container);
    return () => observer.disconnect();
  }, []);

  // 搜索防抖 - 使用 useDeferredValue 延迟过滤计算
  const deferredSearchQuery = useDeferredValue(searchQuery);

  // 缓存正则表达式（仅在 deferredSearchQuery 变化时重新编译）
  const highlightRegex = useMemo(() => {
    if (!deferredSearchQuery) return null;
    try {
      return new RegExp(`(${escapeRegex(deferredSearchQuery)})`, 'gi');
    } catch {
      return null;
    }
  }, [deferredSearchQuery]);

  // 保存结果弹窗状态
  const [saveResult, setSaveResult] = useState(null);
  const [showSaveDialog, setShowSaveDialog] = useState(false);

  // 批量复制成功提示
  const [copySuccess, setCopySuccess] = useState(false);

  // 过滤日志（使用 deferredSearchQuery 实现防抖）
  const filteredLogs = useMemo(() => {
    // 防御性检查：确保 logs 是数组且元素有效
    if (!Array.isArray(logs)) return [];

    let result = logs.filter(log => log && log.message != null);

    // 日志级别过滤
    if (levelFilter !== 'ALL') {
      result = result.filter(log => log.level === levelFilter);
    }

    // 搜索过滤（使用防抖后的查询值）
    if (deferredSearchQuery) {
      const query = deferredSearchQuery.toLowerCase();
      result = result.filter(log =>
        (log.message || '').toLowerCase().includes(query) ||
        (log.attrs && JSON.stringify(log.attrs).toLowerCase().includes(query))
      );
    }

    return result;
  }, [logs, levelFilter, deferredSearchQuery]);

  // 复制选中的日志
  const handleCopySelected = useCallback(async () => {
    if (selectedIndices.size === 0) return;

    const sortedIndices = Array.from(selectedIndices).sort((a, b) => a - b);
    const content = sortedIndices
      .map(index => {
        const log = filteredLogs[index];
        if (!log) return '';
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
        let logText = `[${timestamp}] [${log.level}] ${log.message}`;
        if (log.attrs && Object.keys(log.attrs).length > 0) {
          const attrsText = Object.entries(log.attrs)
            .map(([key, value]) => `${key}=${value}`)
            .join(' ');
          logText += ` | ${attrsText}`;
        }
        return logText;
      })
      .filter(Boolean)
      .join('\n');

    try {
      await navigator.clipboard.writeText(content);
      setSelectedIndices(new Set());
      setCopySuccess(true);
      setTimeout(() => setCopySuccess(false), 2000);
    } catch (err) {
      console.error('批量复制失败:', err);
    }
  }, [selectedIndices, filteredLogs]);

  // 统计信息
  const stats = useMemo(() => {
    const counts = { DEBUG: 0, INFO: 0, WARN: 0, ERROR: 0 };
    if (!Array.isArray(logs)) return counts;

    logs.forEach(log => {
      if (log && counts[log.level] !== undefined) {
        counts[log.level]++;
      }
    });
    return counts;
  }, [logs]);

  // 自动滚动逻辑（适配虚拟滚动）
  useEffect(() => {
    if (isInitialLoadRef.current && !loading && logs.length > 0) {
      isInitialLoadRef.current = false;
      prevLogsLengthRef.current = logs.length;
      // 初始加载时滚动到底部
      if (listRef.current) {
        listRef.current.scrollToItem(filteredLogs.length - 1, 'end');
      }
      return;
    }

    const hasNewLogs = logs.length > prevLogsLengthRef.current;
    if (autoScroll && hasNewLogs && !loading && listRef.current) {
      // 使用虚拟列表的 scrollToItem 方法
      listRef.current.scrollToItem(filteredLogs.length - 1, 'end');
    }

    prevLogsLengthRef.current = logs.length;
  }, [logs.length, filteredLogs.length, autoScroll, loading]);

  // 导出日志 (使用原生文件对话框)
  const handleExport = async () => {
    const content = filteredLogs
      .map(log => `[${log.timestamp}] [${log.level}] ${log.message}`)
      .join('\n');

    // 生成默认文件名
    const now = new Date();
    const defaultFileName = `logs-${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}-${String(now.getHours()).padStart(2, '0')}${String(now.getMinutes()).padStart(2, '0')}.txt`;

    try {
      const result = await saveLogFile(content, defaultFileName);

      // 用户取消不显示弹窗
      if (!result.success && result.error === 'cancelled') {
        return;
      }

      // 显示结果弹窗
      setSaveResult({
        success: result.success,
        fileName: result.file_name,
        filePath: result.file_path,
        fileSize: result.file_size,
        error: result.error
      });
      setShowSaveDialog(true);
    } catch (err) {
      // API 调用失败
      setSaveResult({
        success: false,
        error: err.message || '保存失败'
      });
      setShowSaveDialog(true);
    }
  };

  // 清空日志
  const handleClear = () => {
    clear();
    isInitialLoadRef.current = true;
    prevLogsLengthRef.current = 0;
  };

  return (
    <div
      className="flex min-h-0 flex-1 flex-col bg-white rounded-lg overflow-hidden border border-slate-200 shadow-sm"
    >
      {/* 页面头部 - 固定 */}
      <div className="shrink-0 border-b border-slate-200 px-4 py-3">
        {/* 标题栏 */}
        <div className="flex min-h-9 flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-2.5">
            <div className="shrink-0 rounded-lg bg-slate-900 p-1.5 text-white shadow-sm">
              <FileText size={20} />
            </div>
            <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
              <h1 className="text-xl font-bold text-slate-900">系统日志</h1>
              <p className="text-xs text-slate-500">
                共 {logs.length} 条
                {filteredLogs.length !== logs.length && ` · 筛选后 ${filteredLogs.length} 条`}
              </p>
            </div>
          </div>

          <LogControls
            isStreaming={isStreaming}
            loading={loading}
            logsCount={filteredLogs.length}
            onToggleStream={isStreaming ? stop : start}
            onRefresh={refresh}
            onClear={handleClear}
            onExport={handleExport}
            selectedCount={selectedIndices.size}
            onCopySelected={handleCopySelected}
            copySuccess={copySuccess}
          />
        </div>

        {/* 过滤器栏 */}
        <LogFilters
          stats={stats}
          levelFilter={levelFilter}
          onLevelFilterChange={handleLevelFilterChange}
          searchQuery={searchQuery}
          onSearchChange={handleSearchChange}
          autoScroll={autoScroll}
          onAutoScrollChange={setAutoScroll}
          totalLogs={logs.length}
        />
      </div>

      {/* 日志内容区域 - 虚拟滚动 */}
      <div ref={setContainerRef} className="flex-1 overflow-hidden bg-white">
        {loading && logs.length === 0 ? (
          <div className="flex items-center justify-center h-full text-slate-400">
            <RefreshCw size={24} className="animate-spin mr-2" />
            加载中...
          </div>
        ) : error ? (
          <div className="flex items-center justify-center h-full text-red-500">
            <AlertCircle size={24} className="mr-2" />
            {error}
          </div>
        ) : filteredLogs.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-full text-slate-400">
            <FileText size={48} className="mb-2 opacity-50" />
            <p className="text-lg">暂无日志</p>
            {searchQuery || levelFilter !== 'ALL' ? (
              <p className="text-sm mt-1">尝试调整搜索条件或过滤器</p>
            ) : null}
          </div>
        ) : (
          <List
            ref={listRef}
            height={listHeight || 400}
            itemCount={filteredLogs.length}
            itemSize={LOG_ITEM_HEIGHT}
            width="100%"
            itemData={{
              logs: filteredLogs || [],
              searchQuery: deferredSearchQuery || '',
              highlightRegex: highlightRegex || null,
              selectedIndices,
              onToggleSelect: handleToggleSelect
            }}
          >
            {({ index, style, data }) => (
              <div style={style}>
                <LogEntry
                  log={data.logs[index]}
                  index={index}
                  searchQuery={data.searchQuery}
                  highlightRegex={data.highlightRegex}
                  isSelected={data.selectedIndices.has(index)}
                  onToggleSelect={data.onToggleSelect}
                />
              </div>
            )}
          </List>
        )}
      </div>

      {/* 保存结果弹窗 */}
      <SaveResultDialog
        isOpen={showSaveDialog}
        result={saveResult}
        onClose={() => setShowSaveDialog(false)}
      />
    </div>
  );
}

export default LogsPage;
