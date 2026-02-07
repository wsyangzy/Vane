// ============================================
// RequestsTable - 请求列表表格（支持动态列配置）
// 2025-12-01 10:31:09
// 更新：添加双击查看详情功能
// ============================================

import React, { useState } from 'react';
import { Waves, RefreshCw, GitBranch, ChevronRight } from 'lucide-react';
import { LoadingSpinner } from '@components/ui';
import { formatCost, formatDuration, formatTimestamp } from '@utils/api.js';
import RequestStatusBadge from './RequestStatusBadge.jsx';
import ModelTag from './ModelTag.jsx';
import Pagination from './Pagination.jsx';

/**
 * 解析链路端点，提取端点名称（去除 channel:: 前缀）
 */
const parseEndpointName = (fullKey) => {
  if (!fullKey) return '';
  const parts = fullKey.split('::');
  return parts.length > 1 ? parts[1] : parts[0];
};

/**
 * 解析链路端点，提取渠道名称
 */
const parseChannelName = (fullKey) => {
  if (!fullKey) return '';
  const parts = fullKey.split('::');
  return parts.length > 1 ? parts[0] : '';
};

/**
 * EndpointChainCell - 端点链路单元格组件（折叠显示 + 悬浮 Tooltip）
 * @param {Object} request - 请求数据
 * @param {number} rowIndex - 行索引，用于决定悬浮框方向（前2行向下弹出）
 */
