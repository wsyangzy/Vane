// ============================================
// RequestsTable - 请求列表表格（支持动态列配置）
// 2025-12-01 10:31:09
// 更新：添加双击查看详情功能
// ============================================

import React, { useState, useRef, useCallback, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { Waves, RefreshCw, Route, ChevronRight } from 'lucide-react';
import { LoadingSpinner, Tooltip } from '@components/ui';
import { formatCost, formatTimestamp } from '@utils/api.js';
import DurationBadge from './DurationBadge.jsx';
import RequestStatusBadge from './RequestStatusBadge.jsx';
import ModelTag from './ModelTag.jsx';
import Pagination from './Pagination.jsx';
import { getDisplayInputTokens } from '../utils/tokenDisplay.js';
import {
  countChannelSwitches,
  getEndpointChainItems,
  parseChainNode,
  parseEndpointName,
  requestHasInterChannelTransfer,
} from '../utils/endpointChain.js';
import {
  formatReasoningFieldValue,
  getReasoningEffortMeta,
  parseReasoningConfig
} from '../utils/requestMetadata.js';

const getReasoningDisplay = (raw) => {
  const reasoning = parseReasoningConfig(raw);
  const value = reasoning.effort || reasoning.type || '';
  if (!value) return <span className="text-gray-400 text-xs">-</span>;
  const meta = reasoning.effort ? getReasoningEffortMeta(value) : {
    label: formatReasoningFieldValue('type', value),
    description: '扩展思考类型。',
    className: value === 'enabled' ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-slate-200 bg-slate-50 text-slate-600'
  };
  return (
    <Tooltip content={meta.description}>
      <span className={`inline-flex items-center whitespace-nowrap rounded border px-2 py-0.5 text-xs font-medium ${meta.className}`}>
        {meta.label}
      </span>
    </Tooltip>
  );
};

const ChainTooltipBadge = ({ label, value }) => (
  <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-slate-700/80 text-[9px] font-medium text-slate-100 ring-1 ring-white/10 shrink-0">
    <span className="text-slate-400">{label}</span>
    <span>{value}</span>
  </span>
);

const ChannelCell = ({ request }) => {
  const channelLabel = request.channel || '-';
  const channelTransferCount = countChannelSwitches(getEndpointChainItems(request));

  return (
    <span className="inline-flex max-w-[10rem] items-center justify-center gap-1.5 min-w-0">
      <span className="text-purple-600 text-xs font-medium truncate">{channelLabel}</span>
      {channelTransferCount > 0 && (
        <span
          className="inline-flex items-center gap-0.5 px-1.5 py-0.5 bg-indigo-50 text-indigo-600 text-[10px] font-medium rounded-full border border-indigo-100 whitespace-nowrap"
          aria-label={`渠道切换 ${channelTransferCount} 次`}
        >
          <Route className="w-2.5 h-2.5" />
          <span>{channelTransferCount}</span>
        </span>
      )}
    </span>
  );
};

/**
 * EndpointChainCell - 端点链路单元格组件（折叠显示 + Portal Tooltip）
 * 使用 createPortal 渲染到 document.body，避免被父容器 overflow 裁剪
 * @param {Object} request - 请求数据
 */
const EndpointChainCell = ({ request }) => {
  const [showTooltip, setShowTooltip] = useState(false);
  const [tooltipPos, setTooltipPos] = useState(null);
  const triggerRef = useRef(null);
  const tooltipRef = useRef(null);
  const hideTimerRef = useRef(null);

  // 显示 Tooltip：计算视口定位和可用空间
  const handleMouseEnter = useCallback(() => {
    clearTimeout(hideTimerRef.current);
    if (!triggerRef.current) return;
    const rect = triggerRef.current.getBoundingClientRect();
    const spaceAbove = rect.top;
    const spaceBelow = window.innerHeight - rect.bottom;
    // 选择空间更大的方向弹出
    const showAbove = spaceAbove > spaceBelow;
    // 可用最大高度（减去 gap 8px + viewport 边距 16px）
    const maxHeight = Math.max(120, (showAbove ? spaceAbove : spaceBelow) - 24);
    setTooltipPos({
      left: Math.max(8, Math.min(rect.left, window.innerWidth - 380)),
      showAbove,
      maxHeight,
      ...(showAbove
        ? { bottom: window.innerHeight - rect.top + 8 }
        : { top: rect.bottom + 8 }),
    });
    setShowTooltip(true);
  }, []);

  // 延迟隐藏（允许鼠标移入可交互 Tooltip 时取消关闭）
  const handleMouseLeave = useCallback(() => {
    hideTimerRef.current = setTimeout(() => setShowTooltip(false), 100);
  }, []);

  // Tooltip 鼠标事件（可交互模式：支持内部滚动）
  const handleTooltipEnter = useCallback(() => clearTimeout(hideTimerRef.current), []);
  const handleTooltipLeave = useCallback(() => setShowTooltip(false), []);

  // 清理定时器
  useEffect(() => () => clearTimeout(hideTimerRef.current), []);

  // 滚动时关闭 Tooltip（排除 Tooltip 内部滚动）
  useEffect(() => {
    if (!showTooltip) return;
    const hide = (e) => {
      if (tooltipRef.current?.contains(e.target)) return;
      setShowTooltip(false);
    };
    window.addEventListener('scroll', hide, true);
    return () => window.removeEventListener('scroll', hide, true);
  }, [showTooltip]);

  const chainItems = getEndpointChainItems(request);
  const showChannelInChain = requestHasInterChannelTransfer(request);

  // 获取链路长度（兼容 camelCase 和 Wails 原始 snake_case）
  const chainLength = Number.isFinite(request.endpointChainLength)
    ? request.endpointChainLength
    : chainItems.length;
  const chainHead = request.endpointChainHead || chainItems[0] || request.endpoint || '';
  const chainTail = request.endpointChainTail || chainItems[chainItems.length - 1] || chainHead || '';

  // 解析端点名称（去除 channel:: 前缀以简化显示）
  const tailEndpoint = parseEndpointName(chainTail);

  // Portal Tooltip 渲染器
  // @param content - Tooltip 内容
  // @param interactive - 是否可交互（启用内部滚动，鼠标可移入）
  const renderTooltip = (content, interactive = false) => {
    if (!showTooltip || !tooltipPos) return null;
    const style = {
      position: 'fixed',
      left: `${tooltipPos.left}px`,
      zIndex: 9999,
      ...(interactive
        ? { maxHeight: `${tooltipPos.maxHeight}px`, display: 'flex', flexDirection: 'column' }
        : { pointerEvents: 'none' }),
      ...(tooltipPos.showAbove
        ? { bottom: `${tooltipPos.bottom}px` }
        : { top: `${tooltipPos.top}px` }),
    };
    const arrowClass = tooltipPos.showAbove
      ? 'absolute -bottom-[6px] left-3 w-0 h-0 border-l-[6px] border-l-transparent border-r-[6px] border-r-transparent border-t-[6px] border-t-slate-800'
      : 'absolute -top-[6px] left-3 w-0 h-0 border-l-[6px] border-l-transparent border-r-[6px] border-r-transparent border-b-[6px] border-b-slate-800';
    return createPortal(
      <div
        ref={tooltipRef}
        style={style}
        className="bg-slate-800 text-white text-xs rounded-lg shadow-xl"
        {...(interactive ? { onMouseEnter: handleTooltipEnter, onMouseLeave: handleTooltipLeave } : {})}
      >
        {interactive
          ? <div className="flex-1 min-h-0 flex flex-col">{content}</div>
          : content}
        <div className={arrowClass} />
      </div>,
      document.body
    );
  };

  // 单端点情况
  if (chainLength <= 1) {
    const singleNode = parseChainNode(chainHead);
    const singleEndpoint = singleNode.endpoint || request.endpoint || '';
    const singleChannel = singleNode.channel || request.channel || '';
    const singleGroup = singleNode.credentialGroup || request.credentialGroup || request.groupName || '';
    if (!singleEndpoint) return <span className="text-gray-400 text-xs">-</span>;
    return (
      <div
        ref={triggerRef}
        className="inline-flex items-center"
        onMouseEnter={handleMouseEnter}
        onMouseLeave={handleMouseLeave}
      >
        <div className="inline-flex items-center gap-1.5 min-w-0">
          <span className="text-gray-600 text-xs truncate max-w-[140px]">
            {singleEndpoint}
          </span>
        </div>
        {(singleChannel || singleGroup || singleEndpoint) && renderTooltip(
          <div className="p-2.5 whitespace-nowrap">
            <div className="flex items-center gap-1.5">
              {showChannelInChain && singleChannel && (
                <ChainTooltipBadge label="渠道" value={singleChannel} />
              )}
              {singleGroup && (
                <ChainTooltipBadge label="分组" value={singleGroup} />
              )}
              <span className="text-slate-100">{singleEndpoint}</span>
            </div>
          </div>
        )}
      </div>
    );
  }

  // 多端点情况：折叠显示，悬浮查看完整路径
  return (
    <div
      ref={triggerRef}
      className="inline-flex items-center gap-1.5"
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
    >
      {/* 主显示：最终端点 + 跳数徽章 */}
      <span className="text-gray-700 text-xs font-medium truncate max-w-[100px]">
        {tailEndpoint}
      </span>
      <span className="inline-flex items-center gap-0.5 px-1.5 py-0.5 bg-indigo-50 text-indigo-600 text-[10px] font-medium rounded-full border border-indigo-100 whitespace-nowrap">
        <Route className="w-2.5 h-2.5" />
        <span>{chainLength}</span>
      </span>

      {/* 悬浮 Tooltip：显示完整链路（可滚动） */}
      {chainItems.length > 0 && renderTooltip(
        <div className="min-w-[220px] max-w-[360px] flex flex-col min-h-0">
          <div className="shrink-0 px-3 pt-3 pb-1 text-[10px] text-slate-400 font-medium uppercase tracking-wide">转移路径</div>
          <div className="overflow-y-auto min-h-0 px-3 pb-3 space-y-1" style={{ scrollbarWidth: 'thin', scrollbarColor: 'rgba(100,116,139,0.5) transparent' }}>
            {chainItems.map((item, idx) => {
              const { channel, credentialGroup, endpoint } = parseChainNode(item);
              const isLast = idx === chainItems.length - 1;
              const isFirst = idx === 0;
              const displayName = showChannelInChain
                ? (endpoint || channel || '-')
                : (endpoint || credentialGroup || '-');
              return (
                <div key={idx}>
                  <div className="flex items-center gap-2">
                    <span className={`text-[10px] w-4 shrink-0 text-center ${isLast ? 'text-emerald-400' : isFirst ? 'text-blue-400' : 'text-slate-500'}`}>
                      {isFirst ? '起' : isLast ? '终' : '·'}
                    </span>
                    <div className="flex items-center gap-1.5 flex-1 min-w-0">
                      {showChannelInChain && channel && (
                        <ChainTooltipBadge label="渠道" value={channel} />
                      )}
                      {credentialGroup && (
                        <ChainTooltipBadge label="分组" value={credentialGroup} />
                      )}
                      <span className={`truncate ${isLast ? 'text-emerald-400 font-semibold' : isFirst ? 'text-blue-300' : 'text-slate-300'}`}>
                        {displayName}
                      </span>
                    </div>
                  </div>
                  {/* 中间连接线 */}
                  {!isLast && (
                    <div className="flex items-center gap-2 py-0.5">
                      <span className="w-4 shrink-0 flex justify-center">
                        <span className="w-px h-2 bg-slate-600" />
                      </span>
                      <ChevronRight className="w-3 h-3 text-slate-600" />
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>,
        true
      )}
    </div>
  );
};

const FirstByteBadge = ({ ms, status }) => {
  if (ms === null || ms === undefined) {
    return (
      <span className="inline-flex items-center rounded-full border border-slate-200 bg-slate-50 px-1.5 py-0.5 font-mono text-[11px] text-slate-400 whitespace-nowrap">
        -
      </span>
    );
  }

  return <DurationBadge ms={ms} status={status} size="xs" />;
};

const LatencyCell = ({ request }) => (
  <div className="inline-flex items-center justify-center gap-1.5">
    <DurationBadge ms={request.duration} status={request.status} size="xs" />
    <FirstByteBadge ms={request.firstByteMs} status={request.status} />
  </div>
);

/**
 * 渲染单元格内容
 * @param {string} columnId - 列 ID
 * @param {Object} request - 请求数据
 */
const renderCell = (columnId, request) => {
  switch (columnId) {
    case 'requestId':
      const StreamIcon = request.isStreaming ? Waves : RefreshCw;
      const streamTitle = request.isStreaming ? '流式请求' : '常规请求';
      const iconColor = request.isStreaming ? 'text-blue-500' : 'text-slate-400';
      return (
        <div className="flex w-full items-center justify-center gap-1.5 text-blue-600 font-mono text-xs group-hover:text-indigo-600 transition-colors">
          <Tooltip content={streamTitle}>
            <StreamIcon className={`w-3 h-3 ${iconColor} flex-shrink-0`} />
          </Tooltip>
          <span className="truncate">{request.requestId}</span>
        </div>
      );
    case 'timestamp':
      return <span className="text-xs text-gray-400">{formatTimestamp(request.timestamp)}</span>;
    case 'provider':
      const providerValue = request.provider || 'claude';
      const isCodex = providerValue === 'codex';
      return (
        <span className={`inline-flex items-center justify-center rounded-full border px-2 py-0.5 text-[11px] font-medium whitespace-nowrap ${
          isCodex
            ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
            : 'border-indigo-200 bg-indigo-50 text-indigo-700'
        }`}>
          {isCodex ? 'Codex' : 'Claude'}
        </span>
      );
    case 'requestType':
      return (
        <span className={`inline-flex items-center justify-center rounded-full border px-2 py-0.5 text-[11px] font-medium whitespace-nowrap ${
          request.isStreaming
            ? 'border-blue-200 bg-blue-50 text-blue-700'
            : 'border-slate-200 bg-slate-50 text-slate-600'
        }`}>
          {request.isStreaming ? '流式' : '非流'}
        </span>
      );
    case 'status':
      return <RequestStatusBadge status={request.status} />;
    case 'model':
      return (
        <ModelTag
          model={request.model}
          rewriteSource={request.modelRewriteSource || request.model_rewrite_source || ''}
          originalModel={request.originalModelName || request.original_model_name || ''}
          rewriteTarget={request.modelRewriteTarget || request.model_rewrite_target || request.model || ''}
        />
      );
    case 'channel':
      return <ChannelCell request={request} />;
    case 'endpoint':
      return <EndpointChainCell request={request} />;
    case 'duration':
      return <LatencyCell request={request} />;
    case 'inputTokens':
      return <span className="text-gray-700 font-mono text-xs">{getDisplayInputTokens(request)}</span>;
    case 'outputTokens':
      return <span className="text-gray-700 font-mono text-xs">{request.outputTokens}</span>;
    case 'reasoningEffort':
      return getReasoningDisplay(request.reasoningEffort || request.reasoning_effort);
    case 'cacheCreationTokens':
      return <span className="text-gray-500 font-mono text-xs">{request.cacheCreationTokens}</span>;
    case 'cacheReadTokens':
      return <span className="text-gray-500 font-mono text-xs">{request.cacheReadTokens}</span>;
    case 'cost':
      return <span className="font-mono text-orange-500 font-medium text-xs">{formatCost(request.cost)}</span>;
    default:
      return null;
  }
};

/**
 * RequestRow - 单行请求数据（支持单击复制、双击查看详情）
 */
const RequestRow = ({ request, columns, onCopyId, onDoubleClick }) => {
  const [clickCount, setClickCount] = React.useState(0);
  const [clickTimer, setClickTimer] = React.useState(null);

  const handleRowClick = () => {
    // 增加点击计数
    const newCount = clickCount + 1;
    setClickCount(newCount);

    // 清除之前的定时器
    if (clickTimer) {
      clearTimeout(clickTimer);
    }

    if (newCount === 1) {
      // 第一次点击，启动定时器等待第二次点击
      const timer = setTimeout(() => {
        // 单击：复制 ID
        onCopyId?.(request.requestId);
        setClickCount(0);
      }, 250);
      setClickTimer(timer);
    } else if (newCount === 2) {
      // 双击：打开详情
      onDoubleClick?.(request);
      setClickCount(0);
      setClickTimer(null);
    }
  };

  return (
    <tr
      className="hover:bg-gray-50/80 transition-colors group cursor-pointer"
      onClick={handleRowClick}
    >
      {columns.map((column) => (
        <td
          key={column.id}
          className={`min-w-0 px-3 py-3 text-center align-middle ${column.id === 'model' ? 'overflow-hidden' : ''}`}
          style={column.width === 'auto' ? undefined : { width: column.width, maxWidth: column.width }}
        >
          {renderCell(column.id, request)}
        </td>
      ))}
    </tr>
  );
};

/**
 * RequestsTable - 请求列表表格组件（支持动态列）
 * @param {Object} props
 * @param {Array} props.requests - 请求列表
 * @param {boolean} props.loading - 加载状态
 * @param {Object} props.pagination - 分页信息
 * @param {Function} props.onPageChange - 页码变更回调
 * @param {Function} props.onPageSizeChange - 每页条数变更回调
 * @param {Array} props.visibleColumns - 可见列ID数组
 * @param {Array} props.columnConfigs - 列配置数组
 * @param {Function} props.onRowDoubleClick - 双击行回调
 */
const RequestsTable = ({
  requests = [],
  loading = false,
  pagination = { page: 1, pageSize: 10, total: 0 },
  onPageChange,
  onPageSizeChange,
  visibleColumns = [],
  columnConfigs = [],
  onRowDoubleClick
}) => {
  // 复制请求 ID
  const handleCopyId = (id) => {
    navigator.clipboard.writeText(id);
  };

  // 获取可见的列配置
  const visibleColumnConfigs = columnConfigs.filter(col => visibleColumns.includes(col.id));

  return (
    <div className="bg-white rounded-xl shadow-sm border border-gray-200 overflow-hidden flex flex-col h-full min-h-0">
      {/* 表头 */}
      <div className="flex items-center justify-between border-b border-gray-100 bg-gray-50/30 px-4 py-4">
        <div className="flex items-center gap-2">
          <h3 className="font-semibold text-gray-800">请求明细</h3>
          <span className="px-2 py-0.5 bg-gray-100 text-gray-500 text-xs rounded-full font-medium">
            共 {pagination.total} 条
          </span>
        </div>
        <span className="text-xs text-gray-400">单击复制 ID · 双击查看详情</span>
      </div>

      {/* 表格 */}
      <div className="relative flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
        {loading && (
          <div className="absolute inset-0 z-10 bg-white/70 backdrop-blur-[1px] flex items-center justify-center pointer-events-none">
            <LoadingSpinner text="加载请求数据..." />
          </div>
        )}

        <table className={`w-full text-sm whitespace-nowrap ${loading ? 'opacity-60' : ''}`}>
          <thead className="sticky top-0 z-10 bg-gray-50 text-gray-500 border-b border-gray-100">
            <tr>
              {visibleColumnConfigs.map(col => (
                <th
                  key={col.id}
                  className="px-3 py-3 font-medium text-xs uppercase tracking-wider text-center align-middle"
                  style={col.width === 'auto' ? undefined : { width: col.width, maxWidth: col.width }}
                >
                  {col.label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100">
            {requests.length === 0 ? (
              <tr>
                <td colSpan={visibleColumnConfigs.length} className="px-5 py-12 text-center text-slate-500">
                  暂无请求数据
                </td>
              </tr>
            ) : (
              requests.map((req) => (
                <RequestRow
                  key={req.rawRequestId || req.requestId}
                  request={req}
                  columns={visibleColumnConfigs}
                  onCopyId={handleCopyId}
                  onDoubleClick={onRowDoubleClick}
                />
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* 分页 */}
      <Pagination
        currentPage={pagination.page}
        pageSize={pagination.pageSize}
        total={pagination.total}
        onPageChange={onPageChange}
        onPageSizeChange={onPageSizeChange}
      />
    </div>
  );
};

export default RequestsTable;