const EndpointChainCell = ({ request, rowIndex = 0 }) => {
  const [showTooltip, setShowTooltip] = useState(false);

  // 前两行向下弹出，其他向上弹出（避免被容器裁剪）
  const tooltipPosition = rowIndex < 2 ? 'top-full mt-2' : 'bottom-full mb-2';
  const arrowPosition = rowIndex < 2
    ? 'absolute -top-[6px] left-3 w-0 h-0 border-l-[6px] border-l-transparent border-r-[6px] border-r-transparent border-b-[6px] border-b-slate-800'
    : 'absolute -bottom-[6px] left-3 w-0 h-0 border-l-[6px] border-l-transparent border-r-[6px] border-r-transparent border-t-[6px] border-t-slate-800';

  // 获取链路长度（api.js 已转换为 camelCase）
  const chainLength = Number.isFinite(request.endpointChainLength)
    ? request.endpointChainLength
    : (Array.isArray(request.endpointChain) ? request.endpointChain.length : 0);
  const chainHead = request.endpointChainHead || request.endpoint || '';
  const chainTail = request.endpointChainTail || chainHead || '';

  // 解析端点名称（去除 channel:: 前缀以简化显示）
  const tailEndpoint = parseEndpointName(chainTail);

  // 单端点情况
  if (chainLength <= 1) {
    const singleEndpoint = parseEndpointName(chainHead);
    const singleChannel = parseChannelName(chainHead);
    if (!singleEndpoint) return <span className="text-gray-400 text-xs">-</span>;
    return (
      <div
        className="relative inline-flex items-center"
        onMouseEnter={() => setShowTooltip(true)}
        onMouseLeave={() => setShowTooltip(false)}
      >
        <span className="text-gray-600 text-xs truncate max-w-[140px]">
          {singleEndpoint}
        </span>
        {showTooltip && singleChannel && (
          <div className={`absolute left-0 ${tooltipPosition} z-50 bg-slate-800 text-white text-xs rounded-lg shadow-xl p-2.5 whitespace-nowrap`}>
            <div className="flex items-center gap-1.5">
              <span className="px-1.5 py-0.5 bg-slate-700/80 text-slate-300 text-[9px] rounded font-medium">
                {singleChannel}
              </span>
              <span className="text-slate-100">{singleEndpoint}</span>
            </div>
            {/* 箭头 */}
            <div className={arrowPosition} />
          </div>
        )}
      </div>
    );
  }

  // 多端点情况：折叠显示，悬浮查看完整路径
  // endpointChain 在 api.js 中已被转换为数组
  const chainItems = Array.isArray(request.endpointChain)
    ? request.endpointChain
    : (typeof request.endpoint_chain === 'string'
        ? request.endpoint_chain.split(' -> ').filter(s => s.trim())
        : []);

  return (
    <div
      className="relative inline-flex items-center gap-1.5"
      onMouseEnter={() => setShowTooltip(true)}
      onMouseLeave={() => setShowTooltip(false)}
    >
      {/* 主显示：最终端点 + 跳数徽章 */}
      <span className="text-gray-700 text-xs font-medium truncate max-w-[100px]">
        {tailEndpoint}
      </span>
      <span className="inline-flex items-center gap-0.5 px-1.5 py-0.5 bg-indigo-50 text-indigo-600 text-[10px] font-medium rounded-full border border-indigo-100 whitespace-nowrap">
        <GitBranch className="w-2.5 h-2.5" />
        <span>{chainLength}</span>
      </span>

      {/* 悬浮 Tooltip：显示完整链路 */}
      {showTooltip && chainItems.length > 0 && (
        <div className={`absolute left-0 ${tooltipPosition} z-50 bg-slate-800 text-white text-xs rounded-lg shadow-xl p-3 min-w-[220px] max-w-[360px]`}>
          <div className="text-[10px] text-slate-400 mb-2 font-medium uppercase tracking-wide">转移路径</div>
          <div className="space-y-1">
            {chainItems.map((item, idx) => {
              const channel = parseChannelName(item);
              const endpoint = parseEndpointName(item);
              const isLast = idx === chainItems.length - 1;
              const isFirst = idx === 0;
              return (
                <div key={idx}>
                  <div className="flex items-center gap-2">
                    <span className={`text-[10px] w-4 shrink-0 text-center ${isLast ? 'text-emerald-400' : isFirst ? 'text-blue-400' : 'text-slate-500'}`}>
                      {isFirst ? '起' : isLast ? '终' : '·'}
                    </span>
                    <div className="flex items-center gap-1.5 flex-1 min-w-0">
                      {channel && (
                        <span className="px-1.5 py-0.5 bg-slate-700/80 text-slate-300 text-[9px] rounded shrink-0 font-medium">
                          {channel}
                        </span>
                      )}
                      <span className={`truncate ${isLast ? 'text-emerald-400 font-semibold' : isFirst ? 'text-blue-300' : 'text-slate-300'}`}>
                        {endpoint || item}
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
          {/* 箭头 */}
          <div className={arrowPosition} />
        </div>
      )}
    </div>
  );
};

/**
 * 渲染单元格内容
 * @param {string} columnId - 列 ID
 * @param {Object} request - 请求数据
 * @param {number} rowIndex - 行索引
 */
const renderCell = (columnId, request, rowIndex = 0) => {
  switch (columnId) {
    case 'requestId':
      const StreamIcon = request.isStreaming ? Waves : RefreshCw;
      const streamTitle = request.isStreaming ? '流式请求' : '常规请求';
      const iconColor = request.isStreaming ? 'text-blue-500' : 'text-slate-400';
      return (
        <div className="flex items-center gap-1.5 text-blue-600 font-mono text-xs group-hover:text-indigo-600 transition-colors">
          <StreamIcon className={`w-3 h-3 ${iconColor} flex-shrink-0`} title={streamTitle} />
          <span className="truncate">{request.requestId}</span>
        </div>
      );
    case 'timestamp':
      return <span className="text-xs text-gray-400">{formatTimestamp(request.timestamp)}</span>;
    case 'status':
      return <RequestStatusBadge status={request.status} />;
    case 'model':
      return <ModelTag model={request.model} />;
    case 'channel':
      return <span className="text-purple-600 text-xs font-medium">{request.channel || '-'}</span>;
    case 'endpoint':
      return <EndpointChainCell request={request} rowIndex={rowIndex} />;
    case 'duration':
      return <span className="text-gray-700 font-mono text-xs">{formatDuration(request.duration)}</span>;
    case 'inputTokens':
      return <span className="text-gray-700 text-right font-mono text-xs">{request.inputTokens}</span>;
    case 'outputTokens':
      return <span className="text-gray-700 text-right font-mono text-xs">{request.outputTokens}</span>;
    case 'cacheCreationTokens':
      return <span className="text-gray-500 text-right font-mono text-xs">{request.cacheCreationTokens}</span>;
    case 'cacheReadTokens':
      return <span className="text-gray-500 text-right font-mono text-xs">{request.cacheReadTokens}</span>;
    case 'cost':
      return <span className="text-right font-mono text-orange-500 font-medium text-xs">{formatCost(request.cost)}</span>;
    default:
      return null;
  }
};

/**
 * RequestRow - 单行请求数据（支持单击复制、双击查看详情）
 */
const RequestRow = ({ request, visibleColumns, rowIndex, onCopyId, onDoubleClick }) => {
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
      {visibleColumns.map(colId => (
        <td
          key={colId}
          className={`px-3 py-3 ${colId === 'cost' || colId.includes('Tokens') ? 'text-right' : ''}`}
        >
          {renderCell(colId, request, visibleColumns, rowIndex)}
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
      <div className="px-4 py-4 border-b border-gray-100 flex justify-between items-center bg-gray-50/30">
        <div className="flex items-center gap-2">
          <h3 className="font-semibold text-gray-800">请求明细</h3>
          <span className="px-2 py-0.5 bg-gray-100 text-gray-500 text-xs rounded-full font-medium">
            共 {pagination.total} 条
          </span>
        </div>
        <span className="text-xs text-gray-400">单击复制 ID · 双击查看详情</span>
      </div>

      {/* 表格 */}
      <div className="relative flex-1 min-h-0 overflow-y-auto overflow-x-auto">
        {loading && (
          <div className="absolute inset-0 z-10 bg-white/70 backdrop-blur-[1px] flex items-center justify-center pointer-events-none">
            <LoadingSpinner text="加载请求数据..." />
          </div>
        )}

        <table className={`w-full text-left text-sm whitespace-nowrap ${loading ? 'opacity-60' : ''}`}>
          <thead className="bg-gray-50/50 text-gray-500 border-b border-gray-100">
            <tr>
              {visibleColumnConfigs.map(col => (
                <th
                  key={col.id}
                  className={`px-3 py-3 font-medium text-xs uppercase tracking-wider ${
                    col.align === 'right' ? 'text-right' : ''
                  }`}
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
              requests.map((req, index) => (
                <RequestRow
                  key={req.rawRequestId || req.requestId}
                  request={req}
                  visibleColumns={visibleColumns}
                  rowIndex={index}
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
